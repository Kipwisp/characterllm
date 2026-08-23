package discord

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/discord/commands"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

// Handlers manages the interaction between Discord events, LLM generation, and session state.
type Handlers struct {
	LLM       *llm.Client
	Session   *session.Manager
	BotConfig *config.Config
	Audit     *audit.AuditLogger
}

// NewHandlers creates a new Handlers instance with the provided dependencies.
func NewHandlers(llm *llm.Client, session *session.Manager, cfg *config.Config, audit *audit.AuditLogger) *Handlers {
	return &Handlers{
		LLM:       llm,
		Session:   session,
		BotConfig: cfg,
		Audit:     audit,
	}
}

// GetSession returns the session manager.
func (h *Handlers) GetSession() *session.Manager { return h.Session }

// GetLLM returns the LLM client.
func (h *Handlers) GetLLM() *llm.Client { return h.LLM }

// GetConfig returns the bot configuration.
func (h *Handlers) GetConfig() *config.Config { return h.BotConfig }

// GetAudit returns the audit logger.
func (h *Handlers) GetAudit() *audit.AuditLogger { return h.Audit }

// MessageCreate handles incoming Discord messages. It triggers LLM responses when the bot is mentioned.
func (h *Handlers) MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}

	// Initialize request tracking
	reqID := uuid.New().String()
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID, "guild_id", m.GuildID))

	prompt, ok := h.getPrompt(ctx, s, m)
	if !ok {
		return
	}

	s.ChannelTyping(m.ChannelID)

	var messages []llm.Message
	details, err := h.getSystemPrompt(ctx, m.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("error constructing system prompt", "error", err)
		s.ChannelMessageSendReply(m.ChannelID, responses.MsgNoCharacterSet, &discordgo.MessageReference{
			MessageID: m.ID,
		})
		return
	}

	// Construct the message list starting with the system prompt
	template, err := os.ReadFile(h.BotConfig.Prompts.SystemPath)
	if err != nil {
		logger.FromContext(ctx).Error("error reading system prompt template", "error", err)
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: fmt.Sprintf("You are %s. %s", details.DisplayName, details.Description),
		})
	} else {
		placeholder := "[CHARACTER_DETAILS]"
		finalPrompt := strings.Replace(string(template), placeholder, details.Description, 1)
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: finalPrompt,
		})
	}

	// Append Current Message
	h.Session.SaveMessage(ctx, m.GuildID, "", "user", prompt)
	// Get existing history and append to messages
	history, err := h.Session.GetHistory(ctx, m.GuildID, "")
	if err != nil {
		logger.FromContext(ctx).Error("failed to retrieve chat history", "error", err)
		s.ChannelMessageSendReply(m.ChannelID, "Sorry, I had trouble remembering our conversation.", &discordgo.MessageReference{
			MessageID: m.ID,
		})
		return
	}
	messages = append(messages, history...)

	// Generate and send response
	if err := h.processChat(ctx, s, m, details.CharacterID, prompt, messages, reqID); err != nil {
		logger.FromContext(ctx).Error("error processing chat", "error", err)
	}

	// History Compaction: If history gets too long, summarize the oldest part
	h.compactHistory(ctx, m, details.CharacterID, reqID)
}

// getPrompt checks if the bot is mentioned in a message or if the message is a reply to the bot, and formats the prompt with the user's display name.
func (h *Handlers) getPrompt(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) (string, bool) {
	isMentioned := strings.Contains(m.Content, s.State.User.Mention())
	isReplyToBot := m.ReferencedMessage != nil && m.ReferencedMessage.Author.ID == s.State.User.ID

	if !isMentioned && !isReplyToBot {
		return "", false
	}

	prompt := strings.ReplaceAll(m.Content, s.State.User.Mention(), "")

	displayName := m.Author.Username
	if m.Member != nil && m.Member.Nick != "" {
		displayName = m.Member.Nick
	}
	return fmt.Sprintf("%s: %s", displayName, prompt), true
}

// getSystemPrompt retrieves the current character for a guild, returning an error if none are set.
func (h *Handlers) getSystemPrompt(ctx context.Context, guildID string) (*session.CharacterDetails, error) {
	details, err := h.Session.GetCharacterDetails(ctx, guildID)
	if err != nil {
		return nil, err
	}
	if details == nil || details.CharacterID == "" {
		return nil, fmt.Errorf("no character set for guild")
	}
	return details, nil
}

// processChat handles the core cycle of generating an LLM response, logging it, and sending it to Discord.
func (h *Handlers) processChat(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, charID string, prompt string, messages []llm.Message, reqID string) error {
	start := time.Now()
	// Generate response (non-streaming)
	fullResponse, reasoning, err := h.LLM.GenerateResponse(ctx, messages, h.BotConfig.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("LLM response generation failed", "error", err)
		s.ChannelMessageSend(m.ChannelID, responses.MsgLLMError)
		return err
	}
	latency := time.Since(start)

	// Log the conversation for debugging
	h.Audit.LogConversation(ctx, m.GuildID, charID, prompt, reasoning, fullResponse, messages, latency, reqID)

	// Send the final response as a reply to the user
	_, err = s.ChannelMessageSendReply(m.ChannelID, fullResponse, &discordgo.MessageReference{
		MessageID: m.ID,
	})
	if err != nil {
		return fmt.Errorf("error sending response: %v", err)
	}

	h.Session.SaveMessage(ctx, m.GuildID, "", "assistant", fullResponse)
	return nil
}

// compactHistory checks if the conversation history exceeds the limit and, if so, prunes old messages and inserts a summary.
func (h *Handlers) compactHistory(ctx context.Context, m *discordgo.MessageCreate, charID string, reqID string) {
	count, err := h.Session.GetHistoryCount(ctx, m.GuildID, "")
	if err != nil {
		logger.FromContext(ctx).Error("error getting history count", "error", err)
		return
	}
	if count < 30 {
		return
	}

	logger.FromContext(ctx).Info("history threshold reached, compacting", "guild_id", m.GuildID)
	oldMsgs, err := h.Session.GetOldestMessages(ctx, m.GuildID, "", 10)
	if err != nil {
		logger.FromContext(ctx).Error("error fetching oldest messages for compaction", "error", err)
		return
	}

	compactionTemplate, err := os.ReadFile(h.BotConfig.Prompts.CompactionPath)
	if err != nil {
		logger.FromContext(ctx).Error("error reading compaction prompt file", "error", err)
		return
	}

	summaryPrompt := append([]llm.Message{{
		Role:    "system",
		Content: string(compactionTemplate),
	}}, oldMsgs...)

	start := time.Now()
	summary, reasoning, err := h.LLM.GenerateResponse(ctx, summaryPrompt, h.BotConfig.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("error during history compaction generation", "error", err)
		return
	}
	latency := time.Since(start)

	if err := h.Session.PruneAndSummarize(ctx, m.GuildID, "", summary, 10); err != nil {
		logger.FromContext(ctx).Error("error pruning history", "error", err)
		return
	}
	logger.FromContext(ctx).Info("history compacted successfully", "guild_id", m.GuildID)

	// Also log the compaction reasoning
	h.Audit.LogConversation(ctx, m.GuildID, charID, "SYSTEM_COMPACTION", reasoning, summary, oldMsgs, latency, reqID)

}

// InteractionCreate handles Discord slash command interactions.
func (h *Handlers) InteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	// Initialize request tracking
	reqID := uuid.New().String()
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID, "guild_id", i.GuildID))

	data := i.ApplicationCommandData()
	cmd := commands.Get(data.Name)
	if cmd == nil {
		logger.FromContext(ctx).Warn("unknown command", "command", data.Name)
		return
	}

	if err := cmd.Execute(ctx, h, s, i); err != nil {
		logger.FromContext(ctx).Error("error executing command", "command", data.Name, "error", err)
	}
}

// ComponentCreate handles Discord message component interactions (e.g., select menus).
func (h *Handlers) ComponentCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	// Initialize request tracking
	reqID := uuid.New().String()
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID, "guild_id", i.GuildID))

	if i.MessageComponentData().CustomID == "select_char_image" {
		commands.HandleSetCharacterImage(ctx, h, s, i)
	}
}
