package research

import (
	"context"
	"strings"
	"testing"

	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/mocks"
	"characterllm/internal/prompts"
	"characterllm/internal/search"
)

type mockSearchProvider = mocks.MockSearchProvider

// queueLLM returns a mock LLM client that replays the given responses in call
// order (repeating the last one once exhausted), along with a pointer to the
// call count.
func queueLLM(responses, reasoning []string) (*mocks.MockLLMClient, *int) {
	callCount := 0
	mock := &mocks.MockLLMClient{
		GenerateResponseFn: func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
			callCount++
			idx := callCount - 1
			if idx >= len(responses) {
				idx = len(responses) - 1
			}
			return responses[idx], reasoning[idx], nil
		},
	}
	return mock, &callCount
}

func TestSynthesizer_AnalyzeInput(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxRetries: 1,
			Model:      "test-model",
		},
	}
	ps := &prompts.Set{
		Analyzer: "Analyze this: {{INPUT}}",
	}

	t.Run("analysis_success", func(t *testing.T) {
		mockLLM, _ := queueLLM(
			[]string{`{"official_name": "Test Char", "status": "OK"}`},
			[]string{"reasoning"},
		)
		s := NewSynthesizer(nil, mockLLM, cfg, ps)

		res, resp, reason, err := s.AnalyzeInput(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("AnalyzeInput failed: %v", err)
		}
		if res.OfficialName != "Test Char" {
			t.Errorf("expected Test Char, got %s", res.OfficialName)
		}
		if resp == "" || reason == "" {
			t.Error("expected response and reasoning")
		}
	})

	t.Run("analysis_malformed_json_retry", func(t *testing.T) {
		// First call malformed, second call correct
		mockLLM, callCount := queueLLM(
			[]string{`malformed`, `{"official_name": "Fixed Char", "status": "OK"}`},
			[]string{"r1", "r2"},
		)

		// Need to increase retries for this test
		testCfg := *cfg
		testCfg.LLM.MaxRetries = 2
		s := NewSynthesizer(nil, mockLLM, &testCfg, ps)

		res, _, _, err := s.AnalyzeInput(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("AnalyzeInput failed: %v", err)
		}
		if res.OfficialName != "Fixed Char" {
			t.Errorf("expected Fixed Char, got %s", res.OfficialName)
		}
		if *callCount != 2 {
			t.Errorf("expected 2 calls, got %d", *callCount)
		}
	})
}

func TestSynthesizer_FetchCharacter(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxRetries: 1,
			Model:      "test-model",
		},
		Images: config.ImageConfig{
			MaxResults: 5,
		},
	}
	ps := &prompts.Set{
		Synthesis: "Persona: {{RESULTS}}\n{{SCENARIO_BLOCK}}\n### Output Structure",
	}

	t.Run("fetch_success", func(t *testing.T) {
		mockSearch := &mockSearchProvider{
			Results: []search.SearchResult{{Title: "Wiki", URL: "url", Snippet: "Content"}},
		}
		mockLLM, _ := queueLLM(
			[]string{"STATUS: OK\n### Identity & Temperament\nDetailed spec"},
			[]string{"reasoning"},
		)
		s := NewSynthesizer(mockSearch, mockLLM, cfg, ps)

		analysis := &AnalysisResult{OfficialName: "Test Char"}
		res, err := s.FetchCharacter(context.Background(), analysis)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.Status != "OK" || !strings.Contains(res.PersonaSpec, "Detailed spec") {
			t.Errorf("unexpected synthesis result: %+v", res)
		}
	})

	t.Run("fetch_unknown", func(t *testing.T) {
		mockSearch := &mockSearchProvider{
			Results: []search.SearchResult{}, // No results
		}
		s := NewSynthesizer(mockSearch, nil, cfg, ps)

		analysis := &AnalysisResult{OfficialName: "Unknown Char"}
		res, err := s.FetchCharacter(context.Background(), analysis)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.Status != "UNKNOWN" {
			t.Errorf("expected UNKNOWN status, got %s", res.Status)
		}
	})
}

func TestSynthesizer_ScenarioBlock(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxRetries: 1,
			Model:      "test-model",
		},
	}
	// Mirrors the real synthesis prompt: the placeholder stands alone; the
	// scenario header is supplied by the injected block, not the file.
	ps := &prompts.Set{
		Synthesis: "### Output Structure\n{{SCENARIO_BLOCK}}\n### Input Data\n{{RESULTS}}",
	}

	capturePrompt := func(analysis *AnalysisResult) string {
		mockSearch := &mockSearchProvider{
			Results: []search.SearchResult{{Title: "Wiki", URL: "url", Snippet: "Content"}},
		}
		var captured string
		mockLLM, _ := queueLLM([]string{"### Identity & Temperament\nspec"}, []string{"r"})
		mockLLM.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			captured = msgs[0].Content
			return "### Identity & Temperament\nspec", "r", nil
		}
		s := NewSynthesizer(mockSearch, mockLLM, cfg, ps)
		if _, err := s.FetchCharacter(context.Background(), analysis); err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		return captured
	}

	with := capturePrompt(&AnalysisResult{OfficialName: "Test Char", Scenario: "in a luxury hotel"})
	if strings.Count(with, "### Scenario") != 1 {
		t.Errorf("Expected exactly one '### Scenario' header with a scenario, got %d:\n%s", strings.Count(with, "### Scenario"), with)
	}
	if !strings.Contains(with, "in a luxury hotel") {
		t.Errorf("Expected scenario text in synthesis prompt, got:\n%s", with)
	}

	without := capturePrompt(&AnalysisResult{OfficialName: "Test Char"})
	if strings.Count(without, "### Scenario") != 0 {
		t.Errorf("Expected no '### Scenario' section without a scenario, got:\n%s", without)
	}
}

func TestParseSynthesis_StripsUnrequestedScenario(t *testing.T) {
	s := &SynthesizerClient{}

	t.Run("mid-spec section removed, following sections kept", func(t *testing.T) {
		output := "STATUS: OK\n### Identity & Temperament\nidentity\n\n### Scenario\nin a hotel\n\n### Voice\nvoice"
		res := s.parseSynthesis(output, "")
		want := "### Identity & Temperament\nidentity\n\n### Voice\nvoice"
		if res.PersonaSpec != want {
			t.Errorf("got %q, want %q", res.PersonaSpec, want)
		}
	})

	t.Run("trailing section removed", func(t *testing.T) {
		output := "### Identity & Temperament\nidentity\n\n### Scenario\nin a hotel"
		res := s.parseSynthesis(output, "")
		if res.PersonaSpec != "### Identity & Temperament\nidentity" {
			t.Errorf("got %q", res.PersonaSpec)
		}
	})

	t.Run("no scenario section unchanged", func(t *testing.T) {
		output := "### Identity & Temperament\nidentity\n\n### Voice\nvoice"
		res := s.parseSynthesis(output, "")
		if res.PersonaSpec != output {
			t.Errorf("got %q", res.PersonaSpec)
		}
	})

	t.Run("requested scenario kept", func(t *testing.T) {
		output := "### Identity & Temperament\nidentity\n\n### Scenario\nin a hotel"
		res := s.parseSynthesis(output, "in a hotel")
		if !strings.Contains(res.PersonaSpec, "### Scenario") {
			t.Errorf("expected scenario section to be kept, got %q", res.PersonaSpec)
		}
	})
}
