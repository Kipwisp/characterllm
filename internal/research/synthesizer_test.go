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
		Search: config.SearchConfig{
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
		res, err := s.FetchCharacter(context.Background(), analysis, nil, 0)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.Status != SynthesisStatusOK || !strings.Contains(res.PersonaSpec, "Detailed spec") {
			t.Errorf("unexpected synthesis result: %+v", res)
		}
	})

	t.Run("fetch_unknown", func(t *testing.T) {
		mockSearch := &mockSearchProvider{
			Results: []search.SearchResult{}, // No results
		}
		s := NewSynthesizer(mockSearch, nil, cfg, ps)

		analysis := &AnalysisResult{OfficialName: "Unknown Char"}
		res, err := s.FetchCharacter(context.Background(), analysis, nil, 0)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.Status != SynthesisStatusUnknown {
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
	// Mirrors the real synthesis prompt: the placeholders stand alone; the
	// block headers are supplied by the injected blocks, not the file.
	ps := &prompts.Set{
		Synthesis: "### Output Structure\n{{MODIFIERS_BLOCK}}\n\n{{SCENARIO_BLOCK}}\n### Input Data\n{{RESULTS}}",
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
		if _, err := s.FetchCharacter(context.Background(), analysis, nil, 0); err != nil {
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

	withModifiers := capturePrompt(&AnalysisResult{OfficialName: "Test Char", Modifiers: []string{"young", "grumpy"}})
	if strings.Count(withModifiers, "### Modifiers") != 1 {
		t.Errorf("Expected exactly one '### Modifiers' header with modifiers, got %d:\n%s", strings.Count(withModifiers, "### Modifiers"), withModifiers)
	}
	if !strings.Contains(withModifiers, "Modifiers: young, grumpy") {
		t.Errorf("Expected modifiers text in synthesis prompt, got:\n%s", withModifiers)
	}

	without := capturePrompt(&AnalysisResult{OfficialName: "Test Char"})
	if strings.Count(without, "### Scenario") != 0 {
		t.Errorf("Expected no '### Scenario' section without a scenario, got:\n%s", without)
	}
	if strings.Count(without, "### Modifiers") != 0 {
		t.Errorf("Expected no '### Modifiers' section without modifiers, got:\n%s", without)
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

// Mirrors the real edit-section prompt's request block: the labels are
// supplied by the injected placeholder values, not the file.
const editPromptFixture = "### Request\n{{CHARACTER_BLOCK}}\n{{SERIES_BLOCK}}\n{{CONTEXT_BLOCK}}\n{{TARGET_BLOCK}}\n{{INSTRUCTION_BLOCK}}\n{{SECTION_REFERENCE}}"

func TestSynthesizer_RewriteSection(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxRetries: 1,
			Model:      "test-model",
		},
	}
	// Mirrors the real edit-section prompt: the request block is filled from
	// placeholders; the block labels are supplied by the injected values.
	ps := &prompts.Set{EditSection: editPromptFixture}

	spec := "### Identity & Temperament\nCold and questioning.\n\n### Appearance\nHuman.\n\n### Voice & Habits\nSlow cadence, dry wit.\n"

	var capturedSystem, capturedPrompt string
	mockLLM := &mocks.MockLLMClient{
		GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			capturedSystem = msgs[0].Content
			capturedPrompt = msgs[1].Content
			return "### Voice & Habits\nFast cadence, warm wit.", "rewrote it", nil
		},
	}
	s := NewSynthesizer(nil, mockLLM, cfg, ps)

	res, err := s.RewriteSection(context.Background(), SectionRewriteRequest{
		DisplayName:  "Miles Morales",
		OfficialName: "Miles G. Morales",
		Series:       "Spider-Man",
		Spec:         spec,
		Section:      SectionVoice,
		CurrentBody:  "Slow cadence, dry wit.",
		Instruction:  "make him sound warmer",
	})
	if err != nil {
		t.Fatalf("RewriteSection failed: %v", err)
	}

	if capturedSystem != editPromptFixture {
		t.Errorf("system message must be the stored edit-section prompt, got %q", capturedSystem)
	}
	for _, want := range []string{
		"Character: Miles Morales (official name: Miles G. Morales)",
		"Series: Spider-Man",
		"Rest of the persona specification",
		"Cold and questioning",
		"Current content:\nSlow cadence, dry wit.",
		"Instruction: make him sound warmer",
	} {
		if !strings.Contains(capturedPrompt, want) {
			t.Errorf("user prompt missing %q:\n%s", want, capturedPrompt)
		}
	}
	if strings.Contains(capturedPrompt, "### Voice & Habits") {
		t.Errorf("target section must be removed from the context block:\n%s", capturedPrompt)
	}
	if res.Body != "Fast cadence, warm wit." {
		t.Errorf("Body = %q (leading headers must be stripped)", res.Body)
	}
	if res.Reasoning != "rewrote it" || res.Response == "" || res.Prompt != capturedPrompt {
		t.Errorf("raw exchange not captured: %+v", res)
	}

	t.Run("Empty response is an error", func(t *testing.T) {
		mockLLM.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			return "### Voice & Habits\n", "empty", nil
		}
		if _, err := s.RewriteSection(context.Background(), SectionRewriteRequest{
			DisplayName: "Miles Morales",
			Section:     SectionVoice,
			Instruction: "anything",
		}); err == nil {
			t.Error("Expected error for empty section body")
		}
	})

	t.Run("Whole persona rewrite returns the full spec", func(t *testing.T) {
		mockLLM.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			capturedPrompt = msgs[1].Content
			return "### Identity & Temperament\nPerpetually upbeat.\n\n### Appearance\nHuman.\n\n### Voice & Habits\nBright cadence.\n\n### Example Dialogue\n<START>\nUser: Hi\nCharacter: Hey!\n<END>\n", "ok", nil
		}
		res, err := s.RewriteSection(context.Background(), SectionRewriteRequest{
			DisplayName:  "Miles Morales",
			Spec:         spec + "\n### Example Dialogue\n<START>\n",
			Instruction:  "he is always happy",
			WholePersona: true,
		})
		if err != nil {
			t.Fatalf("RewriteSection failed: %v", err)
		}
		for _, section := range []string{SectionIdentity, SectionAppearance, SectionVoice, SectionDialogue} {
			if _, ok := ExtractSection(res.Body, section); !ok {
				t.Errorf("section %s missing from returned spec:\n%s", section, res.Body)
			}
		}
		if !strings.Contains(res.Body, "Perpetually upbeat") {
			t.Errorf("rewritten content missing from spec:\n%s", res.Body)
		}
		if !strings.Contains(capturedPrompt, "Mode: Whole-Persona") || !strings.Contains(capturedPrompt, "he is always happy") {
			t.Errorf("whole-persona prompt missing context:\n%s", capturedPrompt)
		}
		// The section reference covers every known section, including the
		// canned Scenario definition.
		if !strings.Contains(capturedPrompt, "### Section Reference\n### Scenario\n") ||
			!strings.Contains(capturedPrompt, "temporary context, not a permanent trait") {
			t.Errorf("whole-persona prompt missing the Scenario reference:\n%s", capturedPrompt)
		}
	})

	t.Run("Whole persona rewrite dropping a core header is rejected", func(t *testing.T) {
		mockLLM.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			// Appearance section is missing entirely.
			return "### Identity & Temperament\nPerpetually upbeat.\n\n### Voice & Habits\nBright cadence.\n\n### Example Dialogue\n<START>\n", "ok", nil
		}
		if _, err := s.RewriteSection(context.Background(), SectionRewriteRequest{
			DisplayName:  "Miles Morales",
			Spec:         spec,
			Instruction:  "he is always happy",
			WholePersona: true,
		}); err == nil {
			t.Error("Expected error when a core section header is dropped")
		}
	})
}

func TestSynthesizer_RewriteSection_InjectsSectionReference(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{MaxRetries: 1, Model: "test-model"}}
	synthesis := "### Output Structure\n\n### Greeting\n[Write the opening line. No quotation marks, no emojis.]\n\n{{AVATAR_BLOCK}}\n\n### Critical Constraints\n- x\n"
	ps := &prompts.Set{
		Synthesis:   synthesis,
		EditSection: "{{SECTION_REFERENCE}}\n### Request\n{{TARGET_BLOCK}}\n{{INSTRUCTION_BLOCK}}",
	}
	var capturedPrompt string
	mockLLM := &mocks.MockLLMClient{
		GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			capturedPrompt = msgs[1].Content
			return "Hey, it's me.", "ok", nil
		},
	}
	s := NewSynthesizer(nil, mockLLM, cfg, ps)

	if _, err := s.RewriteSection(context.Background(), SectionRewriteRequest{
		DisplayName: "Miles",
		Spec:        "### Greeting\nold",
		Section:     SectionGreeting,
		CurrentBody: "old",
		Instruction: "warm it up",
	}); err != nil {
		t.Fatalf("RewriteSection failed: %v", err)
	}
	if !strings.Contains(capturedPrompt, "### Greeting\n[Write the opening line. No quotation marks, no emojis.]") {
		t.Errorf("expected the greeting definition fetched from the synthesis prompt:\n%s", capturedPrompt)
	}
	if strings.Contains(capturedPrompt, "{{AVATAR_BLOCK}}") {
		t.Errorf("placeholder must be stripped from the fetched definition:\n%s", capturedPrompt)
	}
}

func TestSynthesizer_RewriteSection_ScenarioReferenceIsCanned(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{MaxRetries: 1, Model: "test-model"}}
	// The synthesis template has no literal "### Scenario" section (it is a
	// conditional block), so the Scenario reference must come from the canned
	// definition.
	synthesis := "### Output Structure\n\n### Greeting\n[opening line]\n\n### Critical Constraints\n- x\n"
	ps := &prompts.Set{
		Synthesis:   synthesis,
		EditSection: "{{SECTION_REFERENCE}}\n### Request\n{{TARGET_BLOCK}}\n{{INSTRUCTION_BLOCK}}",
	}
	var capturedPrompt string
	mockLLM := &mocks.MockLLMClient{
		GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
			capturedPrompt = msgs[1].Content
			return "Stuck in a rainy city.", "ok", nil
		},
	}
	s := NewSynthesizer(nil, mockLLM, cfg, ps)

	if _, err := s.RewriteSection(context.Background(), SectionRewriteRequest{
		DisplayName: "Miles",
		Spec:        "### Scenario\nA rainy city.",
		Section:     SectionScenario,
		CurrentBody: "A rainy city.",
		Instruction: "make it sadder",
	}); err != nil {
		t.Fatalf("RewriteSection failed: %v", err)
	}
	if !strings.Contains(capturedPrompt, "### Section Reference\n### Scenario\n") {
		t.Errorf("expected the canned Scenario reference under its header:\n%s", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, "temporary context, not a permanent trait") {
		t.Errorf("expected the canned Scenario definition text:\n%s", capturedPrompt)
	}
}

func TestStripSectionFormatting(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain body", "plain body"},
		{"### Voice & Habits\nrewritten body", "rewritten body"},
		{"```\nbody line\n```", "body line"},
		{"### Header\nmore body", "more body"},
		{"### Header Only", ""},
		{"\n\n  padded  \n\n", "padded"},
	}
	for _, tt := range tests {
		if got := stripSectionFormatting(tt.in); got != tt.want {
			t.Errorf("stripSectionFormatting(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestSynthesizer_AvatarPick(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxRetries: 1,
			Model:      "test-model",
		},
	}
	ps := &prompts.Set{
		Synthesis: "### Output Structure\n{{MODIFIERS_BLOCK}}\n\n{{SCENARIO_BLOCK}}\n\n{{AVATAR_BLOCK}}\n### Input Data\n{{RESULTS}}",
	}
	mockSearch := &mockSearchProvider{
		Results: []search.SearchResult{{Title: "Wiki", URL: "url", Snippet: "Content"}},
	}
	analysis := &AnalysisResult{OfficialName: "Test Char"}

	t.Run("pick_parsed_and_stripped", func(t *testing.T) {
		var capturedImages []string
		var capturedPrompt string
		mockLLM := &mocks.MockLLMClient{
			GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
				capturedImages = msgs[0].Images
				capturedPrompt = msgs[0].Content
				return "AVATAR: 3\n### Identity & Temperament\nDetailed spec", "r", nil
			},
		}
		s := NewSynthesizer(mockSearch, mockLLM, cfg, ps)
		res, err := s.FetchCharacter(context.Background(), analysis, []string{"data:image/png;base64,row"}, 3)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.AvatarChoice != 3 {
			t.Errorf("expected AvatarChoice 3, got %d", res.AvatarChoice)
		}
		if strings.Contains(res.PersonaSpec, "AVATAR") {
			t.Errorf("avatar line leaked into persona spec: %q", res.PersonaSpec)
		}
		if len(capturedImages) != 1 || capturedImages[0] != "data:image/png;base64,row" {
			t.Errorf("expected the row data URI on the message, got %v", capturedImages)
		}
		if !strings.Contains(capturedPrompt, "### Avatar Selection") || !strings.Contains(capturedPrompt, "numbered 1 to 3") {
			t.Errorf("expected avatar block in prompt, got:\n%s", capturedPrompt)
		}
		if !strings.Contains(capturedPrompt, "small Discord profile picture") {
			t.Errorf("expected the Discord-avatar suitability criterion in the prompt, got:\n%s", capturedPrompt)
		}
	})

	t.Run("no_images_no_block", func(t *testing.T) {
		var capturedImages []string
		var capturedPrompt string
		mockLLM := &mocks.MockLLMClient{
			GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
				capturedImages = msgs[0].Images
				capturedPrompt = msgs[0].Content
				return "### Identity & Temperament\nDetailed spec", "r", nil
			},
		}
		s := NewSynthesizer(mockSearch, mockLLM, cfg, ps)
		res, err := s.FetchCharacter(context.Background(), analysis, nil, 0)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.AvatarChoice != 0 {
			t.Errorf("expected AvatarChoice 0 without images, got %d", res.AvatarChoice)
		}
		if len(capturedImages) != 0 {
			t.Errorf("expected no images on the message, got %v", capturedImages)
		}
		if strings.Contains(capturedPrompt, "### Avatar Selection") {
			t.Errorf("avatar block must not be present without images, got:\n%s", capturedPrompt)
		}
	})

	t.Run("out_of_range_is_zero", func(t *testing.T) {
		mockLLM := &mocks.MockLLMClient{
			GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
				return "AVATAR: 99\n### Identity & Temperament\nspec", "r", nil
			},
		}
		s := NewSynthesizer(mockSearch, mockLLM, cfg, ps)
		res, err := s.FetchCharacter(context.Background(), analysis, []string{"row"}, 2)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.AvatarChoice != 0 {
			t.Errorf("expected out-of-range pick to be 0, got %d", res.AvatarChoice)
		}
		if strings.Contains(res.PersonaSpec, "AVATAR") {
			t.Errorf("avatar line leaked into persona spec: %q", res.PersonaSpec)
		}
	})

	t.Run("no_line_is_zero", func(t *testing.T) {
		mockLLM := &mocks.MockLLMClient{
			GenerateResponseFn: func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
				return "### Identity & Temperament\nspec", "r", nil
			},
		}
		s := NewSynthesizer(mockSearch, mockLLM, cfg, ps)
		res, err := s.FetchCharacter(context.Background(), analysis, []string{"row"}, 1)
		if err != nil {
			t.Fatalf("FetchCharacter failed: %v", err)
		}
		if res.AvatarChoice != 0 {
			t.Errorf("expected AvatarChoice 0 with no AVATAR line, got %d", res.AvatarChoice)
		}
	})
}

func TestRemoveAvatarLine(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		choice int
		out    string
	}{
		{"clean", "AVATAR: 2\n### Identity & Temperament\nbody", 2, "### Identity & Temperament\nbody"},
		{"lowercase", "avatar: 1\nspec", 1, "spec"},
		{"trailing_prose", "AVATAR: 2 (best match)\nspec", 2, "spec"},
		{"no_digits", "AVATAR: abc\nspec", 0, "spec"},
		{"absent", "spec", 0, "spec"},
		{"mid_output", "### Identity & Temperament\nAVATAR: 4\nbody", 4, "### Identity & Temperament\nbody"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, choice := removeAvatarLine(tt.in)
			if choice != tt.choice {
				t.Errorf("expected choice %d, got %d", tt.choice, choice)
			}
			if out != tt.out {
				t.Errorf("expected %q, got %q", tt.out, out)
			}
		})
	}
}
