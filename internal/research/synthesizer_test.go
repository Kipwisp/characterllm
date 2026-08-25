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
