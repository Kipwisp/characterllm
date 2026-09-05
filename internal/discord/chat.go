package discord

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"slices"
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
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID))

	prompt, ok := c.getPrompt(ctx, s, m)
	kind := audit.KindChat
	var userMsg llm.Message
	var record string
	if ok {
		userMsg = llm.TextMessage(llm.RoleUser, prompt)
		// Images are ephemeral: they ride along in this turn's prompt only. The
		// persisted record carries one placeholder per image so each harvested
		// note lands on its own [Image: ...] line after the reply.
		if c.Config.LLM.Vision {
			if uris := c.collectImageURIs(ctx, m); len(uris) > 0 {
				userMsg = llm.Message{Role: llm.RoleUser, Parts: llm.TextWithImages(prompt, uris)}
				record = prompt
				for i := range uris {
					record += "\n" + session.ImageMarker(i+1)
				}
			}
		}
	} else {
		userMsg, record, ok = c.ambientReplyPrompt(ctx, s, m)
		if !ok {
			return
		}
		kind = audit.KindAmbientReply
		c.runTurn(ctx, s, m.GuildID, turn{
			ChannelID:   m.ChannelID,
			UserMessage: userMsg,
			Record:      record,
			ReplyRef:    &discordgo.MessageReference{MessageID: m.ID},
			Route:       identityRoute,
		}, kind, reqID)
		return
	}

	c.runTurn(ctx, s, m.GuildID, turn{
		ChannelID:   m.ChannelID,
		UserMessage: userMsg,
		Record:      record,
		ReplyRef:    &discordgo.MessageReference{MessageID: m.ID},
		Route:       identityRoute,
	}, kind, reqID)
}

// ambientReplyPrompt applies the ambient reply chance: in one of the
// guild's ambient channels, any user message that does not address the bot
// gives the bot a probability-gated chance to join in.
func (c *Chat) ambientReplyPrompt(ctx context.Context, s commands.DiscordSession, m *discordgo.MessageCreate) (llm.Message, string, bool) {
	if !c.Config.Ambient.Enabled {
		return llm.Message{}, "", false
	}

	if strings.Contains(m.Content, s.GetUserMention()) {
		return llm.Message{}, "", false
	}
	channelIDs, err := c.Session.GetAmbientChannels(ctx, m.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to read ambient channels", "error", err)
		return llm.Message{}, "", false
	}
	if !slices.Contains(channelIDs, m.ChannelID) {
		return llm.Message{}, "", false
	}
	if c.roll() >= c.Config.Ambient.ReplyProbability {
		logger.FromContext(ctx).Debug("ambient reply skipped by probability gate")
		return llm.Message{}, "", false
	}
	msg, record, err := buildTranscriptCue(ctx, s, c.Config, c.ImageClient, m.GuildID, m.ChannelID)
	if err != nil {
		logger.FromContext(ctx).Warn("ambient reply transcript fetch failed", "error", err)
		return llm.Message{}, "", false
	}
	if len(msg.Parts) == 0 {
		logger.FromContext(ctx).Debug("ambient reply skipped: empty transcript")
		return llm.Message{}, "", false
	}

	logger.FromContext(ctx).Debug("sending ambient reply")
	return msg, record, true
}

// roll returns the gate's probability roll, defaulting to math/rand when no
// source is injected.
func (c *Chat) roll() float64 {
	if c.Roll != nil {
		return c.Roll()
	}
	return rand.Float64()
}

// TurnRoute finalizes a turn's reply after generation: it sees the
// image-note-stripped reply and returns the (possibly trimmed) content and
// the channel to send to.
type TurnRoute func(content, channelID string) (string, string)

// identityRoute is the TurnRoute for ordinary turns: the reply goes out as
// generated, to the channel the turn was prepared for.
var identityRoute TurnRoute = func(content, channelID string) (string, string) {
	return content, channelID
}

// turn is one fully prepared conversation turn: the user message to send to
// the LLM, where to post the reply, and how to finalize the reply before it
// is stored and sent.
type turn struct {
	ChannelID   string
	UserMessage llm.Message                 // Role is always "user"
	Record      string                      // db persisted form of the user turn; empty → User.Text()
	ReplyRef    *discordgo.MessageReference // nil → plain message, not a reply
	Route       TurnRoute
}

// runTurn executes one full conversation turn: resolve the active character
// and thread, persist the user message, assemble the prompt, generate and
// send the reply, persist it, and compact when the budget demands it. A nil
// turn.ReplyRef sends the reply as a plain channel message instead of a
// reply; turn.Route finalizes the reply (content and send channel) after
// generation.
func (c *Chat) runTurn(ctx context.Context, s commands.DiscordSession, guildID string, t turn, kind audit.Kind, reqID string) {
	s.ChannelTyping(t.ChannelID)

	details, err := c.getActiveCharacter(ctx, guildID)
	if err != nil {
		logger.FromContext(ctx).Error("error getting active character", "error", err)
		if err := c.sendTurnMessage(s, t.ChannelID, t.ReplyRef, responses.General.NoCharacterSet); err != nil {
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
	userTokens := c.LLM.EstimateTokens(ctx, []llm.Message{t.UserMessage})
	record := t.UserMessage.Text()
	if t.Record != "" {
		record = t.Record
	}
	if err := c.Session.SaveMessage(ctx, guildID, threadID, llm.RoleUser, record); err != nil {
		logger.FromContext(ctx).Error("error saving user message", "error", err)
	}
	if err := c.Session.TouchThread(ctx, guildID, details.CharacterID, threadID); err != nil {
		logger.FromContext(ctx).Warn("failed to touch thread", "error", err)
	}

	// compactionNeeded is true when the prompt exceeded the compaction target,
	// which triggers compaction after the reply.
	messages, compactionNeeded, err := c.PromptBuilder.Build(ctx, guildID, threadID, details, t.UserMessage, userTokens)
	if err != nil {
		logger.FromContext(ctx).Error("error assembling prompt", "error", err)
		if err := c.sendTurnMessage(s, t.ChannelID, t.ReplyRef, "Sorry, I had trouble remembering our conversation."); err != nil {
			logger.FromContext(ctx).Error("failed to send turn message", "error", err)
		}
		return
	}

	// Generate and send response
	if err := c.processChat(ctx, s, guildID, t, threadID, details.CharacterID, messages, reqID, kind); err != nil {
		logger.FromContext(ctx).Error("error processing chat", "error", err)
	}

	// Compact as soon as possible after the reply when the soft target was exceeded
	if compactionNeeded {
		c.Compactor.Compact(ctx, guildID, threadID, details.CharacterID, reqID)
	}
}

// sendTurnMessage sends content as a reply to replyRef, or as a plain
// channel message when replyRef is nil.
func (c *Chat) sendTurnMessage(s commands.DiscordSession, channelID string, replyRef *discordgo.MessageReference, content string) error {
	if replyRef != nil {
		_, err := s.ChannelMessageSendReply(channelID, content, replyRef)
		return err
	}
	_, err := s.ChannelMessageSend(channelID, content)
	return err
}

var imageNoteRe = regexp.MustCompile(`(?s)<image_note>.*?</image_note>`)

// splitImageNotes separates the <image_note> blocks the model prepends for
// image messages from the user-visible reply. The notes come back in the
// order the model saw the images.
func splitImageNotes(response string) (visible string, notes []string) {
	matches := imageNoteRe.FindAllStringSubmatch(response, -1)
	visible = strings.TrimSpace(imageNoteRe.ReplaceAllString(response, ""))

	for _, m := range matches {
		if desc := strings.TrimSpace(m[0][len("<image_note>") : len(m[0])-len("</image_note>")]); desc != "" {
			notes = append(notes, desc)
		}
	}
	return visible, notes
}

// collectImageURIs fetches the message's image attachments (up to LLM.MaxImages) as processed data URIs.
func (c *Chat) collectImageURIs(ctx context.Context, m *discordgo.MessageCreate) []string {
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
// fileAttachmentMarkers renders non-image file attachments as space-joined
// "[File: name]" markers, so a prompt can reference a shared file by name
// even though its content is not available.
func fileAttachmentMarkers(atts []*discordgo.MessageAttachment) string {
	var markers []string
	for _, att := range atts {
		if !strings.HasPrefix(att.ContentType, "image/") && att.Filename != "" {
			markers = append(markers, "[File: "+att.Filename+"]")
		}
	}
	return strings.Join(markers, " ")
}

func (c *Chat) getPrompt(_ context.Context, s commands.DiscordSession, m *discordgo.MessageCreate) (string, bool) {
	isMentioned := strings.Contains(m.Content, s.GetUserMention())
	isReplyToBot := m.ReferencedMessage != nil && m.ReferencedMessage.Author.ID == s.GetUserID()

	if !isMentioned && !isReplyToBot {
		return "", false
	}

	prompt := strings.ReplaceAll(m.Content, s.GetUserMention(), "")
	prompt = strings.TrimSpace(prompt)
	if markers := fileAttachmentMarkers(m.Attachments); markers != "" {
		if prompt != "" {
			prompt += " "
		}
		prompt += markers
	}

	nick := ""
	if m.Member != nil {
		nick = m.Member.Nick
	}
	return fmt.Sprintf("%s: %s", displayName(nick, m.Author.GlobalName, m.Author.Username), prompt), true
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
func (c *Chat) processChat(ctx context.Context, s commands.DiscordSession, guildID string, t turn, threadID, charID string, messages []llm.Message, reqID string, kind audit.Kind) error {
	start := time.Now()
	// Generate response
	fullResponse, reasoning, err := c.LLM.GenerateResponse(ctx, messages, c.Config.LLM.Model)
	if err != nil {
		logger.FromContext(ctx).Error("LLM response generation failed", "error", err)
		s.ChannelMessageSend(t.ChannelID, responses.General.LLMError)
		return err
	}
	latency := time.Since(start)

	visible, notes := splitImageNotes(fullResponse)
	if visible == "" {
		visible = fullResponse
	}
	visible, t.ChannelID = t.Route(visible, t.ChannelID)

	// Send the final response as a reply to the user, or plainly when there is no originating message
	if sendErr := c.sendTurnMessage(s, t.ChannelID, t.ReplyRef, visible); sendErr != nil {
		return fmt.Errorf("error sending response: %v", sendErr)
	}

	c.Session.SaveMessage(ctx, guildID, threadID, llm.RoleAssistant, visible)

	// Attach the image descriptions to the user's turn so future prompts and
	// compaction can see what the (ephemeral) images depicted. The persisted
	// record carries one placeholder per image; resolution always runs when
	// markers are present so placeholders without a note are removed.
	record := t.UserMessage.Text()
	if t.Record != "" {
		record = t.Record
	}
	if strings.Contains(record, session.ImageMarkerPrefix) {
		if resolved, err := c.Session.ResolveImageNotes(ctx, guildID, threadID, notes); err != nil {
			logger.FromContext(ctx).Error("failed to attach image note to history", "error", err)
		} else {
			record = resolved
		}
	}

	// Log the exchange with the user input in the form it is persisted, so
	// the audit trail matches what future prompts will see.
	system := ""
	if len(messages) > 0 && messages[0].Role == llm.RoleSystem {
		system = messages[0].Text()
	}
	if n := len(t.UserMessage.ImageURIs()); n > 0 {
		record += fmt.Sprintf("\n[%d image(s) attached]", n)
	}
	c.Audit.Log(ctx, guildID, threadID, charID, reqID, audit.Turn{
		Kind:      kind,
		Model:     c.Config.LLM.Model,
		Latency:   latency,
		System:    system,
		Prompt:    record,
		Reasoning: reasoning,
		Response:  fullResponse,
	})
	return nil
}
