// Package research provides tools for researching character data and synthesizing high-fidelity personas.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/prompts"
	"characterllm/internal/search"
)

// AnalysisStatus is the analyzer's verdict on the user's request.
type AnalysisStatus string

const (
	AnalysisStatusOK        AnalysisStatus = "OK"
	AnalysisStatusUnknown   AnalysisStatus = "UNKNOWN"
	AnalysisStatusAmbiguous AnalysisStatus = "AMBIGUOUS"
	AnalysisStatusInjection AnalysisStatus = "INJECTION"
)

// AnalysisResult contains the deconstructed intent of the user's request.
type AnalysisResult struct {
	Status       AnalysisStatus `json:"status"`
	OfficialName string         `json:"official_name"`
	Modifiers    []string       `json:"modifiers"`
	Scenario     string         `json:"scenario"`
	Series       string         `json:"series"`
	DisplayName  string         `json:"display_name"`
	Ambiguities  []string       `json:"ambiguities"`
}

// SynthesisStatus is the synthesizer's verdict on the character research.
type SynthesisStatus string

const (
	SynthesisStatusOK        SynthesisStatus = "OK"
	SynthesisStatusUnknown   SynthesisStatus = "UNKNOWN"
	SynthesisStatusAmbiguous SynthesisStatus = "AMBIGUOUS"
)

// SynthesisResult contains the result of the synthesis phase.
type SynthesisResult struct {
	PersonaSpec  string
	Reasoning    string
	Status       SynthesisStatus
	Ambiguities  []string
	ResearchData string
	// AvatarChoice is the 1-based position of the candidate image the model
	// picked as the character's avatar (0 = no pick). Only set when the
	// synthesis call was made with candidate images.
	AvatarChoice int
	// RawResponse is the model's unprocessed output, kept for audit logging.
	RawResponse string
}

// CharacterDetails holds the extracted information about a character.
type CharacterDetails struct {
	Name        string
	Series      string
	Description string
	URL         string
}

// SectionRewriteRequest carries the card context for a scoped persona-section
// rewrite.
type SectionRewriteRequest struct {
	DisplayName  string
	OfficialName string
	Series       string
	Spec         string
	Section      string
	CurrentBody  string
	Instruction  string
	// WholePersona rewrites the entire specification in one pass instead of a
	// single section, so an instruction can consistently update every section
	// it implies (Identity, Voice, Example Dialogue, ...). Section,
	// CurrentBody, and the spec's per-section framing are ignored in this
	// mode.
	WholePersona bool
}

// SectionRewriteResult contains the rewritten section body plus the raw
// exchange, kept for audit logging.
type SectionRewriteResult struct {
	Body      string
	Prompt    string
	Response  string
	Reasoning string
}

// Synthesizer defines the interface for analyzing user input and synthesizing character personas.
type Synthesizer interface {
	AnalyzeInput(ctx context.Context, input string) (*AnalysisResult, string, string, error)
	// FetchCharacter researches and synthesizes the persona. imageURIs are
	// data URIs of images shown to the model; candidateCount is how many
	// avatar candidates the model can choose from (the images may tile
	// several candidates into one picture). The model may pick one via the
	// AvatarChoice in the result; empty imageURIs means the synthesis runs
	// without images and no pick.
	FetchCharacter(ctx context.Context, analysis *AnalysisResult, imageURIs []string, candidateCount int) (*SynthesisResult, error)
	RewriteSection(ctx context.Context, req SectionRewriteRequest) (*SectionRewriteResult, error)
}

// Synthesizer coordinates the process of searching for character data and synthesizing a profile via LLM.
type SynthesizerClient struct {
	searchProvider search.SearchProvider
	llmClient      llm.LLMClient
	config         *config.Config
	prompts        *prompts.Set
}

// NewSynthesizer creates a new character synthesizer.
func NewSynthesizer(sp search.SearchProvider, llm llm.LLMClient, cfg *config.Config, ps *prompts.Set) Synthesizer {
	return &SynthesizerClient{
		searchProvider: sp,
		llmClient:      llm,
		config:         cfg,
		prompts:        ps,
	}
}

// AnalyzeInput deconstructs the user's request into a structured analysis result.
func (s *SynthesizerClient) AnalyzeInput(ctx context.Context, input string) (*AnalysisResult, string, string, error) {
	prompt := strings.Replace(s.prompts.Analyzer, "{{INPUT}}", input, 1)
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
func (s *SynthesizerClient) FetchCharacter(ctx context.Context, analysis *AnalysisResult, imageURIs []string, candidateCount int) (*SynthesisResult, error) {
	logger.FromContext(ctx).Info("beginning character research", "target", analysis.OfficialName)

	// Research: Search for the canonical character
	query := fmt.Sprintf("%s character details biography personality traits", analysis.OfficialName)
	if analysis.Series != "" {
		query = fmt.Sprintf("%s (%s) character details biography personality traits", analysis.OfficialName, analysis.Series)
	}
	results, err := s.searchProvider.Search(ctx, query, s.config.Search.MaxResults)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		return &SynthesisResult{Status: SynthesisStatusUnknown}, nil
	}

	// Build the Research Dossier
	var dossier strings.Builder
	for i, res := range results {
		fmt.Fprintf(&dossier, "\n--- Result %d ---\nTitle: %s\nURL: %s\nSnippet: %s\n", i+1, res.Title, res.URL, res.Snippet)
	}

	// Synthesis: Use LLM to create the profile
	prompt := strings.Replace(s.prompts.Synthesis, "{{RESULTS}}", dossier.String(), 1)

	modifiersBlock := ""
	if len(analysis.Modifiers) > 0 {
		mods := strings.Join(analysis.Modifiers, ", ")
		modifiersBlock = fmt.Sprintf(SectionHeaderPrefix+"Modifiers\nModifiers: %s\nMerge the canonical research provided below with the traits, psychology, and physical changes implied by these modifiers.", mods)
	}
	prompt = strings.Replace(prompt, "{{MODIFIERS_BLOCK}}", modifiersBlock, 1)

	scenarioBlock := ""
	if analysis.Scenario != "" {
		scenarioBlock = fmt.Sprintf(SectionHeaderPrefix+"Scenario\nScenario: %s\nDefine the specific, temporary state of the world and the character's immediate circumstances based on the provided scenario.", analysis.Scenario)
	}
	prompt = strings.Replace(prompt, "{{SCENARIO_BLOCK}}", scenarioBlock, 1)

	avatarBlock := ""
	if len(imageURIs) > 0 {
		avatarBlock = fmt.Sprintf(avatarPickBlock, candidateCount, candidateCount)
	}
	prompt = strings.Replace(prompt, "{{AVATAR_BLOCK}}", avatarBlock, 1)

	messages := []llm.Message{
		{Role: "user", Content: prompt, Images: imageURIs},
	}

	for attempt := 1; attempt <= s.config.LLM.MaxRetries; attempt++ {
		logger.FromContext(ctx).Info("sending research dossier to LLM for synthesis", "modifiers", analysis.Modifiers, "attempt", attempt)
		profile, reasoning, err := s.llmClient.GenerateResponse(ctx, messages, s.config.LLM.Model)
		if err != nil {
			return nil, fmt.Errorf("synthesis failed: %w", err)
		}

		res := s.parseSynthesis(profile, analysis.Scenario)
		if len(imageURIs) == 0 || res.AvatarChoice < 1 || res.AvatarChoice > candidateCount {
			res.AvatarChoice = 0
		}

		// If status is OK or explicitly marked as failure (UNKNOWN/AMBIGUOUS), accept it.
		// If status is UNKNOWN but it didn't start with "STATUS:", it's a formatting error (missing headers).
		if res.Status == SynthesisStatusOK || strings.HasPrefix(profile, "STATUS:") {
			res.Reasoning = reasoning
			res.ResearchData = dossier.String()
			res.RawResponse = profile
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

// avatarPickBlock is the {{AVATAR_BLOCK}} content for a synthesis call that
// carries candidate avatar images. The %d placeholders are the candidate count.
const avatarPickBlock = `### Avatar Selection
The attached image shows %d candidate photos of this character arranged in a single horizontal row, left to right, numbered 1 to %d.
- These photos are pulled from the web: they may be fan art, promotional renders, or the wrong person entirely.
- Compare ALL of the photos and pick the single best one: it must clearly be the character, and of the photos that are, it should be the one that would look best as a small Discord profile picture while still representing the character well: the face clearly visible and roughly centered, decent lighting, a single subject (no group shots), and no heavy text, borders, or watermark clutter.
- Output ` + "`AVATAR: n`" + ` (where n is the 1-based position of the best photo) on a line by itself before the specification.
- If no photo reliably matches the character, do not output an AVATAR line and describe the appearance from the research alone.
`

// parseSynthesis extracts a SynthesisResult from the LLM's formatted response.
func (s *SynthesizerClient) parseSynthesis(output, scenario string) *SynthesisResult {
	output, avatarChoice := removeAvatarLine(output)

	if strings.HasPrefix(output, "STATUS: UNKNOWN") {
		return &SynthesisResult{Status: SynthesisStatusUnknown, AvatarChoice: avatarChoice}
	}
	if strings.HasPrefix(output, "STATUS: AMBIGUOUS") {
		msg := strings.TrimPrefix(output, "STATUS: AMBIGUOUS")
		return &SynthesisResult{
			Status:       SynthesisStatusAmbiguous,
			Ambiguities:  strings.Split(strings.TrimSpace(msg), "\n"),
			AvatarChoice: avatarChoice,
		}
	}

	res := &SynthesisResult{Status: SynthesisStatusOK, AvatarChoice: avatarChoice}

	// The persona spec starts after the metadata
	// We find the first header "### Identity & Temperament"
	idx := strings.Index(output, SectionHeaderPrefix+SectionIdentity)
	if idx == -1 {
		res.Status = SynthesisStatusUnknown
		return res
	}
	res.PersonaSpec = output[idx:]
	if scenario == "" {
		res.PersonaSpec = stripScenarioSection(res.PersonaSpec)
	}
	return res
}

// stripScenarioSection removes a model-written Scenario section from the
// persona spec, preserving any sections that follow it.
func stripScenarioSection(spec string) string {
	return RemoveSection(spec, SectionScenario)
}

// removeAvatarLine strips any standalone "AVATAR: n" line from the model
// output and returns the cleaned output plus the parsed 1-based choice
// (0 when absent or unparseable). The line is removed wherever it appears
// so it never leaks into the persona spec.
func removeAvatarLine(output string) (string, int) {
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	choice := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "AVATAR:") {
			if choice == 0 {
				rest := trimmed[len("AVATAR:"):]
				if i := strings.IndexFunc(rest, func(r rune) bool { return r >= '0' && r <= '9' }); i != -1 {
					num := rest[i:]
					j := 0
					for j < len(num) && num[j] >= '0' && num[j] <= '9' {
						j++
					}
					if n, err := strconv.Atoi(num[:j]); err == nil {
						choice = n
					}
				}
			}
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), choice
}

// RewriteSection asks the LLM to rewrite one persona section — or, when
// req.WholePersona is set, the entire specification — according to the
// user's instruction. Section mode returns only the new section body;
// whole-persona mode returns the complete updated spec.
func (s *SynthesizerClient) RewriteSection(ctx context.Context, req SectionRewriteRequest) (*SectionRewriteResult, error) {
	characterBlock := "Character: " + req.DisplayName
	if req.OfficialName != "" && !strings.EqualFold(req.OfficialName, req.DisplayName) {
		characterBlock += " (official name: " + req.OfficialName + ")"
	}

	seriesBlock := ""
	if req.Series != "" {
		seriesBlock = "Series: " + req.Series
	}

	var contextBlock, targetBlock string
	if req.WholePersona {
		targetBlock = "Mode: Whole-Persona\n\nCurrent specification:\n" + req.Spec
		logger.FromContext(ctx).Info("rewriting whole persona spec")
	} else {
		if rest := strings.TrimSpace(RemoveSection(req.Spec, req.Section)); rest != "" {
			contextBlock = "Rest of the persona specification:\n" + rest
		}
		targetBlock = "Mode: Section\n\nSection: " + req.Section + "\n\nCurrent content:\n" + req.CurrentBody
		logger.FromContext(ctx).Info("rewriting persona section", "section", req.Section)
	}

	instructionBlock := "Instruction: " + req.Instruction

	// The section reference is fetched from the synthesis prompt.
	var refSections []string
	if req.WholePersona {
		refSections = append(append([]string{}, PersonaSectionOrder...), SectionScenario)
	} else {
		refSections = []string{req.Section}
	}
	sectionReference := sectionDefinitionsFrom(s.prompts.Synthesis, refSections)
	if sectionReference != "" {
		sectionReference = "### Section Reference\n" + sectionReference
	}

	userPrompt := s.prompts.EditSection
	userPrompt = strings.Replace(userPrompt, "{{CHARACTER_BLOCK}}", characterBlock, 1)
	userPrompt = strings.Replace(userPrompt, "{{SERIES_BLOCK}}", seriesBlock, 1)
	userPrompt = strings.Replace(userPrompt, "{{CONTEXT_BLOCK}}", contextBlock, 1)
	userPrompt = strings.Replace(userPrompt, "{{TARGET_BLOCK}}", targetBlock, 1)
	userPrompt = strings.Replace(userPrompt, "{{INSTRUCTION_BLOCK}}", instructionBlock, 1)
	userPrompt = strings.Replace(userPrompt, "{{SECTION_REFERENCE}}", sectionReference, 1)
	response, reasoning, err := s.llmClient.GenerateResponse(ctx, []llm.Message{
		{Role: "system", Content: s.prompts.EditSection},
		{Role: "user", Content: userPrompt},
	}, s.config.LLM.Model)
	if err != nil {
		return nil, fmt.Errorf("section rewrite request failed: %w", err)
	}

	var body string
	if req.WholePersona {
		body = strings.TrimSpace(stripCodeFences(response))
		if strings.TrimSpace(body) == "" {
			return nil, fmt.Errorf("model returned an empty specification")
		}
		for _, section := range []string{SectionIdentity, SectionAppearance, SectionVoice, SectionDialogue} {
			if _, ok := ExtractSection(body, section); !ok {
				return nil, fmt.Errorf("model dropped the %q section from the specification", section)
			}
		}
	} else {
		body = stripSectionFormatting(response)
		if strings.TrimSpace(body) == "" {
			return nil, fmt.Errorf("model returned an empty section")
		}
	}

	return &SectionRewriteResult{
		Body:      body,
		Prompt:    userPrompt,
		Response:  response,
		Reasoning: reasoning,
	}, nil
}

// stripCodeFences removes a single wrapping code fence the model may have
// added despite instructions.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.IndexByte(s, '\n'); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

// stripSectionFormatting removes code fences and any leading header lines the
// model may have included despite instructions, leaving raw section body.
func stripSectionFormatting(s string) string {
	s = stripCodeFences(s)
	for strings.HasPrefix(s, "#") {
		idx := strings.IndexByte(s, '\n')
		if idx == -1 {
			// A header with no body: the model produced nothing usable.
			return ""
		}
		s = strings.TrimLeft(s[idx+1:], "\n")
	}
	return strings.TrimSpace(s)
}
