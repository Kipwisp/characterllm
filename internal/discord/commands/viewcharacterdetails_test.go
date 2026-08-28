package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func viewInteraction(t *testing.T, nameValue string) *discordgo.InteractionCreate {
	t.Helper()
	data := discordgo.ApplicationCommandInteractionData{Name: "viewcharacterdetails"}
	if nameValue != "" {
		data.Options = []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "name", Value: nameValue, Type: discordgo.ApplicationCommandOptionString},
		}
	}
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: data,
		},
	}
	i.GuildID = "guild1"
	return i
}

func TestViewCharacterCmd_ViewByDisplayName(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	guildID := "guild1"
	card := &session.CharacterCard{
		CharacterID:  "miles-morales-ca8da118",
		OfficialName: "Miles Morales",
		DisplayName:  "Miles Morales",
		Series:       "Spider-Man",
		Description:  "### Identity & Temperament\nBrash and brave.",
	}
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, card)

	var embed *discordgo.MessageEmbed
	var allEmbeds []*discordgo.MessageEmbed
	var files []*discordgo.File
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		allEmbeds = response.Data.Embeds
		embed = response.Data.Embeds[0]
		files = response.Data.Files
		return nil
	}

	i := viewInteraction(t, "miles morales")

	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if embed == nil {
		t.Fatal("expected an embed response")
	}
	if embed.Title != "Miles Morales" {
		t.Errorf("unexpected title %q", embed.Title)
	}
	if !embedContains(allEmbeds, "Brash and brave") {
		t.Errorf("persona spec missing from section embeds: %+v", allEmbeds)
	}
	if !embedHasTitle(allEmbeds, "Identity & Temperament") {
		t.Errorf("expected a section embed titled 'Identity & Temperament': %+v", allEmbeds)
	}
	if len(files) != 0 {
		t.Errorf("expected no files without a cached image, got %d", len(files))
	}
	if embed.Image != nil {
		t.Errorf("expected no embed image, got %q", embed.Image.URL)
	}

	wantFields := map[string]string{
		"Official name": "Miles Morales",
		"ID":            "`miles-morales-ca8da118`",
		"Series":        "Spider-Man",
	}
	got := map[string]string{}
	for _, f := range embed.Fields {
		got[f.Name] = f.Value
	}
	for name, want := range wantFields {
		if got[name] != want {
			t.Errorf("field %q = %q, want %q", name, got[name], want)
		}
	}
	if _, ok := got["Scenario"]; ok {
		t.Error("expected no Scenario field in the embed")
	}
	// Series precedes ID in the field order.
	order := []string{}
	for _, f := range embed.Fields {
		order = append(order, f.Name)
	}
	if iSeries, iID := index(order, "Series"), index(order, "ID"); iSeries == -1 || iID == -1 || iSeries > iID {
		t.Errorf("expected Series before ID, got order %v", order)
	}
	if _, ok := got["Source"]; ok {
		t.Error("expected no Source field in the embed")
	}
}

func TestViewCharacterCmd_ViewWithCachedImage(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
	})

	imgPath := filepath.Join(t.TempDir(), "char1.jpg")
	if err := os.WriteFile(imgPath, []byte("imgdata"), 0o644); err != nil {
		t.Fatalf("failed to write image file: %v", err)
	}
	cmdCtx.ImageClient = &mockImageClient{
		GetImageFn: func(guildID, characterID string) (string, error) {
			return imgPath, nil
		},
	}
	img := cmdCtx.ImageClient

	var embed *discordgo.MessageEmbed
	var files []*discordgo.File
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		embed = response.Data.Embeds[0]
		files = response.Data.Files
		return nil
	}

	i := viewInteraction(t, "char1")

	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: img}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(files) != 1 || files[0].Name != "char1.jpg" {
		t.Fatalf("expected one file named char1.jpg, got %v", files)
	}
	if embed.Image != nil {
		t.Errorf("expected no large embed image, got %v", embed.Image)
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL != "attachment://char1.jpg" {
		t.Errorf("expected attachment thumbnail, got %v", embed.Thumbnail)
	}
}

func TestViewCharacterCmd_ViewWithImageURIFallback(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}
	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}

	var embed *discordgo.MessageEmbed
	var files []*discordgo.File
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		embed = response.Data.Embeds[0]
		files = response.Data.Files
		return nil
	}

	i := viewInteraction(t, "char1")
	card := &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
		ImageURL:    "https://example.com/img.jpg",
	}
	if err := cmd.viewCard(context.Background(), s, i, card); err != nil {
		t.Fatalf("viewCard failed: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected no files, got %d", len(files))
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL != "https://example.com/img.jpg" {
		t.Errorf("expected thumbnail URL fallback, got %v", embed.Thumbnail)
	}
}

func TestViewCharacterCmd_ViewNotFound(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	i := viewInteraction(t, "nobody")

	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(content, "/createcharacter") {
		t.Errorf("expected not-found create suggestion, got %q", content)
	}
}

func TestViewCharacterCmd_EmbedCaps(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	long := strings.Repeat("x", 5000)
	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
		Description: long,
		Series:      long,
	})

	var embeds []*discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		embeds = response.Data.Embeds
		return nil
	}

	i := viewInteraction(t, "char1")

	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(embeds) < 2 {
		t.Fatalf("expected identity + section embeds, got %d", len(embeds))
	}
	sectionChars := 0
	messageTotal := 0
	for idx, embed := range embeds {
		if desc := len([]rune(embed.Description)); desc > embedDescriptionMax {
			t.Errorf("embed %d description exceeds cap: %d", idx, desc)
		}
		messageTotal += embedTextLen(embed)
		if idx > 0 {
			// Strip the truncation ellipsis so only spec content is counted.
			sectionChars += len([]rune(strings.TrimSuffix(embed.Description, "...")))
		}
	}
	// The 6000 cap applies to the total of all embeds in the message.
	if messageTotal > embedTotalLimit {
		t.Errorf("message embed text total %d exceeds Discord's %d cap", messageTotal, embedTotalLimit)
	}
	// The oversized section is truncated at the description limit.
	if sectionChars != embedDescriptionMax-3 {
		t.Errorf("expected section truncated to %d runes, got %d", embedDescriptionMax-3, sectionChars)
	}
}

func TestViewCharacterCmd_EmbedCaps_MultiMessage(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	// Six 1200-rune sections: well over the 6000 per-message embed total,
	// so the overflow must continue in follow-up messages rather than being
	// dropped.
	var spec string
	for i := 0; i < 6; i++ {
		spec += "### Section " + string(rune('A'+i)) + "\n" + strings.Repeat("w", 1200) + "\n\n"
	}
	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
		Description: spec,
	})

	var firstEmbeds []*discordgo.MessageEmbed
	var followUps [][]*discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		firstEmbeds = response.Data.Embeds
		return nil
	}
	s.ChannelMessageSendComplexFn = func(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
		followUps = append(followUps, msg.Embeds)
		return &discordgo.Message{ID: "follow"}, nil
	}

	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, viewInteraction(t, "char1")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(firstEmbeds) < 2 {
		t.Fatalf("expected identity + section embeds in message 1, got %d", len(firstEmbeds))
	}
	if len(followUps) == 0 {
		t.Fatal("expected overflow sections in follow-up messages")
	}

	// Every message stays within both caps and all six sections are present
	// exactly once across the messages, untruncated.
	seen := map[string]bool{}
	for idx, message := range append([][]*discordgo.MessageEmbed{firstEmbeds}, followUps...) {
		total := 0
		for _, embed := range message {
			total += embedTextLen(embed)
			if len(embed.Description) > 0 {
				if strings.HasSuffix(embed.Description, "...") {
					t.Errorf("message %d: section was truncated: %q", idx, embed.Title)
				}
				seen[embed.Title] = true
			}
		}
		if len(message) > embedCountLimit {
			t.Errorf("message %d has %d embeds, over the %d cap", idx, len(message), embedCountLimit)
		}
		if total > embedTotalLimit {
			t.Errorf("message %d embed text total %d exceeds Discord's %d cap", idx, total, embedTotalLimit)
		}
	}
	for i := 0; i < 6; i++ {
		name := "Section " + string(rune('A'+i))
		if !seen[name] {
			t.Errorf("section %q missing from the card messages", name)
		}
	}
}

func TestViewCharacterDetailsCmd_EmbedCaps_ShortSpecUntruncated(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	spec := strings.Repeat("y", 3800)
	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
		Description: spec,
	})

	var embeds []*discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		embeds = response.Data.Embeds
		return nil
	}

	i := viewInteraction(t, "char1")
	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(embeds) != 2 || embeds[1].Description != spec {
		t.Errorf("short spec should be untruncated in a single section embed, got %+v", embeds)
	}
}

func TestViewCharacterDetailsCmd_ViewCurrent(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	guildID := "guild1"
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Active Character",
		Description: "The active spec.",
	})
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char2",
		DisplayName: "Other Character",
	})
	if err := cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, "char1"); err != nil {
		t.Fatalf("SetActiveCharacter failed: %v", err)
	}

	var embed *discordgo.MessageEmbed
	var allEmbeds []*discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		embed = response.Data.Embeds[0]
		allEmbeds = response.Data.Embeds
		return nil
	}

	i := viewInteraction(t, "current")
	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if embed == nil {
		t.Fatal("expected an embed response")
	}
	if embed.Title != "Active Character" {
		t.Errorf("expected the active character's card, got title %q", embed.Title)
	}
	if !embedContains(allEmbeds, "The active spec.") {
		t.Errorf("persona spec missing from section embeds: %+v", allEmbeds)
	}
}

func TestViewCharacterDetailsCmd_ViewCurrent_NoneActive(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.ImageClient = &mockImageClient{}

	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
	})

	var content string
	var ephemeral bool
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		ephemeral = response.Data.Flags&discordgo.MessageFlagsEphemeral != 0
		return nil
	}

	i := viewInteraction(t, "CURRENT")
	cmd := &viewCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if content == "" {
		t.Fatal("expected a response")
	}
	if !ephemeral {
		t.Error("expected an ephemeral response")
	}
}

func TestAutocompleteCharacters_ActiveMarker(t *testing.T) {
	cmdCtx, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	cmdCtx.Session.SaveCharacterCard(ctx, "guild1", &session.CharacterCard{
		CharacterID: "miles-morales-ca8da118",
		DisplayName: "Miles Morales",
	})
	cmdCtx.Session.SaveCharacterCard(ctx, "guild1", &session.CharacterCard{
		CharacterID: "peter-parker-00000001",
		DisplayName: "Peter Parker",
	})
	if err := cmdCtx.Session.SetActiveCharacter(ctx, "guild1", "miles-morales-ca8da118"); err != nil {
		t.Fatalf("SetActiveCharacter failed: %v", err)
	}

	// Empty query offers "current" first and marks the active character.
	choices := autocompleteCharacters(ctx, cmdCtx.Session, "guild1", "", true)
	if len(choices) != 3 {
		t.Fatalf("expected 3 choices (current + 2 cards), got %v", choices)
	}
	if choices[0].Value != currentCardName || choices[0].Name != currentChoiceLabel {
		t.Errorf("expected the current choice first, got %+v", choices[0])
	}
	for _, c := range choices {
		switch c.Value {
		case "miles-morales-ca8da118":
			if c.Name != "Miles Morales miles-morales-ca8da118 (active)" {
				t.Errorf("expected the active marker on Miles, got %+v", c)
			}
		default:
			if strings.HasSuffix(c.Name, " (active)") {
				t.Errorf("unexpected active marker on %+v", c)
			}
		}
	}

	// A matching name still surfaces with the marker.
	choices = autocompleteCharacters(ctx, cmdCtx.Session, "guild1", "mil", true)
	if len(choices) != 1 || choices[0].Name != "Miles Morales miles-morales-ca8da118 (active)" {
		t.Errorf("expected the marked card match for 'mil', got %v", choices)
	}

	// No card matches: current plus the placeholder.
	choices = autocompleteCharacters(ctx, cmdCtx.Session, "guild1", "zzz", true)
	if len(choices) != 2 || choices[0].Value != currentCardName || choices[1].Value != "none" {
		t.Errorf("expected current and placeholder, got %v", choices)
	}
}

func embedContains(embeds []*discordgo.MessageEmbed, substr string) bool {
	for _, e := range embeds {
		if strings.Contains(e.Description, substr) {
			return true
		}
	}
	return false
}

func embedHasTitle(embeds []*discordgo.MessageEmbed, title string) bool {
	for _, e := range embeds {
		if e.Title == title {
			return true
		}
	}
	return false
}

func index(values []string, target string) int {
	for i, v := range values {
		if v == target {
			return i
		}
	}
	return -1
}

// The avatar file must stay open until the caller has sent it: discordgo
// reads the reader when building the request, which happens after the embed
// helpers return.
func TestCharacterAvatarEmbed_FileReadableAfterReturn(t *testing.T) {
	path := t.TempDir() + "/avatar.png"
	if err := os.WriteFile(path, []byte("png-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	client := &mockImageClient{
		GetImageFn: func(g, c string) (string, error) { return path, nil },
	}
	card := &session.CharacterCard{CharacterID: "char1", DisplayName: "Miles Morales"}

	embed, files, closeFiles := characterAvatarEmbed(client, "guild1", card)
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d", len(files))
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL != "attachment://avatar.png" {
		t.Fatalf("unexpected thumbnail: %+v", embed.Thumbnail)
	}

	// Simulates discordgo reading the attachment after the helper returned.
	body, err := io.ReadAll(files[0].Reader)
	if err != nil {
		t.Fatalf("file must remain readable after the helper returns: %v", err)
	}
	if string(body) != "png-data" {
		t.Errorf("read %q, want the file contents", body)
	}

	closeFiles()
	if _, err := io.ReadAll(files[0].Reader); err == nil {
		t.Error("expected the reader to be closed after closeFiles")
	}
}
