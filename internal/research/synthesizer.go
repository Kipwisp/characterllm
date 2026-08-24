// Package research provides tools for researching character data and synthesizing high-fidelity personas.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/search"
)

// AnalysisResult contains the deconstructed intent of the user's request.
type AnalysisResult struct {
	Status       string   `json:"status"`
	OfficialName string   `json:"official_name"`
	Modifiers    []string `json:"modifiers"`
	Scenario     string   `json:"scenario"`
	ScenarioID   string   `json:"scenario_id"`
	Series       string   `json:"series"`
	DisplayName  string   `json:"display_name"`
	Aliases      []string `json:"aliases"`
	Ambiguities  []string `json:"ambiguities"`
}

// SynthesisResult contains the result of the synthesis phase.
type SynthesisResult struct {
	PersonaSpec  string
	Reasoning    string
	Status       string // "OK", "UNKNOWN", "AMBIGUOUS"
	Ambiguities  []string
	ResearchData string
}

// CharacterDetails holds the extracted information about a character.
type CharacterDetails struct {
	Name        string
	Series      string
	Description string
	URL         string
}

// Synthesizer coordinates the process of searching for character data and synthesizing a profile via LLM.
type Synthesizer struct {
	searchProvider search.SearchProvider
	llmClient      llm.LLMClient
	config         *config.Config
}

// NewSynthesizer creates a new character synthesizer.
func NewSynthesizer(sp search.SearchProvider, llm llm.LLMClient, cfg *config.Config) *Synthesizer {
	return &Synthesizer{
		searchProvider: sp,
		llmClient:      llm,
		config:         cfg,
	}
}

// AnalyzeInput deconstructs the user's request into a structured analysis result.
func (s *Synthesizer) AnalyzeInput(ctx context.Context, input string) (*AnalysisResult, string, string, error) {
	template, err := os.ReadFile(s.config.Prompts.AnalyzerPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read analyzer prompt: %w", err)
	}

	prompt := strings.Replace(string(template), "{{INPUT}}", input, 1)
	messages := []llm.Message{{Role: "user", Content: prompt}}

	var lastResponse, lastReasoning string
	for attempt := 1; attempt <= s.config.LLM.MaxRetries; attempt++ {
		logger.FromContext(ctx).Debug("analyzing user input", "input", input, "attempt", attempt)
		response, reasoning, err := s.llmClient.GenerateResponse(ctx, messages, s.config.LLM.Model)
		if err != nil {
			return nil, lastResponse, lastReasoning, fmt.Errorf("analysis failed: %w", err)
		}

		lastResponse, lastReasoning = response, reasoning

		var result AnalysisResult
		if err := json.Unmarshal([]byte(response), &result); err != nil {
			// Attempt to clean response if LLM added markdown blocks
			trimmed := strings.TrimPrefix(response, "```json")
			trimmed = strings.TrimSuffix(trimmed, "```")
			trimmed = strings.TrimSpace(trimmed)
			if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
				if attempt < s.config.LLM.MaxRetries {
					logger.FromContext(ctx).Warn("analysis JSON malformed, retrying", "error", err)
					messages = append(messages, llm.Message{Role: "assistant", Content: response})
					messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf("Your response was not valid JSON: %v. Please output only the raw JSON object without markdown blocks.", err)})
					continue
				}
				return nil, response, reasoning, fmt.Errorf("failed to decode analysis JSON after retry: %w", err)
			}
			return &result, response, reasoning, nil
		}
		return &result, response, reasoning, nil
	}

	return nil, lastResponse, lastReasoning, fmt.Errorf("analysis failed after maximum retries")
}

// FetchCharacter performs a web search and uses an LLM to synthesize a structured character profile.
func (s *Synthesizer) FetchCharacter(ctx context.Context, analysis *AnalysisResult) (*SynthesisResult, error) {
	logger.FromContext(ctx).Info("beginning character research", "target", analysis.OfficialName)

	// 1. Research: Search for the canonical character
	query := fmt.Sprintf("%s character details biography personality traits", analysis.OfficialName)
	if analysis.Series != "" {
		query = fmt.Sprintf("%s (%s) character details biography personality traits", analysis.OfficialName, analysis.Series)
	}
	results, err := s.searchProvider.Search(ctx, query, s.config.Images.MaxResults)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		return &SynthesisResult{Status: "UNKNOWN"}, nil
	}

	// 2. Assembly: Build the Research Dossier
	var dossier strings.Builder
	for i, res := range results {
		fmt.Fprintf(&dossier, "\n--- Result %d ---\nTitle: %s\nURL: %s\nSnippet: %s\n", i+1, res.Title, res.URL, res.Snippet)
	}

	// 3. Synthesis: Use LLM to create the profile
	template, err := os.ReadFile(s.config.Prompts.SynthesisPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read synthesis prompt: %w", err)
	}

	// Inject the modifiers into the prompt if any exist
	modifierInstruction := ""
	if len(analysis.Modifiers) > 0 {
		mods := strings.Join(analysis.Modifiers, ", ")
		modifierInstruction = fmt.Sprintf("\nIMPORTANT: The user has requested a MODIFIED version of this character: %s. You MUST merge the canonical research provided below with the traits, psychology, and physical changes implied by the modifiers '%s'.", mods, mods)
	}

	prompt := strings.Replace(string(template), "{{RESULTS}}", dossier.String(), 1)

	scenarioBlock := ""
	if analysis.Scenario != "" {
		scenarioBlock = fmt.Sprintf("### Scenario\nScenario: %s\nDefine the specific, temporary state of the world and the character's immediate circumstances based on the provided scenario.", analysis.Scenario)
	}
	prompt = strings.Replace(prompt, "{{SCENARIO_BLOCK}}", scenarioBlock, 1)
	prompt = strings.Replace(prompt, "### Output Structure", modifierInstruction+"\n\n### Output Structure", 1)

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	for attempt := 1; attempt <= s.config.LLM.MaxRetries; attempt++ {
		logger.FromContext(ctx).Info("sending research dossier to LLM for synthesis", "modifiers", analysis.Modifiers, "attempt", attempt)
		profile, reasoning, err := s.llmClient.GenerateResponse(ctx, messages, s.config.LLM.Model)
		if err != nil {
			return nil, fmt.Errorf("synthesis failed: %w", err)
		}

		res := s.parseSynthesis(profile)

		// If status is OK or explicitly marked as failure (UNKNOWN/AMBIGUOUS), accept it.
		// If status is UNKNOWN but it didn't start with "STATUS:", it's a formatting error (missing headers).
		if res.Status == "OK" || strings.HasPrefix(profile, "STATUS:") {
			res.Reasoning = reasoning
			res.ResearchData = dossier.String()
			return res, nil
		}

		if attempt < s.config.LLM.MaxRetries {
			logger.FromContext(ctx).Warn("synthesis response missing required headers, retrying", "attempt", attempt)
			messages = append(messages, llm.Message{Role: "assistant", Content: profile})
			messages = append(messages, llm.Message{Role: "user", Content: "Your response is missing the required '### Identity & Temperament' section. Please ensure you follow the Output Structure exactly."})
			continue
		}
	}

	return nil, fmt.Errorf("synthesis failed to produce valid output after maximum retries")
}

// parseSynthesis extracts a SynthesisResult from the LLM's formatted response.
func (s *Synthesizer) parseSynthesis(output string) *SynthesisResult {
	if strings.HasPrefix(output, "STATUS: UNKNOWN") {
		return &SynthesisResult{Status: "UNKNOWN"}
	}
	if strings.HasPrefix(output, "STATUS: AMBIGUOUS") {
		msg := strings.TrimPrefix(output, "STATUS: AMBIGUOUS")
		return &SynthesisResult{
			Status:      "AMBIGUOUS",
			Ambiguities: strings.Split(strings.TrimSpace(msg), "\n"),
		}
	}

	res := &SynthesisResult{Status: "OK"}

	// The persona spec starts after the metadata
	// We find the first header "### Identity & Temperament"
	idx := strings.Index(output, "### Identity & Temperament")
	if idx != -1 {
		res.PersonaSpec = output[idx:]
	} else {
		res.Status = "UNKNOWN"
	}

	return res
}
