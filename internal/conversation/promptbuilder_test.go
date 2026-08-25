package conversation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"characterllm/internal/llm"
)

func TestBuild_BelowSoftTargetNotTriggered(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 1000
	f.cfg.LLM.CompactionThreshold = 0.9
	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return 10 }
	f.seed(t, 10, "Msg %d")
	f.saveCurrent(t)

	details, err := f.sm.GetCharacterDetails(ctx, testGuildID)
	if err != nil {
		t.Fatal(err)
	}

	userTokens := f.llm.EstimateTokens(ctx, []llm.Message{{Role: "user", Content: "current"}})
	messages, compactionNeeded, err := NewPromptBuilder(f.llm, f.sm, f.cfg, f.ps).
		Build(ctx, testGuildID, testThread, details, "current", userTokens)
	if err != nil {
		t.Fatal(err)
	}

	if compactionNeeded {
		t.Error("Expected no compaction trigger below the soft target")
	}
	// system + 10 history + current
	if len(messages) != 12 {
		t.Fatalf("Expected 12 messages (system + 10 history + current), got %d", len(messages))
	}
	if strings.Contains(messages[0].Content, "summary of earlier turns") {
		t.Error("Expected no summary pointer in system prompt when no summary exists")
	}
}

func TestBuild_SoftTargetTriggersWithoutTruncation(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	// Soft target is tiny (10% of 1000), but the full history fits MaxContext.
	f.cfg.LLM.MaxContext = 1000
	f.cfg.LLM.CompactionThreshold = 0.1
	f.cfg.LLM.RecentMemoryWindow = 2
	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return len(msgs) * 20 }
	f.seed(t, 10, "Msg %d")
	f.saveCurrent(t)

	details, err := f.sm.GetCharacterDetails(ctx, testGuildID)
	if err != nil {
		t.Fatal(err)
	}

	userTokens := f.llm.EstimateTokens(ctx, []llm.Message{{Role: "user", Content: "current"}})
	messages, compactionNeeded, err := NewPromptBuilder(f.llm, f.sm, f.cfg, f.ps).
		Build(ctx, testGuildID, testThread, details, "current", userTokens)
	if err != nil {
		t.Fatal(err)
	}

	// All 10 stored messages must be in the prompt even though the prompt
	// (240 tokens) far exceeds the soft target (100 tokens): system + 10 history + current.
	if len(messages) != 12 {
		t.Fatalf("Expected 12 messages in prompt (system + 10 history + current), got %d", len(messages))
	}
	for i := 0; i < 10; i++ {
		want := fmt.Sprintf("Msg %d", i)
		if messages[1+i].Content != want {
			t.Errorf("Expected history message %d to be %q, got %q", i, want, messages[1+i].Content)
		}
	}

	if !compactionNeeded {
		t.Error("Expected exceeding the soft target to trigger compaction")
	}
}

func TestBuild_HardCapTruncatesOldest(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 100
	f.cfg.LLM.CompactionThreshold = 0.9
	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return len(msgs) * 20 }
	f.seed(t, 10, "Msg %d")
	f.saveCurrent(t)

	details, err := f.sm.GetCharacterDetails(ctx, testGuildID)
	if err != nil {
		t.Fatal(err)
	}

	userTokens := f.llm.EstimateTokens(ctx, []llm.Message{{Role: "user", Content: "current"}})
	messages, compactionNeeded, err := NewPromptBuilder(f.llm, f.sm, f.cfg, f.ps).
		Build(ctx, testGuildID, testThread, details, "current", userTokens)
	if err != nil {
		t.Fatal(err)
	}

	// system (20) + current (20) leave 60 tokens: exactly 3 history messages fit,
	// and the newest must be the ones kept.
	if len(messages) != 5 {
		t.Fatalf("Expected 5 messages (system + 3 history + current), got %d", len(messages))
	}
	for i, want := range []string{"Msg 7", "Msg 8", "Msg 9"} {
		if messages[1+i].Content != want {
			t.Errorf("Expected history slot %d to be %q, got %q", i, want, messages[1+i].Content)
		}
	}

	if !compactionNeeded {
		t.Error("Expected truncation at the hard cap to trigger compaction")
	}
}

func TestBuild_SummaryIncludedBeforeHistory(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return 10 }
	f.seed(t, 6, "Msg %d")
	f.saveCurrent(t)

	const summary = "SUMMARY_TEXT"
	if err := f.sm.PruneAndSummarize(ctx, testGuildID, testThread, summary, 0, 10); err != nil {
		t.Fatal(err)
	}

	details, err := f.sm.GetCharacterDetails(ctx, testGuildID)
	if err != nil {
		t.Fatal(err)
	}

	userTokens := f.llm.EstimateTokens(ctx, []llm.Message{{Role: "user", Content: "current"}})
	messages, _, err := NewPromptBuilder(f.llm, f.sm, f.cfg, f.ps).
		Build(ctx, testGuildID, testThread, details, "current", userTokens)
	if err != nil {
		t.Fatal(err)
	}

	// system + summary + 6 history + current
	if len(messages) != 9 {
		t.Fatalf("Expected 9 messages (system + summary + 6 history + current), got %d", len(messages))
	}
	wantSummary := "Summary of the earlier part of this conversation:\n" + summary
	if messages[1].Role != "user" || messages[1].Content != wantSummary {
		t.Errorf("Expected framed summary as second message, got role=%q content=%q", messages[1].Role, messages[1].Content)
	}
	if !strings.Contains(messages[0].Content, "summary of earlier turns") {
		t.Errorf("Expected system prompt to reference the summary, got: %s", messages[0].Content)
	}
	if messages[2].Content != "Msg 0" || messages[7].Content != "Msg 5" {
		t.Errorf("Expected history after the summary, got %q ... %q", messages[2].Content, messages[7].Content)
	}
}
