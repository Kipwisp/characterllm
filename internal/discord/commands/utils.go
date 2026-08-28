package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/research"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

const MaxSelectMenuDescriptionLength = 100

// discordNickLimit is the maximum length of a guild nickname.
const discordNickLimit = 32

// discordMessageLimit is the maximum length of a message content payload.
const discordMessageLimit = 2000

// maxAvatarBytes mirrors Discord's 10 MB avatar upload limit.
const maxAvatarBytes = 10 << 20 // 10 MiB

// choiceNameLimit is Discord's maximum length for an autocomplete choice name.
const choiceNameLimit = 100

// currentThreadChoiceLabel is the display name of the current-thread choice;
// it is what users type to find it in autocomplete.
const currentThreadChoiceLabel = "Current (active thread)"

// currentChoiceLabel is the display name of the current-character choice;
// it is what users type to find it in autocomplete.
const currentChoiceLabel = "Current (active character)"

// currentCardName is the special name option value that targets the guild's
// active character.
const currentCardName = "current"

// currentThreadKey is the option value that targets the character's active
// thread.
const currentThreadKey = currentCardName

// ErrCardNotFound is returned by resolveCard when no saved character matches.
var ErrCardNotFound = errors.New("character not found")

// cardAmbiguityMaxLines bounds the candidates listed in an ambiguity reply.
const cardAmbiguityMaxLines = 10

// Message component custom IDs.
const (
	setCharacterImageID       = "select_char_image"
	deleteConfirmPrefix       = "delete_confirm_"
	deleteCancelPrefix        = "delete_cancel_"
	deleteThreadConfirmPrefix = "delete_thread_confirm_"
	deleteThreadCancelPrefix  = "delete_thread_cancel_"
)

// Embed text caps. Discord allows 4096 for the description and 1024 per field
// value, but the total of all embed text is capped at 6000.
const (
	embedTotalLimit     = 6000
	embedDescriptionMax = 4096
	embedFieldLimit     = 250

	// embedCountLimit is Discord's maximum number of embeds per message;
	// exceeding it makes the API reject the whole message.
	embedCountLimit = 10
)

// maxAutocompleteChoices is Discord's limit on autocomplete suggestions.
const maxAutocompleteChoices = 25

func deleteConfirmID(characterID string) string { return deleteConfirmPrefix + characterID }

func deleteCancelID(characterID string) string { return deleteCancelPrefix + characterID }

func deleteThreadConfirmID(threadID string) string { return deleteThreadConfirmPrefix + threadID }

func deleteThreadCancelID(threadID string) string { return deleteThreadCancelPrefix + threadID }

// CardAmbiguityError is returned by resolveCard when several saved characters
// match the input.
type CardAmbiguityError struct {
	Input      string
	Candidates []*session.CharacterCard
}

func (e *CardAmbiguityError) Error() string {
	return fmt.Sprintf("multiple characters match %q", e.Input)
}

// ErrNoActiveCharacter is returned by resolveActiveCard when the guild has
// no active character set.
var ErrNoActiveCharacter = errors.New("no active character")

// resolveNameOrCurrent resolves the `current` key to the guild's active
// character card, falling back to resolveCard for any other input.
func resolveNameOrCurrent(ctx context.Context, sm *session.Manager, guildID, name string) (*session.CharacterCard, error) {
	if strings.EqualFold(name, currentCardName) {
		return resolveActiveCard(ctx, sm, guildID)
	}
	return resolveCard(ctx, sm, guildID, name)
}

// resolveActiveCard returns the guild's currently active character card.
func resolveActiveCard(ctx context.Context, sm *session.Manager, guildID string) (*session.CharacterCard, error) {
	details, err := sm.GetCharacterDetails(ctx, guildID)
	if err != nil || details == nil || details.CharacterID == "" {
		return nil, ErrNoActiveCharacter
	}
	card, err := sm.GetCharacterCard(ctx, guildID, details.CharacterID)
	if err != nil || card == nil {
		return nil, fmt.Errorf("failed to retrieve active character %q: %w", details.CharacterID, err)
	}
	return card, nil
}

// resolveThreadOption maps a /setthread or /deletethread option value to
// one of the character's threads: when the "current" key is allowed it
// targets the active thread, and anything else is an exact thread ID.
// Returns nil when no thread matches.
func resolveThreadOption(ctx context.Context, sm *session.Manager, guildID, characterID, value string, allowCurrent bool) *session.Thread {
	switch {
	case allowCurrent && strings.EqualFold(value, currentThreadKey):
		threads, err := sm.ListThreads(ctx, guildID, characterID)
		if err != nil {
			return nil
		}
		for _, th := range threads {
			if th.Active {
				return th
			}
		}
		return nil
	default:
		th, err := sm.GetThread(ctx, guildID, characterID, value)
		if err != nil || th == nil {
			return nil
		}
		return th
	}
}

// resolveCard resolves a user-supplied name to a character card, trying in
// order: direct character ID, then case-insensitive display or official name.
func resolveCard(ctx context.Context, sm *session.Manager, guildID, name string) (*session.CharacterCard, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "none") {
		return nil, fmt.Errorf("%w: %q", ErrCardNotFound, name)
	}

	if card, err := sm.GetCharacterCard(ctx, guildID, name); err != nil {
		return nil, fmt.Errorf("failed to resolve character %q: %w", name, err)
	} else if card != nil {
		return card, nil
	}

	cards, err := sm.GetGuildCharacters(ctx, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve character %q: %w", name, err)
	}

	var matches []*session.CharacterCard
	for _, card := range cards {
		if strings.EqualFold(card.DisplayName, name) || strings.EqualFold(card.OfficialName, name) {
			matches = append(matches, card)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: %q", ErrCardNotFound, name)
	case 1:
		return matches[0], nil
	default:
		return nil, &CardAmbiguityError{Input: name, Candidates: matches}
	}
}

// respondResolveError answers a failed resolveCard with the matching user-facing
// message. It returns a non-nil error only for unexpected (non-resolution) failures.
func respondResolveError(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, err error) error {
	var amb *CardAmbiguityError
	switch {
	case errors.Is(err, ErrNoActiveCharacter):
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.Empty,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return nil
	case errors.Is(err, ErrCardNotFound):
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(responses.CharacterResolution.NotFound, i.ApplicationCommandData().GetOption("name").StringValue()),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return nil
	case errors.As(err, &amb):
		names := make([]string, 0, len(amb.Candidates))
		for _, card := range amb.Candidates {
			if len(names) >= cardAmbiguityMaxLines {
				break
			}
			names = append(names, fmt.Sprintf("- %s (%s)", card.DisplayName, card.CharacterID))
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(responses.CharacterResolution.Ambiguous, amb.Input, strings.Join(names, "\n")),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return nil
	default:
		logger.FromContext(ctx).Error("failed to resolve character", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.SetError,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return err
	}
}

// activeChoiceSuffix marks the guild's active character in autocomplete
// choices.
const activeChoiceSuffix = " (active)"

// currentChoice is the autocomplete entry that targets the guild's active
// character without naming it.
func currentChoice() *discordgo.ApplicationCommandOptionChoice {
	return &discordgo.ApplicationCommandOptionChoice{Name: currentChoiceLabel, Value: currentCardName}
}

// autocompleteCharacters builds Discord autocomplete choices for the guild's
// characters matching query. The active character's entry is marked with
// " (active)"; when includeCurrent is set, the "Current (active character)"
// choice is offered first.
func autocompleteCharacters(ctx context.Context, sm *session.Manager, guildID, query string, includeCurrent bool) []*discordgo.ApplicationCommandOptionChoice {
	none := []*discordgo.ApplicationCommandOptionChoice{{Name: "No matching characters", Value: "none"}}
	if includeCurrent {
		none = append([]*discordgo.ApplicationCommandOptionChoice{currentChoice()}, none...)
	}

	cards, err := sm.GetGuildCharacters(ctx, guildID)
	if err != nil || len(cards) == 0 {
		return none
	}

	var activeID string
	if details, err := sm.GetCharacterDetails(ctx, guildID); err == nil && details != nil {
		activeID = details.CharacterID
	}

	query = strings.ToLower(query)
	includeCurrent = includeCurrent && strings.Contains(strings.ToLower(currentChoiceLabel), query)

	var prefix, partial []*session.CharacterCard
	for _, card := range cards {
		if card.DisplayName == "" {
			continue
		}
		display := strings.ToLower(card.DisplayName)
		series := strings.ToLower(card.Series)
		id := strings.ToLower(card.CharacterID)
		switch {
		case strings.HasPrefix(display, query) || strings.HasPrefix(series, query) || strings.HasPrefix(id, query):
			prefix = append(prefix, card)
		case strings.Contains(display, query) || strings.Contains(series, query) || strings.Contains(id, query):
			partial = append(partial, card)
		}
	}
	matched := append(prefix, partial...)
	if len(matched) == 0 {
		return none
	}
	if includeCurrent && len(matched) >= maxAutocompleteChoices {
		matched = matched[:maxAutocompleteChoices-1]
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(matched)+1)
	if includeCurrent {
		choices = append(choices, currentChoice())
	}
	for _, card := range matched {
		name := cardChoiceName(card)
		if card.CharacterID == activeID {
			name = truncateToRuneLimit(name+activeChoiceSuffix, choiceNameLimit)
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  name,
			Value: card.CharacterID,
		})
	}
	return choices
}

func characterGreeting(description string) string {
	greeting, ok := research.ExtractSection(description, research.SectionGreeting)
	if !ok {
		return ""
	}
	return strings.TrimSpace(greeting)
}

// autocompleteThreads builds Discord autocomplete choices for the active
// character's threads matching query, most recently used first.
func autocompleteThreads(ctx context.Context, sm *session.Manager, guildID, query string, includeCurrent bool) []*discordgo.ApplicationCommandOptionChoice {
	none := []*discordgo.ApplicationCommandOptionChoice{{Name: "No active character", Value: "none"}}
	details, err := sm.GetCharacterDetails(ctx, guildID)
	if err != nil || details == nil || details.CharacterID == "" {
		return none
	}

	threads, err := sm.ListThreads(ctx, guildID, details.CharacterID)
	if err == nil && len(threads) == 0 {
		// The character has not been promoted to the threads table yet
		// (autocomplete can run before any command has), so promote it.
		if perr := sm.EnsureDefaultThread(ctx, guildID, details.CharacterID); perr == nil {
			threads, err = sm.ListThreads(ctx, guildID, details.CharacterID)
		}
	}
	if err != nil || len(threads) == 0 {
		return none
	}

	query = strings.ToLower(query)
	includeCurrent = includeCurrent && strings.Contains(strings.ToLower(currentThreadChoiceLabel), query)

	var prefix, partial []*session.Thread
	for _, th := range threads {
		switch {
		case strings.HasPrefix(strings.ToLower(th.Name), query):
			prefix = append(prefix, th)
		case strings.Contains(strings.ToLower(th.Name), query):
			partial = append(partial, th)
		}
	}
	matched := append(prefix, partial...)
	if len(matched) == 0 && !includeCurrent {
		return none
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(matched)+1)
	if includeCurrent {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: currentThreadChoiceLabel, Value: currentThreadKey})
	}
	for _, th := range matched {
		name := th.Name
		if th.Active {
			name += " (active)"
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateToRuneLimit(name, choiceNameLimit),
			Value: th.ThreadID,
		})
	}
	return choices
}

// cardChoiceName renders an autocomplete choice label: display name, series,
// and the character ID. The ID is guild-unique, so it is what tells apart
// cards of the same character; it is therefore never truncated, while the
// display name yields space first.
func cardChoiceName(card *session.CharacterCard) string {
	suffix := card.CharacterID
	if card.Series != "" {
		suffix = "[" + card.Series + "] " + suffix
	}
	name := card.DisplayName + " " + suffix
	if len([]rune(name)) <= choiceNameLimit {
		return name
	}
	displayBudget := choiceNameLimit - len([]rune(suffix)) - 1
	if displayBudget < 0 {
		displayBudget = 0
	}
	name = truncateToRuneLimit(card.DisplayName, displayBudget) + " " + suffix
	return truncateToRuneLimit(name, choiceNameLimit)
}

// truncateToRuneLimit shortens s to at most limit runes so it never trips
// Discord's length validation on display names.
func truncateToRuneLimit(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	ellipsis := "..."
	if limit <= len([]rune(ellipsis)) {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + ellipsis
}

// errGuildAvatarUpdate marks failures at the Discord avatar upload step.
var errGuildAvatarUpdate = errors.New("failed to update guild avatar")

// SyncGuildIdentity aligns the bot's visible identity in a guild (nickname and
// avatar) with the active character.
func SyncGuildIdentity(ctx context.Context, sm *session.Manager, imgClient images.ImageClient, s DiscordSession, guildID string) error {
	details, err := sm.GetCharacterDetails(ctx, guildID)
	if err != nil || details == nil || details.DisplayName == "" {
		return nil
	}

	if err := s.GuildMemberNickname(guildID, "@me", truncateToRuneLimit(details.DisplayName, discordNickLimit)); err != nil {
		return fmt.Errorf("failed to sync nickname: %w", err)
	}

	return ApplyCharacterAvatar(ctx, imgClient, s, guildID, details.CharacterID, details.ImageURL)
}

// ApplyCharacterAvatar sets the guild avatar to the character's image. Uses
// the local cache first, then tries to refetch via the image URL.
func ApplyCharacterAvatar(ctx context.Context, imgClient images.ImageClient, s DiscordSession, guildID, characterID, imageURL string) error {
	if imgClient == nil {
		logger.FromContext(ctx).Error("no image client available")
		return fmt.Errorf("no image client available")
	}

	path, err := imgClient.GetImage(guildID, characterID)
	if err != nil {
		if imageURL == "" {
			return nil
		}
		path, err = imgClient.SaveImage(ctx, guildID, characterID, imageURL)
		if err != nil {
			return fmt.Errorf("failed to download character image: %w", err)
		}
	}

	if fi, err := os.Stat(path); err == nil && fi.Size() > maxAvatarBytes {
		return fmt.Errorf("avatar image exceeds %d bytes", maxAvatarBytes)
	}

	dataURI, err := imgClient.ImageToBase64(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to encode character image: %w", err)
	}

	if err := s.UpdateGuildAvatar(guildID, dataURI); err != nil {
		return fmt.Errorf("%w: %w", errGuildAvatarUpdate, err)
	}
	return nil
}

// buildCharacterCardEmbed assembles the character card as a slice of
// messages (one embed slice per message) plus the avatar attachment (local
// cache first, stored hint URL as fallback). Message 1 carries the identity
// embed — display name, avatar thumbnail, and official name / series / ID
// fields — and each "### " spec section gets its own embed; sections that do
// not fit the per-message embed budget continue in follow-up messages, so
// the full spec is always shown.
func buildCharacterCardEmbed(imageClient images.ImageClient, guildID string, card *session.CharacterCard) ([][]*discordgo.MessageEmbed, []*discordgo.File, func()) {
	embed, files, closeFiles := characterAvatarEmbed(imageClient, guildID, card)

	field := func(name, value string, inline bool) *discordgo.MessageEmbedField {
		return &discordgo.MessageEmbedField{Name: name, Value: truncateToRuneLimit(value, embedFieldLimit), Inline: inline}
	}
	if card.OfficialName != "" {
		embed.Fields = append(embed.Fields, field("Official name", card.OfficialName, true))
	}
	if card.Series != "" {
		embed.Fields = append(embed.Fields, field("Series", card.Series, true))
	}
	embed.Fields = append(embed.Fields, field("ID", "`"+card.CharacterID+"`", true))

	// Discord caps the total of all embed text in one message at
	// embedTotalLimit (and the number of embeds at embedCountLimit), so each
	// message gets a fresh budget and sections roll over into follow-up
	// messages as budgets fill up. A section body that cannot fit any single
	// embed is truncated at the description cap.
	messages := [][]*discordgo.MessageEmbed{{embed}}
	budget := embedTotalLimit - embedTextLen(embed)
	for _, sec := range research.SplitSections(card.Description) {
		// Title plus room for the ellipsis truncateToRuneLimit may append.
		overhead := len([]rune(sec.Name)) + 3
		body := sec.Body
		// A section rolls into a fresh message whenever its body (at most
		// the description cap) does not fit the current budget. A body
		// larger than any single embed is then truncated at the cap.
		bodyLen := len([]rune(body))
		if bodyLen > embedDescriptionMax {
			bodyLen = embedDescriptionMax
		}
		if len(messages[len(messages)-1]) == embedCountLimit || overhead+bodyLen > budget {
			messages = append(messages, nil)
			budget = embedTotalLimit
		}
		allowance := budget - overhead
		if allowance > embedDescriptionMax {
			allowance = embedDescriptionMax
		}
		if len([]rune(body)) > allowance {
			body = truncateToRuneLimit(body, allowance)
		}
		secEmbed := sectionBodyEmbed(sec.Name, body)
		messages[len(messages)-1] = append(messages[len(messages)-1], secEmbed)
		budget -= embedTextLen(secEmbed)
	}
	return messages, files, closeFiles
}

// embedTextLen is how much of an embed's text counts toward the
// per-message embed total: title, description, and field names and values.
func embedTextLen(e *discordgo.MessageEmbed) int {
	total := len([]rune(e.Title)) + len([]rune(e.Description))
	for _, f := range e.Fields {
		total += len([]rune(f.Name)) + len([]rune(f.Value))
	}
	return total
}

// sectionBodyEmbed renders a spec section body as a single embed titled with
// the section name; bodies longer than Discord's description limit are
// truncated. A section with no body still gets its embed so the section name
// remains visible.
func sectionBodyEmbed(section, body string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       section,
		Description: truncateToRuneLimit(body, embedDescriptionMax),
		Color:       0x5865F2,
	}
}

// characterAvatarEmbed returns a base embed for the given card — its display
// name as the title and the cached avatar attached as the thumbnail (stored
// hint URL as fallback) — plus the files to send alongside. The returned
// file closer must be called only after the files have been sent.
func characterAvatarEmbed(imageClient images.ImageClient, guildID string, card *session.CharacterCard) (*discordgo.MessageEmbed, []*discordgo.File, func()) {
	embed := &discordgo.MessageEmbed{
		Title: card.DisplayName,
		Color: 0x5865F2,
	}

	var files []*discordgo.File
	var closer func()
	if path, err := imageClient.GetImage(guildID, card.CharacterID); err == nil {
		if f, err := os.Open(path); err == nil {
			name := filepath.Base(path)
			files = append(files, &discordgo.File{Name: name, Reader: f})
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: "attachment://" + name}
			closer = func() { _ = f.Close() }
		}
	} else if card.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: card.ImageURL}
	}
	if closer == nil {
		closer = func() {}
	}

	return embed, files, closer
}

// markChanges renders newText with insertions underlined (__…__) and
// deletions from oldText struck through (~~…~~), so a rewrite preview shows
// exactly what moved. If the texts are too large to diff, newText is returned
// unchanged.
//
// How it works, in four steps:
//  1. Split both texts into word tokens (case-insensitive).
//  2. Find the longest common subsequence (LCS) of tokens — the biggest set
//     of words that appear in both texts in the same order. These are the
//     "unchanged" anchor words; the algorithm works bottom-up in a memo
//     table so each pair of positions is solved only once.
//  3. Record the byte offset of every new word inside newText.
//  4. Walk the LCS table from the start. At each step the table tells us
//     whether the current words match (copy as-is), the old word was
//     dropped (strikethrough), or the new word is an addition (underline).
//     The output is built by slicing newText itself at the recorded
//     offsets, so original line breaks, spacing, and markdown survive;
//     deleted words (which don't exist in newText) are spliced in at the
//     position where they were dropped.
func markChanges(oldText, newText string) string {
	// Step 1: tokenize. Words are the unit of the diff so a changed phrase
	// highlights as whole words rather than a character soup.
	oldTokens := strings.Fields(oldText)
	newTokens := strings.Fields(newText)
	// Steps 2 and 3 cost one table cell and one walk step per token pair,
	// so bail out for pathologically large inputs and show the plain text.
	if len(oldTokens) == 0 || len(newTokens) == 0 || len(oldTokens)*len(newTokens) > 1_000_000 {
		return newText
	}

	// Step 2: fill memo[i][j] = length of the LCS of oldTokens[i:] and
	// newTokens[j:]. Filling from the end means every cell only ever reads
	// neighbors that are already computed.
	n, m := len(oldTokens), len(newTokens)
	memo := make([][]uint16, n+1)
	for i := range memo {
		memo[i] = make([]uint16, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case strings.EqualFold(oldTokens[i], newTokens[j]):
				// The words match (ignoring case): the common subsequence is
				// this pair plus the best alignment of everything after it.
				memo[i][j] = memo[i+1][j+1] + 1
			case memo[i+1][j] >= memo[i][j+1]:
				// No match. Dropping the old word leaves at least as long a
				// common tail as skipping the new word, so this cell's best
				// path goes "down": old word i will be a deletion.
				memo[i][j] = memo[i+1][j]
			default:
				// No match, and skipping the new word is better: this cell's
				// best path goes "right", so new word j will be an insertion.
				memo[i][j] = memo[i][j+1]
			}
		}
	}

	// Step 3: byte offset of every new word inside newText. The renderer slices
	// newText at these offsets, so it copies the real text (with its
	// original whitespace and markdown) instead of re-joining words.
	// Whitespace here means unicode.IsSpace, the same rule strings.Fields
	// used when splitting, so the offsets line up with the tokens.
	newRanges := make([][2]int, m)
	pos := 0
	for j := range newTokens {
		for pos < len(newText) {
			r, size := utf8.DecodeRuneInString(newText[pos:])
			if !unicode.IsSpace(r) {
				break
			}
			pos += size
		}
		newRanges[j] = [2]int{pos, pos + len(newTokens[j])}
		pos = newRanges[j][1]
	}

	// Helpers for the step 4 walk. It appends to out as it walks, and
	// consumed is how far through newText the plain copies have already
	// reached.
	var out strings.Builder
	consumed := 0
	// ensureSep separates a span from preceding text when the original
	// whitespace does not already provide one.
	ensureSep := func() {
		s := out.String()
		if len(s) > 0 {
			r, _ := utf8.DecodeLastRuneInString(s)
			if !unicode.IsSpace(r) {
				out.WriteString(" ")
			}
		}
	}
	plain := func(end int) {
		out.WriteString(newText[consumed:end])
		consumed = end
	}
	// insRun wraps new tokens [from, to) in one underline span.
	insRun := func(from, to int) {
		out.WriteString(newText[consumed:newRanges[from][0]])
		ensureSep()
		out.WriteString("__" + newText[newRanges[from][0]:newRanges[to-1][1]] + "__")
		consumed = newRanges[to-1][1]
	}
	// delRun wraps old tokens [from, to) in one strikethrough span.
	delRun := func(from, to int) {
		ensureSep()
		out.WriteString("~~" + strings.Join(oldTokens[from:to], " ") + "~~")
	}
	nextIsMatch := func(i, j int) bool {
		return i < n && j < m && strings.EqualFold(oldTokens[i], newTokens[j])
	}
	// goesLeft reports whether the best path through (i, j) drops old word i
	// (goes down the table) rather than skipping new word j (goes right).
	goesLeft := func(i, j int) bool {
		return memo[i+1][j] >= memo[i][j+1]
	}

	// Step 4: replay the table from (0, 0), turning it into three kinds of
	// runs. i indexes the old words, j the new ones; every step advances at
	// least one of them, so the walk always terminates.
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case nextIsMatch(i, j):
			// Anchor word: present in both texts. Copy it (and any
			// whitespace in between) from newText untouched.
			plain(newRanges[j][1])
			i++
			j++
		case goesLeft(i, j):
			// The table keeps dropping old words before the next match:
			// emit them all as one strikethrough run.
			k := i
			for k < n && !nextIsMatch(k, j) && goesLeft(k, j) {
				k++
			}
			delRun(i, k)
			i = k
		default:
			// The table skips new words: they are insertions. Group the run
			// while the words stay on the same line (a one-space gap), since
			// underline spans across newlines render unreliably in Discord.
			// The run also stops short of any word that would match the
			// current old word, so a coming anchor is not swallowed.
			k := j
			for k+1 < m && !nextIsMatch(i, k+1) && !goesLeft(i, k+1) && newRanges[k+1][0]-newRanges[k][1] == 1 {
				k++
			}
			insRun(j, k+1)
			j = k + 1
		}
	}
	// One side ran out: everything left in the old text is deleted, and
	// everything left in the new text is inserted (same line grouping).
	if i < n {
		delRun(i, n)
	}
	for j < m {
		k := j
		for k+1 < m && newRanges[k+1][0]-newRanges[k][1] == 1 {
			k++
		}
		insRun(j, k+1)
		j = k + 1
	}
	return out.String()
}

// markSpecChanges marks up the diff between two persona specifications
// section by section, so change spans never cross a "### " header line.
// Sections present only in newSpec are rendered without markup; sections
// dropped from the spec do not appear.
func markSpecChanges(oldSpec, newSpec string) string {
	oldBodies := map[string]string{}
	for _, sec := range research.SplitSections(oldSpec) {
		if _, ok := oldBodies[sec.Name]; !ok {
			oldBodies[sec.Name] = sec.Body
		}
	}
	var parts []string
	for _, sec := range research.SplitSections(newSpec) {
		if sec.Name == "" {
			if sec.Body != "" {
				parts = append(parts, markChanges(oldBodies[""], sec.Body))
			}
			continue
		}
		parts = append(parts, research.SectionHeaderPrefix+sec.Name+"\n"+markChanges(oldBodies[sec.Name], sec.Body))
	}
	return strings.Join(parts, "\n\n")
}
