package conversation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"characterllm/internal/llm"
)

func TestCompact_BudgetCapping(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 100
	f.cfg.LLM.RecentMemoryWindow = 2
	f.cfg.LLM.SummaryMaxTokens = 20

	// Each message is 20 tokens.
	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int {
		return len(msgs) * 20
	}
	f.seed(t, 20, "Msg %d")

	var capturedMessages []llm.Message
	f.llm.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
		capturedMessages = msgs
		return "Summary", "Reasoning", nil
	}

	NewCompactor(f.llm, f.sm, f.cfg, f.audit, f.ps).Compact(ctx, testGuildID, testThread, "char1", "req1")

	// budget = 100 (max context) - 20 (compact prompt) - 20 (summary cap) = 60.
	// Each message is 20. So at most 3 messages are summarized (plus the system prompt).
	if len(capturedMessages) > 4 {
		t.Errorf("Expected budget capping to limit messages, got %d messages", len(capturedMessages))
	}

	// The configured summary cap (20 tokens) must be injected into the prompt as a word limit.
	if !strings.Contains(capturedMessages[0].Text(), "15 words") {
		t.Errorf("Expected injected length limit of '15 words' in compaction prompt, got: %s", capturedMessages[0].Text())
	}
}

func TestCompact_PruningEffect(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 100
	f.cfg.LLM.RecentMemoryWindow = 2
	f.cfg.LLM.SummaryMaxTokens = 20

	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return 20 }
	f.seed(t, 20, "Msg %d")
	initialCount, err := f.sm.GetHistoryCount(ctx, testGuildID, testThread)
	if err != nil {
		t.Fatal(err)
	}

	f.llm.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
		return "Summary", "Reasoning", nil
	}

	NewCompactor(f.llm, f.sm, f.cfg, f.audit, f.ps).Compact(ctx, testGuildID, testThread, "char1", "req1")

	finalCount, err := f.sm.GetHistoryCount(ctx, testGuildID, testThread)
	if err != nil {
		t.Fatal(err)
	}
	if finalCount >= initialCount {
		t.Errorf("Expected history to be pruned, but count stayed %d", finalCount)
	}
	summary, err := f.sm.GetSummary(ctx, testGuildID, testThread)
	if err != nil {
		t.Fatal(err)
	}
	if summary == "" {
		t.Error("Expected a summary to be stored after compaction")
	}
}

func TestCompact_IncludesPreviousSummary(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 100
	f.cfg.LLM.RecentMemoryWindow = 2
	f.cfg.LLM.SummaryMaxTokens = 20

	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return 20 }
	f.seed(t, 6, "Msg %d")

	var mu sync.Mutex
	var prompts [][]llm.Message
	f.llm.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
		mu.Lock()
		captured := make([]llm.Message, len(msgs))
		copy(captured, msgs)
		prompts = append(prompts, captured)
		mu.Unlock()
		return "Summary1", "Reasoning", nil
	}

	c := NewCompactor(f.llm, f.sm, f.cfg, f.audit, f.ps)
	c.Compact(ctx, testGuildID, testThread, "char1", "req1")

	// Grow the history again and compact a second time.
	f.seed(t, 4, "More %d")
	c.Compact(ctx, testGuildID, testThread, "char1", "req2")

	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("Expected 2 compaction prompts, got %d", len(prompts))
	}
	// Re-summarization must include the previous summary so no information is lost.
	if !strings.Contains(fmt.Sprintf("%v", prompts[1]), "Previous conversation summary:\nSummary1") {
		t.Errorf("Expected second compaction prompt to include the previous summary, got: %v", prompts[1])
	}
}

func TestCompact_ConcurrentGuard(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 100
	f.cfg.LLM.RecentMemoryWindow = 2
	f.cfg.LLM.SummaryMaxTokens = 20

	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return 20 }
	f.seed(t, 10, "Msg %d")

	c := NewCompactor(f.llm, f.sm, f.cfg, f.audit, f.ps)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	generationCalls := 0
	f.llm.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
		generationCalls++
		entered <- struct{}{}
		<-release
		return "Summary", "Reasoning", nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.Compact(ctx, testGuildID, testThread, "char1", "req1")
	}()

	// Wait until the first compaction holds the slot
	<-entered

	// Second compaction must skip without calling the LLM
	c.Compact(ctx, testGuildID, testThread, "char1", "req2")
	if generationCalls != 1 {
		t.Errorf("Expected exactly 1 LLM generation call, got %d", generationCalls)
	}

	close(release)
	wg.Wait()

	// Slot must be released: a subsequent compaction can run
	f.seed(t, 10, "New %d")
	entered2 := make(chan struct{}, 1)
	f.llm.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
		entered2 <- struct{}{}
		return "Summary2", "Reasoning", nil
	}
	c.Compact(ctx, testGuildID, testThread, "char1", "req3")
	select {
	case <-entered2:
	default:
		t.Error("Expected compaction slot to be released after first compaction finished")
	}
}

func TestCompact_NoopWithoutOlderTurns(t *testing.T) {
	f := setupConversation(t)
	ctx := context.Background()
	f.cfg.LLM.MaxContext = 100
	f.cfg.LLM.RecentMemoryWindow = 2
	f.cfg.LLM.SummaryMaxTokens = 20

	f.llm.EstimateTokensFn = func(ctx context.Context, msgs []llm.Message) int { return 20 }
	f.seed(t, 2, "Msg %d") // nothing older than the recent window

	calls := 0
	f.llm.GenerateResponseFn = func(ctx context.Context, msgs []llm.Message, model string) (string, string, error) {
		calls++
		return "Summary", "Reasoning", nil
	}

	NewCompactor(f.llm, f.sm, f.cfg, f.audit, f.ps).Compact(ctx, testGuildID, testThread, "char1", "req1")

	if calls != 0 {
		t.Errorf("Expected no LLM call when nothing is older than the window, got %d", calls)
	}
	summary, err := f.sm.GetSummary(ctx, testGuildID, testThread)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		t.Errorf("Expected no summary after a no-op compaction, got %q", summary)
	}
}
