package discord

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
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
	// ambientChannelListLabel introduces the guild's ambient channels in a
	// multi-channel topic cue; the model answers with a CHANNEL: line.
	ambientChannelListLabel = "Ambient channels:"
	// ambientChannelPickInstruction tells a multi-channel topic cue how to
	// report the chosen channel.
	ambientChannelPickInstruction = "The channel names only indicate where the message will be posted, not what it should be about: choose the topic freely, then pick the number of the channel that best fits the message — if none of them fits well, still pick the closest one, the list is only where the message goes, not a filter on the topic. Start the reply with a `CHANNEL: n` line, then the message."
	// ambientChannelLinePrefix is the marker line the model puts before a
	// multi-channel topic reply to give the number of the channel it chose.
	ambientChannelLinePrefix = "CHANNEL: "
	// ambientTranscriptHeader opens the synthetic user message for a reply turn.
	ambientTranscriptHeader = "Messages in this channel just now:"
	// ambientTranscriptFooter closes the transcript with the reply instruction.
	ambientTranscriptFooter = "Reply to this conversation in character."
)

// Ambient is the scheduler that makes the guild's active character speak
// unprompted in one of its ambient channels.
type Ambient struct {
	Session     *session.Manager
	Chat        *Chat
	Config      *config.Config
	ImageClient images.ImageClient
	Discord     commands.DiscordSession
	// Roll returns a uniform float in [0,1) for the per-guild intervals, the
	// tick probability gate, the mode coin flip, and the channel picks.
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

		// The snapshot above can be stale: the set may have been cleared or
		// changed while we slept, so re-read it before speaking.
		channels, ok := a.configuredGuilds(ctx)[guildID]
		if !ok || len(channels) == 0 {
			delete(nextDue, guildID)
			continue
		}

		// The tick probability gate: the wake fired, but the coin flip must
		// pass before the bot actually speaks.
		if a.Roll() < a.Config.Ambient.TickProbability {
			a.tick(ctx, guildID, channels)
		} else {
			logger.FromContext(ctx).Debug("ambient tick skipped by probability gate", "guild_id", guildID)
		}
		nextDue[guildID] = time.Now().Add(a.randomInterval())
	}
}

// configuredGuilds returns the ambient channels per guild, or nil when the
// list cannot be read (the caller backs off and retries).
func (a *Ambient) configuredGuilds(ctx context.Context) map[string][]string {
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
func (a *Ambient) nextDueGuild(ctx context.Context, nextDue map[string]time.Time, guilds map[string][]string) (string, time.Time) {
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

// pickChannel picks a channel from the set uniformly at random (in sorted
// order, through the injected roll source).
func (a *Ambient) pickChannel(channels []string) string {
	sorted := make([]string, len(channels))
	copy(sorted, channels)
	sort.Strings(sorted)
	return sorted[int(a.Roll()*float64(len(sorted)))]
}

// tick runs one ambient turn for the guild's active character in one of the
// guild's ambient channels.
func (a *Ambient) tick(ctx context.Context, guildID string, channels []string) {
	reqID := uuid.New().String()
	ctx = logger.ToContext(ctx, logger.WithRequestID(reqID, "guild_id", guildID))

	details, err := a.Session.GetCharacterDetails(ctx, guildID)
	if err != nil {
		logger.FromContext(ctx).Warn("ambient tick failed to get active character", "error", err)
		return
	}
	if details == nil || details.CharacterID == "" {
		logger.FromContext(ctx).Debug("ambient tick skipped: no active character")
		return
	}

	t, ok := a.buildTurn(ctx, guildID, channels)
	if !ok {
		return
	}

	logger.FromContext(ctx).Debug("ambient tick passed the check: sending message", "channel_id", t.ChannelID)
	a.Chat.runTurn(ctx, a.Discord, guildID, t, audit.KindAmbient, reqID)
}

// buildTurn builds the synthetic user message and destination channel for an
// ambient tick. The mode is a coin flip: topic mode posts a fresh
// conversation (the model picks the channel when the guild has several),
// transcript mode reads the recent messages of a randomly picked channel and
// replies to them, falling back to the plain topic cue when nothing remains
// after filtering.
func (a *Ambient) buildTurn(ctx context.Context, guildID string, channels []string) (turn, bool) {
	if a.Roll() < 0.5 {
		return a.topicTurn(ctx, guildID, channels), true
	}

	channelID := a.pickChannel(channels)
	cue, imageDataURIs, err := buildTranscriptCue(ctx, a.Discord, a.Config, a.ImageClient, guildID, channelID)
	if err != nil {
		logger.FromContext(ctx).Warn("ambient transcript fetch failed", "error", err)
		return turn{}, false
	}
	if cue == "" {
		return turn{ChannelID: channelID, Content: ambientTopicCue, Route: identityRoute}, true
	}
	return turn{ChannelID: channelID, Content: cue, Images: imageDataURIs, Route: identityRoute}, true
}

// topicTurn builds the topic-mode turn. With one ambient channel the turn
// posts there with the plain cue; with several, the cue lists the channels
// and the model chooses the destination via a CHANNEL: line.
func (a *Ambient) topicTurn(ctx context.Context, guildID string, channels []string) turn {
	if len(channels) == 1 {
		return turn{ChannelID: channels[0], Content: ambientTopicCue, Route: identityRoute}
	}
	list := make([]string, len(channels))
	for i, name := range a.channelNames(ctx, guildID, channels) {
		list[i] = fmt.Sprintf("%d. %s", i+1, name)
	}
	content := ambientTopicCue + "\n" + ambientChannelListLabel + "\n" + strings.Join(list, "\n") +
		"\n" + ambientChannelPickInstruction
	return turn{
		ChannelID: a.pickChannel(channels),
		Content:   content,
		Route:     a.channelRoute(ctx, channels),
	}
}

// channelNames resolves the channel IDs to "#name" labels for the topic cue
// (falling back to the raw ID when the guild's channel list cannot be read
// or does not contain the ID).
func (a *Ambient) channelNames(ctx context.Context, guildID string, channelIDs []string) []string {
	nameByID := make(map[string]string, len(channelIDs))
	if channels, err := a.Discord.GuildChannels(guildID); err == nil {
		for _, ch := range channels {
			if ch.Type == discordgo.ChannelTypeGuildText {
				nameByID[ch.ID] = "#" + ch.Name
			}
		}
	} else {
		logger.FromContext(ctx).Warn("failed to list guild channels for the ambient topic cue", "error", err)
	}

	names := make([]string, len(channelIDs))
	for i, id := range channelIDs {
		name, ok := nameByID[id]
		if !ok {
			name = id
		}
		names[i] = name
	}
	return names
}

// channelRoute re-points a multi-channel topic turn's send from the
// `CHANNEL: n` line the model puts before its message (n is the number of a
// channel in the cue's list), falling back to the turn's default channel
// when the line is missing or is not a valid in-range number.
func (a *Ambient) channelRoute(ctx context.Context, channels []string) TurnRoute {
	return func(content, fallback string) (string, string) {
		number, rest, hasLine := splitChannelLine(content)
		if !hasLine {
			return content, fallback
		}
		number = strings.TrimSpace(number)
		if number == "" {
			return content, fallback
		}
		n, err := strconv.Atoi(number)
		if err != nil || n < 1 || n > len(channels) {
			logger.FromContext(ctx).Warn("ambient topic reply picked a channel number outside the list; using the fallback channel", "pick", number)
			return rest, fallback
		}
		return rest, channels[n-1]
	}
}

// splitChannelLine reports whether the reply's first line is a CHANNEL: line
// and, when it is, returns the channel number as written and the reply with
// the line removed.
func splitChannelLine(reply string) (number, rest string, ok bool) {
	lines := strings.SplitN(reply, "\n", 2)
	first := strings.TrimSpace(lines[0])
	if len(lines) == 2 {
		rest = lines[1]
	}
	number, found := strings.CutPrefix(first, ambientChannelLinePrefix)
	return number, rest, found
}

// buildTranscriptCue fetches the channel's recent messages and builds the
// reply-mode cue: the transcript of messages the bot has not already
// answered, framed with the header/footer cues, plus the transcript's image
// data URIs. It returns an error when the fetch fails and an empty cue when
// nothing remains after filtering.
func buildTranscriptCue(ctx context.Context, s commands.DiscordSession, cfg *config.Config, imageClient images.ImageClient, guildID, channelID string) (string, []string, error) {
	msgs, err := s.ChannelMessages(channelID, cfg.Ambient.ReplyCount, "", "", "")
	if err != nil {
		return "", nil, err
	}
	names := resolveTranscriptNames(s, guildID, msgs, s.GetUserID())
	lines, imageURLs := extractTranscript(msgs, s.GetUserID(), cfg, names)
	if len(lines) == 0 {
		return "", nil, nil
	}
	content := ambientTranscriptHeader + "\n" + strings.Join(lines, "\n") + "\n" + ambientTranscriptFooter
	return content, collectImageURIs(ctx, imageClient, imageURLs), nil
}

// extractTranscript formats the channel chatter as "Name: message" lines and
// collects the messages' image attachment URLs (up to LLM.MaxImages, only
// when vision is enabled). Non-image file attachments are referenced inline
// as "[File: name]" so the transcript still shows that something was shared
// even though its content is not available. Messages the bot would already
// have answered are left out: the bot's own messages, mentions of the bot,
// and replies to a bot message that falls inside the fetched window.
func extractTranscript(msgs []*discordgo.Message, botID string, cfg *config.Config, names map[string]string) (lines, imageURLs []string) {
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
		line := m.Content
		if cfg.LLM.Vision {
			for _, att := range m.Attachments {
				if strings.HasPrefix(att.ContentType, "image/") && len(imageURLs) < cfg.LLM.MaxImages {
					imageURLs = append(imageURLs, att.URL)
				}
			}
		}
		if markers := fileAttachmentMarkers(m.Attachments); markers != "" {
			if line != "" {
				line += " "
			}
			line += markers
		}
		if line != "" {
			lines = append(lines, names[m.Author.ID]+": "+line)
		}
	}
	return lines, imageURLs
}

// displayName resolves a user's display name the way Discord shows it:
// the guild nickname when set, otherwise the global display name when set,
// otherwise the username.
func displayName(nick, globalName, username string) string {
	switch {
	case nick != "":
		return nick
	case globalName != "":
		return globalName
	default:
		return username
	}
}

// resolveTranscriptNames maps each non-bot author's ID to the display name
// for the transcript. The REST message fetch carries no member info, so the
// guild nickname is looked up per author; a failed lookup leaves that author
// on their global display name or username.
func resolveTranscriptNames(s commands.DiscordSession, guildID string, msgs []*discordgo.Message, botID string) map[string]string {
	names := make(map[string]string)
	for _, m := range msgs {
		if m.Author == nil || m.Author.ID == botID {
			continue
		}
		if _, ok := names[m.Author.ID]; ok {
			continue
		}
		nick := ""
		if member, err := s.GuildMember(guildID, m.Author.ID); err == nil && member != nil {
			nick = member.Nick
		}
		names[m.Author.ID] = displayName(nick, m.Author.GlobalName, m.Author.Username)
	}
	return names
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
