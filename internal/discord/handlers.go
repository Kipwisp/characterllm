package discord

import (
	"context"
	"fmt"
	"strings"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/conversation"
	"characterllm/internal/discord/commands"
	"characterllm/internal/images"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/prompts"
	"characterllm/internal/research"
	"characterllm/internal/responses"
	"characterllm/internal/search"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

type Handlers struct {
	LLM                 llm.LLMClient
	Session             *session.Manager
	BotConfig           *config.Config
	Audit               *audit.AuditLogger
	Prompts             *prompts.Set
	searchProvider      search.SearchProvider
	imageSearchProvider search.ImageSearchProvider
	synthesizer         research.Synthesizer
	imageClient         images.ImageClient
	builder             *conversation.PromptBuilder
	compactor           *conversation.Compactor
}

// NewHandlers creates a new Handlers instance with the provided dependencies.
func NewHandlers(llm llm.LLMClient, session *session.Manager, cfg *config.Config, audit *audit.AuditLogger, ps *prompts.Set) (*Handlers, error) {
	sp, isp, err := search.NewProvider(cfg.Images.Provider, cfg.Images.SearXNGURL)
	if err != nil {
		return nil, err
	}

	synth := research.NewSynthesizer(sp, llm, cfg, ps)
	imgClient := images.NewImageClient(isp, cfg.Images.CacheDir)
	if imgClient == nil {
		return nil, fmt.Errorf("failed to initialize image client")
	}

	return &Handlers{
		LLM:                 llm,
		Session:             session,
		BotConfig:           cfg,
		Audit:               audit,
		Prompts:             ps,
		searchProvider:      sp,
		imageSearchProvider: isp,
		synthesizer:         synth,
		imageClient:         imgClient,
		builder:             conversation.NewPromptBuilder(llm, session, cfg, ps),
		compactor:           conversation.NewCompactor(llm, session, cfg, audit, ps),
	}, nil
}

// GetSession returns the session manager.
func (h *Handlers) GetSession() *session.Manager { return h.Session }

// GetLLM returns the LLM client.
func (h *Handlers) GetLLM() llm.LLMClient { return h.LLM }

// GetConfig returns the bot configuration.
func (h *Handlers) GetConfig() *config.Config { return h.BotConfig }

// GetAudit returns the audit logger.
func (h *Handlers) GetAudit() *audit.AuditLogger { return h.Audit }

// GetSearchProvider returns the configured web search provider.
func (h *Handlers) GetSearchProvider() search.SearchProvider {
	return h.searchProvider
}

// GetImageSearchProvider returns the configured image search provider.
func (h *Handlers) GetImageSearchProvider() search.ImageSearchProvider {
	return h.imageSearchProvider
}

// GetSynthesizer returns the configured character synthesizer.
func (h *Handlers) GetSynthesizer() research.Synthesizer {
	return h.synthesizer
}

// GetImageClient returns the configured image client.
func (h *Handlers) GetImageClient() images.ImageClient {
	return h.imageClient
}

// MessageCreate handles incoming Discord messages. It triggers LLM responses when the bot is mentioned.
func (h *Handlers) MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	h.handleMessageCreate(NewSessionWrapper(s), m)
}

// handleMessageCreate contains the core logic for processing incoming messages.
func (h *Handlers) handleMessageCreate(s commands.DiscordSession, m *discordgo.MessageCreate) {
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

	details, err := h.getActiveCharacter(ctx, m.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("error getting active character", "error", err)
		s.ChannelMessageSendReply(m.ChannelID, responses.General.NoCharacterSet, &discordgo.MessageReference{
			MessageID: m.ID,
		})
		return
	}

	// Persist the incoming message before assembling the prompt
	userTokens := h.LLM.EstimateTokens(ctx, []llm.Message{{Role: "user", Content: prompt}})
	if err := h.Session.SaveMessage(ctx, m.GuildID, "", "user", prompt, userTokens); err != nil {
		logger.FromContext(ctx).Error("error saving user message", "error", err)
	}

	// compactionNeeded is true when the prompt exceeded the compaction target,
	// which triggers compaction after the reply.
	messages, compactionNeeded, err := h.builder.Build(ctx, m.GuildID, "", details, prompt, userTokens)
	if err != nil {
		logger.FromContext(ctx).Error("error assembling prompt", "error", err)
		s.ChannelMessageSendReply(m.ChannelID, "Sorry, I had trouble remembering our conversation.", &discordgo.MessageReference{
			MessageID: m.ID,
		})
		return
	}

	// Generate and send response
	if err := h.processChat(ctx, s, m, details.CharacterID, prompt, messages, reqID); err != nil {
		logger.FromContext(ctx).Error("error processing chat", "error", err)
	}

	// Compact as soon as possible after the reply when the soft target was exceeded
	if compactionNeeded {
		h.compactor.Compact(ctx, m.GuildID, "", details.CharacterID, reqID)
	}
}

// getPrompt checks if the bot is mentioned in a message or if the message is a reply to the bot, and formats the prompt with the user's display name.
func (h *Handlers) getPrompt(_ context.Context, s commands.DiscordSession, m *discordgo.MessageCreate) (string, bool) {
	isMentioned := strings.Contains(m.Content, s.GetUserMention())
	isReplyToBot := m.ReferencedMessage != nil && m.ReferencedMessage.Author.ID == s.GetUserID()

	if !isMentioned && !isReplyToBot {
		return "", false
	}

	prompt := strings.ReplaceAll(m.Content, s.GetUserMention(), "")
	prompt = strings.TrimSpace(prompt)

	displayName := m.Author.Username
	if m.Member != nil && m.Member.Nick != "" {
		displayName = m.Member.Nick
	}
	return fmt.Sprintf("%s: %s", displayName, prompt), true
}

// getActiveCharacter retrieves the active character for a guild, returning an error if none are set.
func (h *Handlers) getActiveCharacter(ctx context.Context, guildID string) (*session.CharacterDetails, error) {
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
func (h *Handlers) processChat(ctx context.Context, s commands.DiscordSession, m *discordgo.MessageCreate, charID string, prompt string, messages []llm.Message, reqID string) error {
	start := time.Now()
	// Generate response (non-streaming)
	fullResponse, reasoning, err := h.LLM.GenerateResponse(ctx, messages, h.BotConfig.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("LLM response generation failed", "error", err)
		s.ChannelMessageSend(m.ChannelID, responses.General.LLMError)
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

	assistantMsg := llm.Message{Role: "assistant", Content: fullResponse}
	assistantTokens := h.LLM.EstimateTokens(ctx, []llm.Message{assistantMsg})
	h.Session.SaveMessage(ctx, m.GuildID, "", "assistant", fullResponse, assistantTokens)
	return nil
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

	if err := cmd.Execute(ctx, h, NewSessionWrapper(s), i); err != nil {
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
		commands.HandleSetCharacterImage(ctx, h, NewSessionWrapper(s), i)
	} else if i.MessageComponentData().CustomID == "select_character_card" {
		commands.HandleSelectCharacterCard(ctx, h, NewSessionWrapper(s), i)
	} else if strings.HasPrefix(i.MessageComponentData().CustomID, "list_char_") {
		commands.HandleListCharactersPagination(ctx, h, NewSessionWrapper(s), i)
	}

}
