package discord

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/discord/commands"
	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

const (
	// ambientTopicCue is the synthetic user message for a fresh-topic turn.
	ambientTopicCue = "The channel has gone quiet. Start a conversation — either a completely new topic or bringing back an old one."
	// ambientTranscriptHeader opens the synthetic user message for a reply turn.
	ambientTranscriptHeader = "Messages in this channel just now:"
	// ambientTranscriptFooter closes the transcript with the reply instruction.
	ambientTranscriptFooter = "Reply to this conversation in character."
)

// Ambient is the scheduler that makes the guild's active character speak
// unprompted in its ambient channel.
type Ambient struct {
	Session     *session.Manager
	Chat        *Chat
	Config      *config.Config
	ImageClient images.ImageClient
	Discord     commands.DiscordSession
	// Roll returns a uniform float in [0,1) for the per-guild intervals, the
	// tick probability gate, and the mode coin flip.
	Roll func() float64
}

// NewAmbient builds the ambient scheduler.
func NewAmbient(session *session.Manager, chat *Chat, cfg *config.Config, imageClient images.ImageClient, s commands.DiscordSession) *Ambient {
	return &Ambient{
		Session:     session,
		Chat:        chat,
		Config:      cfg,
		ImageClient: imageClient,
		Discord:     s,
		Roll:        rand.Float64,
	}
}

// noGuildsBackoff is how often Run re-checks for configured guilds when
// there is none to schedule.
const noGuildsBackoff = time.Second

// Run loops until ctx is cancelled, keeping one next-due timestamp per
// configured guild. Each guild is paced by its own random
// AMBIENT_MIN_SECONDS-AMBIENT_MAX_SECONDS interval; the loop sleeps until
// the earliest deadline and runs that guild's tick alone, so ticks never
// overlap and a newly configured guild starts with a full random offset
// instead of in lockstep with the others.
func (a *Ambient) Run(ctx context.Context) {
	nextDue := make(map[string]time.Time)
	for {
		guilds := a.configuredGuilds(ctx)
		if len(guilds) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(noGuildsBackoff):
			}
			continue
		}

		guildID, due := a.nextDueGuild(ctx, nextDue, guilds)
		if delay := time.Until(due); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		// The snapshot above can be stale: the channel may have been cleared
		// or re-pointed while we slept, so re-read it before speaking.
		channelID, ok := a.configuredGuilds(ctx)[guildID]
		if !ok {
			delete(nextDue, guildID)
			continue
		}

		// The tick probability gate: the wake fired, but the coin flip must
		// pass before the bot actually speaks.
		if a.Roll() < a.Config.Ambient.TickProbability {
			a.tick(ctx, guildID, channelID)
		} else {
			logger.FromContext(ctx).Debug("ambient tick skipped by probability gate", "guild_id", guildID)
		}
		nextDue[guildID] = time.Now().Add(a.randomInterval())
	}
}

// configuredGuilds returns the ambient channel per guild, or nil when the
// list cannot be read (the caller backs off and retries).
func (a *Ambient) configuredGuilds(ctx context.Context) map[string]string {
	channels, err := a.Session.ListAmbientChannels(ctx)
	if err != nil {
		logger.FromContext(ctx).Error("failed to list ambient channels", "error", err)
		return nil
	}
	return channels
}

// nextDueGuild returns the guild whose ambient turn is due earliest, dropping
// unconfigured guilds and assigning a random first-due time (in sorted guild
// order) to any guild seen for the first time.
func (a *Ambient) nextDueGuild(ctx context.Context, nextDue map[string]time.Time, guilds map[string]string) (string, time.Time) {
	for id := range nextDue {
		if _, ok := guilds[id]; !ok {
			delete(nextDue, id)
		}
	}
	ids := make([]string, 0, len(guilds))
	for id := range guilds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, ok := nextDue[id]; !ok {
			nextDue[id] = time.Now().Add(a.randomInterval())
		}
	}
	guildID, due := ids[0], nextDue[ids[0]]
	for _, id := range ids[1:] {
		if nextDue[id].Before(due) {
			guildID, due = id, nextDue[id]
		}
	}
	return guildID, due
}

func (a *Ambient) randomInterval() time.Duration {
	ambient := a.Config.Ambient
	min := float64(ambient.MinSeconds)
	max := float64(ambient.MaxSeconds)
	seconds := min + a.Roll()*(max-min)
	return time.Duration(seconds * float64(time.Second))
}

// pickAmbientGuild returns a random guild with an ambient channel set, or
// ok=false when none are configured.
func (a *Ambient) pickAmbientGuild(ctx context.Context) (string, string, bool) {
	channels, err := a.Session.ListAmbientChannels(ctx)
	if err != nil {
		logger.FromContext(ctx).Error("failed to list ambient channels", "error", err)
		return "", "", false
	}
	if len(channels) == 0 {
		return "", "", false
	}
	guilds := make([]string, 0, len(channels))
	for guildID := range channels {
		guilds = append(guilds, guildID)
	}
	sort.Strings(guilds)
	guildID := guilds[int(a.Roll()*float64(len(guilds)))]
	return guildID, channels[guildID], true
}

// tick runs one ambient turn for the guild's active character.
func (a *Ambient) tick(ctx context.Context, guildID, channelID string) {
	reqID := uuid.New().String()
	ctx = logger.ToContext(ctx, logger.WithRequestID(reqID, "guild_id", guildID))

	details, err := a.Session.GetCharacterDetails(ctx, guildID)
	if err != nil {
		logger.FromContext(ctx).Warn("ambient tick failed to get active character", "error", err, "guild_id", guildID)
		return
	}
	if details == nil || details.CharacterID == "" {
		logger.FromContext(ctx).Debug("ambient tick skipped: no active character", "guild_id", guildID)
		return
	}

	userContent, imageDataURIs, ok := a.buildCue(ctx, channelID)
	if !ok {
		return
	}

	logger.FromContext(ctx).Debug("ambient tick passed the check: sending message", "guild_id", guildID)
	a.Chat.runTurn(ctx, a.Discord, guildID, channelID, userContent, imageDataURIs, nil, audit.KindAmbient, reqID)
}

// buildTranscriptCue fetches the channel's recent messages and builds the
// reply-mode cue: the transcript of messages the bot has not already
// answered, framed with the header/footer cues, plus the transcript's image
// data URIs. It returns an error when the fetch fails and an empty cue when
// nothing remains after filtering.
func buildTranscriptCue(ctx context.Context, s commands.DiscordSession, cfg *config.Config, imageClient images.ImageClient, channelID string) (string, []string, error) {
	msgs, err := s.ChannelMessages(channelID, cfg.Ambient.ReplyCount, "", "", "")
	if err != nil {
		return "", nil, err
	}
	lines, imageURLs := extractTranscript(msgs, s.GetUserID(), cfg)
	if len(lines) == 0 {
		return "", nil, nil
	}
	content := ambientTranscriptHeader + "\n" + strings.Join(lines, "\n") + "\n" + ambientTranscriptFooter
	return content, collectImageURIs(ctx, imageClient, imageURLs), nil
}

// buildCue builds the synthetic user message (and any transcript images) for
// an ambient tick. The mode is a coin flip; an empty transcript falls back to
// the topic cue.
func (a *Ambient) buildCue(ctx context.Context, channelID string) (string, []string, bool) {
	if a.Roll() < 0.5 {
		return ambientTopicCue, nil, true
	}

	cue, imageDataURIs, err := buildTranscriptCue(ctx, a.Discord, a.Config, a.ImageClient, channelID)
	if err != nil {
		logger.FromContext(ctx).Warn("ambient transcript fetch failed", "error", err)
		return "", nil, false
	}
	if cue == "" {
		return ambientTopicCue, nil, true
	}
	return cue, imageDataURIs, true
}

// extractTranscript formats the channel chatter as "Name: message" lines and
// collects the messages' image attachment URLs (up to LLM.MaxImages, only
// when vision is enabled). Messages the bot would already have answered are
// left out: the bot's own messages, mentions of the bot, and replies to a
// bot message that falls inside the fetched window.
func extractTranscript(msgs []*discordgo.Message, botID string, cfg *config.Config) (lines, imageURLs []string) {
	// Discord returns the fetched messages newest-first; reverse so the
	// transcript reads chronologically.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	byID := make(map[string]*discordgo.Message, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	for _, m := range msgs {
		if m.Author == nil || m.Author.ID == botID || messageAddressesBot(m, botID, byID) {
			continue
		}
		if m.Content != "" {
			lines = append(lines, m.Author.Username+": "+m.Content)
		}
		if cfg.LLM.Vision {
			for _, att := range m.Attachments {
				if strings.HasPrefix(att.ContentType, "image/") && len(imageURLs) < cfg.LLM.MaxImages {
					imageURLs = append(imageURLs, att.URL)
				}
			}
		}
	}
	return lines, imageURLs
}

// messageAddressesBot reports whether m mentions the bot or replies to a bot
// message within the fetched window — the two forms of address that trigger
// a normal bot reply (getPrompt).
func messageAddressesBot(m *discordgo.Message, botID string, byID map[string]*discordgo.Message) bool {
	for _, u := range m.Mentions {
		if u.ID == botID {
			return true
		}
	}
	if ref := m.MessageReference; ref != nil {
		if target, ok := byID[ref.MessageID]; ok && target.Author != nil && target.Author.ID == botID {
			return true
		}
	}
	return false
}

func collectImageURIs(ctx context.Context, imageClient images.ImageClient, urls []string) []string {
	var dataURIs []string
	for _, u := range urls {
		duri, err := imageClient.ImageToDataURI(ctx, u)
		if err != nil {
			logger.FromContext(ctx).Warn("skipping unreadable transcript image", "url", u, "error", err)
			continue
		}
		dataURIs = append(dataURIs, duri)
	}
	return dataURIs
}
