package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"characterllm/internal/research"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type identityCalls struct {
	nickname  string
	avatar    string
	nickSet   bool
	avatarSet bool
}

func TestSyncGuildIdentity(t *testing.T) {
	guildID := "guild1"
	charID := "char1"
	imageURL := "http://img.example/c.png"

	setup := func(t *testing.T) (*testDeps, *mockDiscordSession, *mockImageClient, *identityCalls) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		t.Cleanup(func() { os.Remove(dbPath) })

		sm := cmdCtx.Session
		sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID: charID,
			DisplayName: "Display Name",
		})
		sm.SetActiveCharacter(context.Background(), guildID, charID)

		calls := &identityCalls{}
		s.GuildMemberNicknameFn = func(g string, member string, nick string) error {
			calls.nickname, calls.nickSet = nick, true
			return nil
		}
		s.UpdateGuildAvatarFn = func(g string, dataURI string) error {
			calls.avatar, calls.avatarSet = dataURI, true
			return nil
		}

		mockImg := &mockImageClient{
			GetImageFn: func(g, c string) (string, error) {
				return "/tmp/cached.png", nil
			},
			ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
				return "data:image/png;base64,abc", nil
			},
		}
		cmdCtx.ImageClient = mockImg

		return cmdCtx, s, mockImg, calls
	}

	t.Run("sets nickname and avatar from cache", func(t *testing.T) {
		cmdCtx, s, _, calls := setup(t)
		cmdCtx.Session.SetCharacterImage(context.Background(), guildID, charID, imageURL)

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.nickSet || calls.nickname != "Display Name" {
			t.Errorf("expected nickname sync, got %+v", calls)
		}
		if !calls.avatarSet || calls.avatar != "data:image/png;base64,abc" {
			t.Errorf("expected avatar sync, got %+v", calls)
		}
	})

	t.Run("long display name truncated to limit", func(t *testing.T) {
		cmdCtx, s, _, calls := setup(t)
		longName := "This Character Name Deliberately Exceeds The Limit"
		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID: charID,
			DisplayName: longName,
		})

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.nickSet {
			t.Fatal("expected nickname sync")
		}
		if len([]rune(calls.nickname)) != discordNickLimit {
			t.Errorf("expected nickname truncated to %d runes, got %d: %q", discordNickLimit, len([]rune(calls.nickname)), calls.nickname)
		}
		if calls.nickname != truncateToRuneLimit(longName, discordNickLimit) {
			t.Errorf("expected ellipsized prefix truncation, got %q", calls.nickname)
		}
		if !strings.HasSuffix(calls.nickname, "...") {
			t.Errorf("truncated nickname should end with an ellipsis, got %q", calls.nickname)
		}
	})

	t.Run("no image: nickname only", func(t *testing.T) {
		cmdCtx, s, mockImg, calls := setup(t)
		mockImg.GetImageFn = func(g, c string) (string, error) {
			return "", errors.New("no cached image")
		}

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.nickSet {
			t.Error("expected nickname sync")
		}
		if calls.avatarSet {
			t.Errorf("expected no avatar sync, got %+v", calls)
		}
	})

	t.Run("cache miss triggers re-download", func(t *testing.T) {
		cmdCtx, s, mockImg, calls := setup(t)
		cmdCtx.Session.SetCharacterImage(context.Background(), guildID, charID, imageURL)

		var downloadedURL string
		mockImg.GetImageFn = func(g, c string) (string, error) {
			return "", errors.New("no cached image")
		}
		mockImg.SaveImageFn = func(ctx context.Context, g, c, url string) (string, error) {
			downloadedURL = url
			return "/tmp/redownloaded.png", nil
		}

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if downloadedURL != imageURL {
			t.Errorf("expected re-download from %s, got %s", imageURL, downloadedURL)
		}
		if !calls.avatarSet {
			t.Error("expected avatar sync after re-download")
		}
	})

	t.Run("avatar from cache without stored URL", func(t *testing.T) {
		// Attachment-sourced avatars persist no URL; the cache file alone
		// must be enough to apply the avatar.
		cmdCtx, s, _, calls := setup(t)

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.avatarSet || calls.avatar != "data:image/png;base64,abc" {
			t.Errorf("expected avatar from cache, got %+v", calls)
		}
	})

	t.Run("no active character: no-op", func(t *testing.T) {
		cmdCtx, s, _, calls := setup(t)

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, "empty-guild"); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if calls.nickSet || calls.avatarSet {
			t.Errorf("expected no sync calls, got %+v", calls)
		}
	})
}

func TestResolveCard(t *testing.T) {
	cmdCtx, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	ctx := context.Background()
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID:  "miles-morales-ca8da118",
		DisplayName:  "Miles Morales",
		OfficialName: "Miles G. Morales",
	})
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID:  "twin1",
		DisplayName:  "Twin",
		OfficialName: "Twin One",
	})
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID:  "twin2",
		DisplayName:  "Twin",
		OfficialName: "Twin Two",
	})

	t.Run("Direct ID", func(t *testing.T) {
		card, err := resolveCard(ctx, sm, guildID, "miles-morales-ca8da118")
		if err != nil || card == nil || card.CharacterID != "miles-morales-ca8da118" {
			t.Errorf("got %v (err %v)", card, err)
		}
	})

	t.Run("Case-insensitive display name", func(t *testing.T) {
		card, err := resolveCard(ctx, sm, guildID, "MILES morales")
		if err != nil || card == nil || card.CharacterID != "miles-morales-ca8da118" {
			t.Errorf("got %v (err %v)", card, err)
		}
	})

	t.Run("Official name", func(t *testing.T) {
		card, err := resolveCard(ctx, sm, guildID, "twin one")
		if err != nil || card == nil || card.CharacterID != "twin1" {
			t.Errorf("got %v (err %v)", card, err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := resolveCard(ctx, sm, guildID, "Nobody")
		if !errors.Is(err, ErrCardNotFound) {
			t.Errorf("expected ErrCardNotFound, got %v", err)
		}
	})

	t.Run("Placeholder none", func(t *testing.T) {
		_, err := resolveCard(ctx, sm, guildID, "none")
		if !errors.Is(err, ErrCardNotFound) {
			t.Errorf("expected ErrCardNotFound for placeholder, got %v", err)
		}
	})

	t.Run("Ambiguous", func(t *testing.T) {
		_, err := resolveCard(ctx, sm, guildID, "twin")
		var amb *CardAmbiguityError
		if !errors.As(err, &amb) || len(amb.Candidates) != 2 {
			t.Errorf("expected ambiguity with 2 candidates, got %v", err)
		}
	})
}

func TestAutocompleteCharacters(t *testing.T) {
	cmdCtx, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	ctx := context.Background()
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: "c1", DisplayName: "Miles Morales"})
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: "c2", DisplayName: "Mordecai"})
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: "c3", DisplayName: "Peter Parker"})

	t.Run("Empty query lists characters with ID values", func(t *testing.T) {
		choices := autocompleteCharacters(ctx, sm, guildID, "", false)
		if len(choices) != 3 {
			t.Fatalf("expected 3 card choices, got %d", len(choices))
		}
		if choices[0].Value != "c1" || choices[0].Name != "Miles Morales c1" {
			t.Errorf("unexpected first choice: %+v", choices[0])
		}
	})

	t.Run("Active character is marked", func(t *testing.T) {
		if err := sm.SetActiveCharacter(ctx, guildID, "c3"); err != nil {
			t.Fatalf("SetActiveCharacter failed: %v", err)
		}
		choices := autocompleteCharacters(ctx, sm, guildID, "", false)
		for _, c := range choices {
			switch c.Value {
			case "c3":
				if c.Name != "Peter Parker c3 (active)" {
					t.Errorf("expected the active marker on Peter Parker, got %+v", c)
				}
			default:
				if strings.HasSuffix(c.Name, " (active)") {
					t.Errorf("unexpected active marker on %+v", c)
				}
				if c.Value == currentCardName {
					t.Errorf("unexpected current suggestion: %+v", c)
				}
			}
		}
	})

	t.Run("Current choice is offered when requested", func(t *testing.T) {
		choices := autocompleteCharacters(ctx, sm, guildID, "", true)
		if len(choices) != 4 {
			t.Fatalf("expected 4 choices (current + 3 cards), got %d", len(choices))
		}
		if choices[0].Value != currentCardName || choices[0].Name != currentChoiceLabel {
			t.Errorf("expected the current choice first, got %+v", choices[0])
		}
		marked := false
		for _, c := range choices {
			if c.Value == "c3" && c.Name == "Peter Parker c3 (active)" {
				marked = true
			}
		}
		if !marked {
			t.Errorf("expected the active marker to survive alongside the current choice, got %v", choices)
		}
	})

	t.Run("Current choice follows the query", func(t *testing.T) {
		// The label matches, no card does: current plus the placeholder.
		choices := autocompleteCharacters(ctx, sm, guildID, "current", true)
		if len(choices) != 2 || choices[0].Value != currentCardName || choices[1].Value != "none" {
			t.Errorf("expected current and placeholder for 'current', got %v", choices)
		}
		// The label no longer matches: cards only.
		choices = autocompleteCharacters(ctx, sm, guildID, "mor", true)
		for _, c := range choices {
			if c.Value == currentCardName {
				t.Errorf("unexpected current suggestion for 'mor': %+v", c)
			}
		}
	})

	t.Run("Prefix preferred over substring", func(t *testing.T) {
		choices := autocompleteCharacters(ctx, sm, guildID, "mor", false)
		if len(choices) != 2 {
			t.Fatalf("expected 2 choices, got %d", len(choices))
		}
		if choices[0].Value != "c2" {
			t.Errorf("expected prefix match first, got %+v", choices[0])
		}
	})

	t.Run("Series name matches", func(t *testing.T) {
		sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: "c4", DisplayName: "Tank", Series: "Rick and Morty"})

		// Prefix match on the series.
		choices := autocompleteCharacters(ctx, sm, guildID, "rick", false)
		if len(choices) != 1 || choices[0].Value != "c4" {
			t.Errorf("expected Tank for 'rick', got %+v", choices)
		}

		// Substring match on the series.
		choices = autocompleteCharacters(ctx, sm, guildID, "morty", false)
		if len(choices) != 1 || choices[0].Value != "c4" {
			t.Errorf("expected Tank for 'morty', got %+v", choices)
		}
	})

	t.Run("Character ID matches", func(t *testing.T) {
		// The ID is what tells apart same-named cards, so it must be searchable.
		choices := autocompleteCharacters(ctx, sm, guildID, "c4", false)
		if len(choices) != 1 || choices[0].Value != "c4" {
			t.Errorf("expected Tank for ID query 'c4', got %+v", choices)
		}
		choices = autocompleteCharacters(ctx, sm, guildID, "4", false)
		if len(choices) != 1 || choices[0].Value != "c4" {
			t.Errorf("expected Tank for ID substring '4', got %+v", choices)
		}
	})

	t.Run("No matches returns the placeholder", func(t *testing.T) {
		choices := autocompleteCharacters(ctx, sm, guildID, "zzz", false)
		if len(choices) != 1 || choices[0].Value != "none" {
			t.Errorf("expected placeholder choice only, got %v", choices)
		}
	})

	t.Run("Empty guild returns the placeholder", func(t *testing.T) {
		choices := autocompleteCharacters(ctx, sm, "otherguild", "", false)
		if len(choices) != 1 || choices[0].Value != "none" {
			t.Errorf("expected placeholder choice only, got %v", choices)
		}
	})
}

func TestCardChoiceName(t *testing.T) {
	cases := []struct {
		name string
		card *session.CharacterCard
		want string
	}{
		{
			"display, series, and ID",
			&session.CharacterCard{DisplayName: "Miles Morales", Series: "Spider-Man", CharacterID: "miles-morales-1234"},
			"Miles Morales [Spider-Man] miles-morales-1234",
		},
		{
			"no series",
			&session.CharacterCard{DisplayName: "Geralt of Rivia", CharacterID: "geralt-of-rivia-0042"},
			"Geralt of Rivia geralt-of-rivia-0042",
		},
		{
			"long display name yields space but the ID survives intact",
			&session.CharacterCard{DisplayName: string(make([]byte, 90)), Series: "Series", CharacterID: "id-9999"},
			truncateToRuneLimit(string(make([]byte, 90)), 100-len("id-9999")-len("[Series] ")-1) + " [Series] id-9999",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cardChoiceName(tc.card)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if len([]rune(got)) > choiceNameLimit {
				t.Errorf("choice name exceeds %d runes: %d", choiceNameLimit, len([]rune(got)))
			}
		})
	}
}

func TestCapAutocompleteChoices(t *testing.T) {
	mk := func(n int) []*discordgo.ApplicationCommandOptionChoice {
		choices := make([]*discordgo.ApplicationCommandOptionChoice, n)
		for i := range choices {
			choices[i] = &discordgo.ApplicationCommandOptionChoice{Name: fmt.Sprintf("c%d", i), Value: fmt.Sprintf("v%d", i)}
		}
		return choices
	}
	if got := capAutocompleteChoices(mk(10)); len(got) != 10 {
		t.Fatalf("under limit: got %d choices, want 10", len(got))
	}
	if got := capAutocompleteChoices(mk(31)); len(got) != maxAutocompleteChoices {
		t.Fatalf("over limit: got %d choices, want %d", len(got), maxAutocompleteChoices)
	}
}

func TestAutocompleteCharacters_CapsAtLimitWithoutCurrent(t *testing.T) {
	cmdCtx, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	for i := 0; i < 31; i++ {
		id := fmt.Sprintf("c%02d", i)
		cmdCtx.Session.SaveCharacterCard(ctx, "guild1", &session.CharacterCard{CharacterID: id, DisplayName: "Name " + id})
	}

	choices := autocompleteCharacters(ctx, cmdCtx.Session, "guild1", "", false)
	if len(choices) != maxAutocompleteChoices {
		t.Fatalf("expected %d choices, got %d", maxAutocompleteChoices, len(choices))
	}
}

func TestAutocompleteCharacters_DetailInName(t *testing.T) {
	cmdCtx, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	cmdCtx.Session.SaveCharacterCard(ctx, "guild1", &session.CharacterCard{
		CharacterID: "arthur-morgan-a1b2c3d4",
		DisplayName: "Arthur Morgan",
		Series:      "Red Dead Redemption",
	})

	choices := autocompleteCharacters(ctx, cmdCtx.Session, "guild1", "arthur", false)
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	if choices[0].Name != "Arthur Morgan [Red Dead Redemption] arthur-morgan-a1b2c3d4" {
		t.Errorf("unexpected choice name %q", choices[0].Name)
	}
	if choices[0].Value != "arthur-morgan-a1b2c3d4" {
		t.Errorf("value must stay the character ID, got %q", choices[0].Value)
	}

	// Long name + detail stays within the 100-char choice cap.
	cmdCtx.Session.SaveCharacterCard(ctx, "guild1", &session.CharacterCard{
		CharacterID: "longchar-00000000",
		DisplayName: string(make([]byte, 90)),
		Series:      "A very long series name to push the choice label over the cap",
	})
	for _, c := range autocompleteCharacters(ctx, cmdCtx.Session, "guild1", string([]byte{97}), false) {
		if len([]rune(c.Name)) > 100 {
			t.Errorf("choice name exceeds 100 runes: %d", len([]rune(c.Name)))
		}
	}
}

func TestMarkChanges(t *testing.T) {
	tests := []struct {
		name, oldText, newText, want string
	}{
		{
			name:    "identical text is unchanged",
			oldText: "Slow cadence, dry wit.",
			newText: "Slow cadence, dry wit.",
			want:    "Slow cadence, dry wit.",
		},
		{
			name:    "changed sentence is struck through and underlined",
			oldText: "Slow cadence, dry wit.",
			newText: "Fast cadence, warm wit.",
			want:    "~~Slow cadence, dry wit.~~ __Fast cadence, warm wit.__",
		},
		{
			name:    "unchanged sentences are not touched",
			oldText: "He is cold. He likes rain.",
			newText: "He is warm. He likes rain.",
			want:    "~~He is cold.~~ __He is warm.__ He likes rain.",
		},
		{
			name:    "newline-separated sentences split the diff",
			oldText: "Line one.\nLine two.",
			newText: "Line one.\nLine three.",
			want:    "Line one.\n~~Line two.~~\n__Line three.__",
		},
		{
			name:    "case differences are not changes",
			oldText: "He is cold.",
			newText: "he is cold.",
			want:    "he is cold.",
		},
		{
			name:    "added lines are underlined",
			oldText: "Line one.",
			newText: "Line one.\nLine two.",
			want:    "Line one.\n__Line two.__",
		},
		{
			name:    "empty old text underlines the whole new text",
			oldText: "",
			newText: "Fresh section.",
			want:    "__Fresh section.__",
		},
		{
			name:    "empty old text underlines the multi-line new text",
			oldText: "",
			newText: "First line.\nSecond line.",
			want:    "__First line.__\n__Second line.__",
		},
		{
			name:    "empty new text strikes through the whole old text",
			oldText: "Gone one. Gone two.",
			newText: "",
			want:    "~~Gone one. Gone two.~~",
		},
		{
			name:    "both texts empty",
			oldText: "",
			newText: "",
			want:    "",
		},
		{
			name:    "markdown survives wrapping",
			oldText: "- **Species**: Human",
			newText: "- **Species**: Dragon",
			want:    "~~- **Species**: Human~~ __- **Species**: Dragon__",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markChanges(tc.oldText, tc.newText)
			if got != tc.want {
				t.Errorf("markChanges(%q, %q) = %q, want %q", tc.oldText, tc.newText, got, tc.want)
			}
			// Unbalanced markers render as stray symbols and swallow
			// following text in Discord.
			if strings.Count(got, "~~")%2 != 0 || strings.Count(got, "__")%2 != 0 {
				t.Errorf("unbalanced markers in %q", got)
			}
		})
	}
}

func TestMarkChanges_TooLargeFallsBack(t *testing.T) {
	oldText := strings.Repeat("old\n", 1100)
	newText := strings.Repeat("new\n", 1100)
	if got := markChanges(oldText, newText); got != newText {
		t.Errorf("expected the fallback to return newText unchanged, got %q...", got[:min(len(got), 40)])
	}
}

func TestMarkSpecChanges(t *testing.T) {
	oldSpec := "### Identity & Temperament\nCold and questioning.\n\n### Appearance\n- **Species**: Human"
	newSpec := "### Identity & Temperament\nPerpetually upbeat.\n\n### Appearance\n- **Species**: Dragon\n\n### Scenario\nNew scene."
	got := markSpecChanges(oldSpec, newSpec)

	// Headers stay clean so the marked-up text still parses as sections.
	sections := map[string]string{}
	for _, sec := range research.SplitSections(got) {
		sections[sec.Name] = sec.Body
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 parseable sections, got %+v", sections)
	}
	if !strings.Contains(sections["Identity & Temperament"], "~~") || !strings.Contains(sections["Identity & Temperament"], "__Perpetually") {
		t.Errorf("identity section should carry the diff markup: %q", sections["Identity & Temperament"])
	}
	if !strings.Contains(sections["Appearance"], "__- **Species**: Dragon__") {
		t.Errorf("appearance section should mark the change: %q", sections["Appearance"])
	}
	if sections["Scenario"] != "__New scene.__" {
		t.Errorf("new section must be fully underlined, got %q", sections["Scenario"])
	}
}

func TestMarkSpecChanges_DeletedSection(t *testing.T) {
	oldSpec := "### Identity & Temperament\nCold and questioning.\n\n### Greeting\nHello there.\nHow are you?"
	newSpec := "### Identity & Temperament\nCold and questioning."
	got := markSpecChanges(oldSpec, newSpec)

	sections := map[string]string{}
	for _, sec := range research.SplitSections(got) {
		sections[sec.Name] = sec.Body
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 parseable sections, got %+v", sections)
	}
	want := "~~Hello there.\nHow are you?~~"
	if sections["Greeting"] != want {
		t.Errorf("deleted section must be fully struck through, got %q (want %q)", sections["Greeting"], want)
	}
}

func TestTruncateToRuneLimit(t *testing.T) {
	if got := truncateToRuneLimit("short", 10); got != "short" {
		t.Errorf("within limit must be unchanged, got %q", got)
	}
	got := truncateToRuneLimit(strings.Repeat("a", 50), 10)
	if len([]rune(got)) != 10 || got != strings.Repeat("a", 7)+"..." {
		t.Errorf("truncated value = %q", got)
	}
	// Limits too small to fit the ellipsis fall back to a bare cut.
	if got := truncateToRuneLimit("abcdef", 2); got != "ab" {
		t.Errorf("tiny limit = %q", got)
	}
	// Multibyte runes must not be split.
	got = truncateToRuneLimit("héllo wörld", 8)
	if got != "héllo..." {
		t.Errorf("multibyte truncation = %q", got)
	}
}

func TestCardSections(t *testing.T) {
	spec := "Intro text.\n\n### Appearance\nTall.\n\n### Custom Section\nExtra.\n\n### Greeting\nHi."
	sections := cardSections(spec)

	// Preamble first, then the full canonical set (empty ones included),
	// then non-canonical sections in spec order.
	wantNames := append([]string{""}, append(append([]string{}, research.PersonaSectionOrder...), research.SectionScenario)...)
	wantNames = append(wantNames, "Custom Section")
	if len(sections) != len(wantNames) {
		t.Fatalf("expected %d sections, got %d: %+v", len(wantNames), len(sections), sections)
	}
	for i, want := range wantNames {
		if sections[i].Name != want {
			t.Errorf("section %d name = %q, want %q", i, sections[i].Name, want)
		}
	}
	bodies := map[string]string{}
	for _, s := range sections {
		bodies[s.Name] = s.Body
	}
	if bodies[""] != "Intro text." || bodies["Appearance"] != "Tall." || bodies["Greeting"] != "Hi." || bodies["Custom Section"] != "Extra." {
		t.Errorf("unexpected bodies: %+v", bodies)
	}
	for _, name := range []string{research.SectionIdentity, research.SectionVoice, research.SectionDialogue, research.SectionScenario} {
		if bodies[name] != "" {
			t.Errorf("section %q should be empty, got %q", name, bodies[name])
		}
	}
}
