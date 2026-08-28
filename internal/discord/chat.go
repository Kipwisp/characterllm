package discord

import (
	"context"
	"fmt"
	"math/rand"
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
	// Roll returns a uniform float in [0,1) for the ambient reply
	// probability gate; nil defaults to math/rand.
	Roll func() float64
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
	kind := audit.KindChat
	var imageDataURIs []string
	if ok {
		// Images are ephemeral: they ride along in this turn's prompt only and are never persisted to history.
		if c.Config.LLM.Vision {
			imageDataURIs = c.collectImageAttachments(ctx, m)
		}
	} else {
		prompt, imageDataURIs, ok = c.ambientReplyPrompt(ctx, s, m)
		if !ok {
			return
		}
		kind = audit.KindAmbientReply
	}

	c.runTurn(ctx, s, m.GuildID, m.ChannelID, prompt, imageDataURIs, &discordgo.MessageReference{MessageID: m.ID}, kind, reqID)
}

// ambientReplyPrompt applies the ambient reply chance: in the guild's
// ambient channel, any user message that does not address the bot gives the
// bot a probability-gated chance to join in.
func (c *Chat) ambientReplyPrompt(ctx context.Context, s commands.DiscordSession, m *discordgo.MessageCreate) (string, []string, bool) {
	if !c.Config.Ambient.Enabled {
		return "", nil, false
	}
	if strings.Contains(m.Content, s.GetUserMention()) {
		return "", nil, false
	}
	channelID, err := c.Session.GetAmbientChannel(ctx, m.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to read ambient channel", "error", err)
		return "", nil, false
	}
	if channelID != m.ChannelID {
		return "", nil, false
	}
	if c.roll() >= c.Config.Ambient.ReplyProbability {
		logger.FromContext(ctx).Debug("ambient reply skipped by probability gate", "guild_id", m.GuildID)
		return "", nil, false
	}
	cue, imageDataURIs, err := buildTranscriptCue(ctx, s, c.Config, c.ImageClient, m.ChannelID)
	if err != nil {
		logger.FromContext(ctx).Warn("ambient reply transcript fetch failed", "error", err)
		return "", nil, false
	}
	if cue == "" {
		logger.FromContext(ctx).Debug("ambient reply skipped: empty transcript", "guild_id", m.GuildID)
		return "", nil, false
	}

	logger.FromContext(ctx).Debug("sending ambient reply", "guild_id", m.GuildID)
	return cue, imageDataURIs, true
}

// roll returns the gate's probability roll, defaulting to math/rand when no
// source is injected.
func (c *Chat) roll() float64 {
	if c.Roll != nil {
		return c.Roll()
	}
	return rand.Float64()
}

// runTurn executes one full conversation turn: resolve the active character
// and thread, persist the user message, assemble the prompt, generate and
// send the reply, persist it, and compact when the budget demands it. A nil
// replyRef sends the reply as a plain channel message instead of a reply.
func (c *Chat) runTurn(ctx context.Context, s commands.DiscordSession, guildID, channelID, userContent string, images []string, replyRef *discordgo.MessageReference, kind audit.Kind, reqID string) {
	s.ChannelTyping(channelID)

	details, err := c.getActiveCharacter(ctx, guildID)
	if err != nil {
		logger.FromContext(ctx).Error("error getting active character", "error", err)
		if err := c.sendTurnMessage(ctx, s, channelID, replyRef, responses.General.NoCharacterSet); err != nil {
			logger.FromContext(ctx).Error("failed to send turn message", "error", err)
		}
		return
	}

	// Give the character its default thread so the active pointer always
	// resolves, then read the pointer (details was snapshotted before it).
	if err := c.Session.EnsureDefaultThread(ctx, guildID, details.CharacterID); err != nil {
		logger.FromContext(ctx).Warn("failed to ensure default thread", "error", err)
	}
	threadID := details.ActiveThreadID
	if resolved, err := c.Session.GetActiveThreadID(ctx, guildID, details.CharacterID); err == nil && resolved != "" {
		threadID = resolved
	}

	// Serialize the whole turn (save, assemble, generate, persist) so a queued
	// turn assembles its prompt after the previous turn's reply is stored.
	defer c.Locks.Lock(guildID, threadID)()

	// Persist the incoming message before assembling the prompt
	userMsg := llm.Message{Role: "user", Content: userContent, Images: images}
	userTokens := c.LLM.EstimateTokens(ctx, []llm.Message{userMsg})
	if err := c.Session.SaveMessage(ctx, guildID, threadID, "user", userContent); err != nil {
		logger.FromContext(ctx).Error("error saving user message", "error", err)
	}
	if err := c.Session.TouchThread(ctx, guildID, details.CharacterID, threadID); err != nil {
		logger.FromContext(ctx).Warn("failed to touch thread", "error", err)
	}

	// compactionNeeded is true when the prompt exceeded the compaction target,
	// which triggers compaction after the reply.
	messages, compactionNeeded, err := c.PromptBuilder.Build(ctx, guildID, threadID, details, userContent, images, userTokens)
	if err != nil {
		logger.FromContext(ctx).Error("error assembling prompt", "error", err)
		if err := c.sendTurnMessage(ctx, s, channelID, replyRef, "Sorry, I had trouble remembering our conversation."); err != nil {
			logger.FromContext(ctx).Error("failed to send turn message", "error", err)
		}
		return
	}

	// Generate and send response
	if err := c.processChat(ctx, s, guildID, channelID, replyRef, threadID, details.CharacterID, userContent, messages, reqID, kind); err != nil {
		logger.FromContext(ctx).Error("error processing chat", "error", err)
	}

	// Compact as soon as possible after the reply when the soft target was exceeded
	if compactionNeeded {
		c.Compactor.Compact(ctx, guildID, threadID, details.CharacterID, reqID)
	}
}

// sendTurnMessage sends content as a reply to replyRef, or as a plain
// channel message when replyRef is nil.
func (c *Chat) sendTurnMessage(ctx context.Context, s commands.DiscordSession, channelID string, replyRef *discordgo.MessageReference, content string) error {
	if replyRef != nil {
		_, err := s.ChannelMessageSendReply(channelID, content, replyRef)
		return err
	}
	_, err := s.ChannelMessageSend(channelID, content)
	return err
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
func (c *Chat) processChat(ctx context.Context, s commands.DiscordSession, guildID, channelID string, replyRef *discordgo.MessageReference, threadID, charID, prompt string, messages []llm.Message, reqID string, kind audit.Kind) error {
	start := time.Now()
	// Generate response
	fullResponse, reasoning, err := c.LLM.GenerateResponse(ctx, messages, c.Config.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("LLM response generation failed", "error", err)
		s.ChannelMessageSend(channelID, responses.General.LLMError)
		return err
	}
	latency := time.Since(start)

	// Log the raw response (including any image notes) for debugging
	system := ""
	if len(messages) > 0 && messages[0].Role == "system" {
		system = messages[0].Content
	}
	c.Audit.Log(ctx, guildID, threadID, charID, reqID, audit.Turn{
		Kind:      kind,
		Model:     c.Config.LLM.Model,
		Latency:   latency,
		System:    system,
		Prompt:    prompt,
		Reasoning: reasoning,
		Response:  fullResponse,
	})

	visible, imageNotes := splitImageNotes(fullResponse)
	if visible == "" {
		visible = fullResponse
	}

	// Send the final response as a reply to the user, or plainly when there is no originating message
	if sendErr := c.sendTurnMessage(ctx, s, channelID, replyRef, visible); sendErr != nil {
		return fmt.Errorf("error sending response: %v", sendErr)
	}

	c.Session.SaveMessage(ctx, guildID, threadID, "assistant", visible)

	// Attach the image descriptions to the user's turn so future prompts and compaction can see what the (ephemeral) images depicted.
	if imageNotes != "" {
		if err := c.Session.AppendToLastUserMessage(ctx, guildID, threadID, "\n[Image: "+imageNotes+"]"); err != nil {
			logger.FromContext(ctx).Error("failed to attach image note to history", "error", err)
		}
	}
	return nil
}
