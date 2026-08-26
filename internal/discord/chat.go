package discord

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/conversation"
	"characterllm/internal/discord/commands"
	"characterllm/internal/images"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

// Chat runs the conversation turn cycle
type Chat struct {
	LLM           llm.LLMClient
	Session       *session.Manager
	Config        *config.Config
	Audit         *audit.AuditLogger
	ImageClient   images.ImageClient
	PromptBuilder *conversation.PromptBuilder
	Compactor     *conversation.Compactor
	Locks         *ConversationLocks
}

// Handle processes one incoming message into a complete conversation turn.
func (c *Chat) Handle(s commands.DiscordSession, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}

	// Initialize request tracking
	reqID := uuid.New().String()
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID, "guild_id", m.GuildID))

	prompt, ok := c.getPrompt(ctx, s, m)
	if !ok {
		return
	}

	s.ChannelTyping(m.ChannelID)

	details, err := c.getActiveCharacter(ctx, m.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("error getting active character", "error", err)
		s.ChannelMessageSendReply(m.ChannelID, responses.General.NoCharacterSet, &discordgo.MessageReference{
			MessageID: m.ID,
		})
		return
	}

	// Images are ephemeral: they ride along in this turn's prompt only and are never persisted to history.
	var imageDataURIs []string
	if c.Config.LLM.Vision {
		imageDataURIs = c.collectImageAttachments(ctx, m)
	}

	// Serialize the whole turn (save, assemble, generate, persist) so a queued
	// turn assembles its prompt after the previous turn's reply is stored.
	defer c.Locks.Lock(m.GuildID, "")()

	// Persist the incoming message before assembling the prompt
	userMsg := llm.Message{Role: "user", Content: prompt, Images: imageDataURIs}
	userTokens := c.LLM.EstimateTokens(ctx, []llm.Message{userMsg})
	if err := c.Session.SaveMessage(ctx, m.GuildID, "", "user", prompt); err != nil {
		logger.FromContext(ctx).Error("error saving user message", "error", err)
	}

	// compactionNeeded is true when the prompt exceeded the compaction target,
	// which triggers compaction after the reply.
	messages, compactionNeeded, err := c.PromptBuilder.Build(ctx, m.GuildID, "", details, prompt, imageDataURIs, userTokens)
	if err != nil {
		logger.FromContext(ctx).Error("error assembling prompt", "error", err)
		s.ChannelMessageSendReply(m.ChannelID, "Sorry, I had trouble remembering our conversation.", &discordgo.MessageReference{
			MessageID: m.ID,
		})
		return
	}

	// Generate and send response
	if err := c.processChat(ctx, s, m, details.CharacterID, prompt, messages, reqID); err != nil {
		logger.FromContext(ctx).Error("error processing chat", "error", err)
	}

	// Compact as soon as possible after the reply when the soft target was exceeded
	if compactionNeeded {
		c.Compactor.Compact(ctx, m.GuildID, "", details.CharacterID, reqID)
	}
}

var imageNoteRe = regexp.MustCompile(`(?s)<image_note>.*?</image_note>`)

// splitImageNotes separates the <image_note> blocks the model
// appends for image messages from the user-visible reply.
func splitImageNotes(response string) (visible, record string) {
	notes := imageNoteRe.FindAllStringSubmatch(response, -1)
	visible = strings.TrimSpace(imageNoteRe.ReplaceAllString(response, ""))

	var parts []string
	for _, n := range notes {
		if desc := strings.TrimSpace(n[0][len("<image_note>") : len(n[0])-len("</image_note>")]); desc != "" {
			parts = append(parts, desc)
		}
	}
	return visible, strings.Join(parts, "; ")
}

// collectImageAttachments fetches the message's image attachments (up to LLM.MaxImages) as processed data URIs.
func (c *Chat) collectImageAttachments(ctx context.Context, m *discordgo.MessageCreate) []string {
	var urls []string
	for _, a := range m.Attachments {
		if strings.HasPrefix(a.ContentType, "image/") && len(urls) < c.Config.LLM.MaxImages {
			urls = append(urls, a.URL)
		}
	}

	var dataURIs []string
	for _, u := range urls {
		duri, err := c.ImageClient.ImageToDataURI(ctx, u)
		if err != nil {
			logger.FromContext(ctx).Warn("skipping unreadable image attachment", "url", u, "error", err)
			continue
		}
		dataURIs = append(dataURIs, duri)
	}
	return dataURIs
}

// getPrompt checks if the bot is mentioned in a message or if the message is a reply to the bot, and formats the prompt with the user's display name.
func (c *Chat) getPrompt(_ context.Context, s commands.DiscordSession, m *discordgo.MessageCreate) (string, bool) {
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
func (c *Chat) getActiveCharacter(ctx context.Context, guildID string) (*session.CharacterDetails, error) {
	details, err := c.Session.GetCharacterDetails(ctx, guildID)
	if err != nil {
		return nil, err
	}
	if details == nil || details.CharacterID == "" {
		return nil, fmt.Errorf("no character set for guild")
	}
	return details, nil
}

// processChat handles the core cycle of generating an LLM response, logging it, and sending it to Discord.
func (c *Chat) processChat(ctx context.Context, s commands.DiscordSession, m *discordgo.MessageCreate, charID string, prompt string, messages []llm.Message, reqID string) error {
	start := time.Now()
	// Generate response
	fullResponse, reasoning, err := c.LLM.GenerateResponse(ctx, messages, c.Config.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("LLM response generation failed", "error", err)
		s.ChannelMessageSend(m.ChannelID, responses.General.LLMError)
		return err
	}
	latency := time.Since(start)

	// Log the raw response (including any image notes) for debugging
	c.Audit.LogConversation(ctx, m.GuildID, charID, prompt, reasoning, fullResponse, messages, latency, reqID)

	visible, imageNotes := splitImageNotes(fullResponse)
	if visible == "" {
		visible = fullResponse
	}

	// Send the final response as a reply to the user
	_, err = s.ChannelMessageSendReply(m.ChannelID, visible, &discordgo.MessageReference{
		MessageID: m.ID,
	})
	if err != nil {
		return fmt.Errorf("error sending response: %v", err)
	}

	c.Session.SaveMessage(ctx, m.GuildID, "", "assistant", visible)

	// Attach the image descriptions to the user's turn so future prompts and compaction can see what the (ephemeral) images depicted.
	if imageNotes != "" {
		if err := c.Session.AppendToLastUserMessage(ctx, m.GuildID, "", "\n[Image: "+imageNotes+"]"); err != nil {
			logger.FromContext(ctx).Error("failed to attach image note to history", "error", err)
		}
	}
	return nil
}
