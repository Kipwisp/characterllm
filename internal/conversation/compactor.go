package conversation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/prompts"
	"characterllm/internal/session"
)

// tokensPerWord is the heuristic used to render the token cap as a word limit in the
// compaction prompt (standard English figure: ~4 chars/token, ~5 chars/word).
const tokensPerWord = 53.0 / 40.0

// Compactor condenses the oldest turns of a conversation into a rolling summary.
// At most one compaction runs per (guild, thread) at a time.
type Compactor struct {
	llm        llm.LLMClient
	session    *session.Manager
	cfg        *config.Config
	audit      *audit.AuditLogger
	prompts    *prompts.Set
	compacting sync.Map // key: guildID + "|" + threadID -> struct{}, one in-flight compaction per conversation
}

// NewCompactor creates a Compactor with the provided dependencies.
func NewCompactor(llm llm.LLMClient, session *session.Manager, cfg *config.Config, audit *audit.AuditLogger, ps *prompts.Set) *Compactor {
	return &Compactor{
		llm:     llm,
		session: session,
		cfg:     cfg,
		audit:   audit,
		prompts: ps,
	}
}

// Compact condenses the oldest turns of a conversation into a rolling summary,
// preserving a recent memory window. It is invoked when stored history no
// longer fits the prompt budget. It is a no-op if a compaction is already in
// flight for the conversation.
func (c *Compactor) Compact(ctx context.Context, guildID, threadID, charID, reqID string) {
	if !c.tryStartCompaction(guildID, threadID) {
		logger.FromContext(ctx).Info("compaction already in progress, skipping")
		return
	}
	defer c.endCompaction(guildID, threadID)

	logger.FromContext(ctx).Info("stored history exceeds prompt budget, initiating compaction")

	total, err := c.session.GetHistoryCount(ctx, guildID, threadID)
	if err != nil {
		logger.FromContext(ctx).Error("error getting history count", "error", err)
		return
	}

	// Fetch all turns except the most recent window.
	recentWindowSize := c.cfg.LLM.RecentMemoryWindow
	olderTurns, err := c.session.GetHistory(ctx, guildID, threadID, total-recentWindowSize, 0)
	if err != nil {
		logger.FromContext(ctx).Error("error fetching older history for compaction", "error", err)
		return
	}

	if len(olderTurns) == 0 {
		// If there are no older turns to summarize, we don't need to compact.
		return
	}

	// Compaction Budgeting (Safety)
	promptMsg := llm.TextMessage(llm.RoleSystem, c.compactionPrompt())

	// Reserve room for the summary output so the full request fits the context window
	maxContext := c.cfg.LLM.MaxContext
	budget := maxContext - c.llm.EstimateTokens(ctx, []llm.Message{promptMsg}) - c.cfg.LLM.SummaryMaxTokens

	// Include the previous rolling summary so no information is lost when re-summarizing
	var messagesToSummarize []llm.Message
	currentSum := 0
	summary, err := c.session.GetSummary(ctx, guildID, threadID)
	if err != nil {
		logger.FromContext(ctx).Error("error getting previous summary for compaction", "error", err)
		return
	}
	if summary != "" {
		summaryMsg := llm.TextMessage(llm.RoleUser, "Previous conversation summary:\n"+summary)
		summaryMsgTokens := c.llm.EstimateTokens(ctx, []llm.Message{summaryMsg})
		if summaryMsgTokens <= budget {
			messagesToSummarize = append(messagesToSummarize, summaryMsg)
			currentSum += summaryMsgTokens
		}
	}

	// Walk oldest-first so the most recent older turns are the first to be dropped
	// if the budget runs out.
	selected, _, _ := fitWithinBudget(ctx, c.llm, olderTurns, budget, currentSum)
	messagesToSummarize = append(messagesToSummarize, selected...)
	prunedCount := len(selected)

	if prunedCount == 0 {
		logger.FromContext(ctx).Warn("compaction budget too small to summarize any messages", "budget", budget)
		return
	}

	// Execution & Rebuild
	summaryPrompt := append([]llm.Message{promptMsg}, messagesToSummarize...)
	start := time.Now()
	summary, reasoning, err := c.llm.GenerateResponse(ctx, summaryPrompt, c.cfg.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("error during history compaction generation", "error", err)
		return
	}
	latency := time.Since(start)

	summaryTokens := c.llm.EstimateTokens(ctx, []llm.Message{llm.TextMessage(llm.RoleUser, summary)})
	if summaryTokens > c.cfg.LLM.SummaryMaxTokens {
		logger.FromContext(ctx).Warn("summary exceeds configured token cap", "summary_tokens", summaryTokens, "cap", c.cfg.LLM.SummaryMaxTokens)
	}
	if err := c.session.PruneAndSummarize(ctx, guildID, threadID, summary, prunedCount); err != nil {
		logger.FromContext(ctx).Error("error pruning history", "error", err)
		return
	}
	logger.FromContext(ctx).Info("history compacted successfully", "messages_pruned", prunedCount)

	// Log the compaction reasoning
	c.audit.Log(ctx, guildID, threadID, charID, reqID, audit.Turn{
		Kind:      audit.KindCompaction,
		Model:     c.cfg.LLM.Model,
		Latency:   latency,
		Prompt:    fmt.Sprintf("rolling summary of %d messages", prunedCount),
		Reasoning: reasoning,
		Response:  summary,
	})
}

// compactionPrompt injects the configured summary length limit into the cached template.
func (c *Compactor) compactionPrompt() string {
	maxWords := int(float64(c.cfg.LLM.SummaryMaxTokens) / tokensPerWord)
	return strings.Replace(c.prompts.Compaction, "{{LENGTH_LIMIT}}", fmt.Sprintf("%d words", maxWords), 1)
}

// tryStartCompaction claims the compaction slot for a conversation, returning false
// if a compaction is already in flight.
func (c *Compactor) tryStartCompaction(guildID, threadID string) bool {
	key := guildID + "|" + threadID
	_, loaded := c.compacting.LoadOrStore(key, struct{}{})
	return !loaded
}

func (c *Compactor) endCompaction(guildID, threadID string) {
	c.compacting.Delete(guildID + "|" + threadID)
}
