package conversation

import (
	"context"
	"fmt"
	"strings"

	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/prompts"
	"characterllm/internal/session"
)

// historyPageSize is the number of history messages fetched and estimated per batch.
const historyPageSize = 20

// summaryPointer is injected at the {{SUMMARY_CONTEXT}} placeholder of the system prompt
// when a rolling summary is present; it instructs the model how to interpret the summary message.
const summaryPointer = "The first user message in this conversation is a summary of earlier turns, provided for continuity. Treat it as context, not as something the user just said."

// PromptBuilder assembles the outgoing LLM prompt (system prompt, rolling summary,
// history, current message) under the context budget.
type PromptBuilder struct {
	llm     llm.LLMClient
	session *session.Manager
	cfg     *config.Config
	prompts *prompts.Set
}

// NewPromptBuilder creates a PromptBuilder with the provided dependencies.
func NewPromptBuilder(llm llm.LLMClient, session *session.Manager, cfg *config.Config, ps *prompts.Set) *PromptBuilder {
	return &PromptBuilder{llm: llm, session: session, cfg: cfg, prompts: ps}
}

// buildSystemPrompt constructs the system message by injecting the character
// identity and persona and, when a rolling summary is present, the summary
// pointer into the cached template.
func (p *PromptBuilder) buildSystemPrompt(details *session.CharacterDetails, hasSummary bool) llm.Message {
	name := details.OfficialName
	if name == "" {
		name = details.DisplayName
	}
	identity := "You are the character named " + name
	if details.Series != "" {
		identity += " from the series " + details.Series
	}
	identity += "."
	detailsBlock := identity + "\n\n" + details.Description
	content := strings.Replace(p.prompts.System, "{{CHARACTER_DETAILS}}", detailsBlock, 1)
	notice := ""
	if hasSummary {
		notice = "\n\n### Summary\n" + summaryPointer
	}
	content = strings.Replace(content, "{{SUMMARY_CONTEXT}}", notice, 1)
	return llm.TextMessage(llm.RoleSystem, content)
}

// Build assembles the full outgoing prompt: system prompt, rolling summary
// (if any), the stored history, and the current user message. History
// is only truncated when it cannot fit the model's context window; exceeding
// the compaction target does not truncate.
// The boolean result is true when the prompt exceeded the compaction target
// (or history had to be truncated), triggering compaction.
func (p *PromptBuilder) Build(ctx context.Context, guildID, threadID string, details *session.CharacterDetails, user llm.Message, userTokens int) ([]llm.Message, bool, error) {
	// Fetch the rolling summary once so the system notice and the summary message stay in sync
	summary, err := p.session.GetSummary(ctx, guildID, threadID)
	if err != nil {
		return nil, false, fmt.Errorf("get conversation summary: %w", err)
	}

	messages := []llm.Message{p.buildSystemPrompt(details, summary != "")}
	if summary != "" {
		messages = append(messages, llm.TextMessage(llm.RoleUser, "Summary of the earlier part of this conversation:\n"+summary))
	}

	total, err := p.session.GetHistoryCount(ctx, guildID, threadID)
	if err != nil {
		return nil, false, fmt.Errorf("get history count: %w", err)
	}

	// The hard cap is the model's context window; the compaction target is soft and
	// only decides when to compact, never what to include in this turn's prompt.
	currentTokens := p.llm.EstimateTokens(ctx, messages) + userTokens
	hardBudget := p.cfg.LLM.MaxContext
	softBudget := int(float64(hardBudget) * p.cfg.LLM.CompactionThreshold)

	// The current message was saved before assembly and is the newest stored row;
	// retrieve only the history preceding it, since it is appended explicitly below.
	stored, err := p.session.GetHistory(ctx, guildID, threadID, total-1, 0)
	if err != nil {
		return nil, false, fmt.Errorf("retrieve history: %w", err)
	}

	// Prefer the newest messages: walk the history newest-first and keep what fits.
	newestFirst := make([]llm.Message, len(stored))
	for i, msg := range stored {
		newestFirst[len(stored)-1-i] = msg
	}
	history, promptTokens, hardTruncated := fitWithinBudget(ctx, p.llm, newestFirst, hardBudget, currentTokens)
	reverseMessages(history)

	messages = append(messages, history...)
	messages = append(messages, user)

	compactionNeeded := hardTruncated || promptTokens > softBudget
	return messages, compactionNeeded, nil
}

// fitWithinBudget returns the longest prefix of msgs, in the order given, that fits
// within budget given tokens already accounted for, using batched estimates per
// historyPageSize chunk. The boolean result is true when msgs was left behind.
func fitWithinBudget(ctx context.Context, est llm.LLMClient, msgs []llm.Message, budget, currentSum int) ([]llm.Message, int, bool) {
	var selected []llm.Message
	for start := 0; start < len(msgs); start += historyPageSize {
		end := start + historyPageSize
		if end > len(msgs) {
			end = len(msgs)
		}
		chunk := msgs[start:end]

		chunkTokens := est.EstimateTokens(ctx, chunk)
		if currentSum+chunkTokens <= budget {
			selected = append(selected, chunk...)
			currentSum += chunkTokens
			continue
		}

		for _, msg := range chunk {
			msgTokens := est.EstimateTokens(ctx, []llm.Message{msg})
			if currentSum+msgTokens > budget {
				return selected, currentSum, true
			}
			selected = append(selected, msg)
			currentSum += msgTokens
		}
	}
	return selected, currentSum, false
}

func reverseMessages(msgs []llm.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
