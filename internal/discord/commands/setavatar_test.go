package commands

import (
	"context"
	"os"
	"testing"

	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func TestSetAvatarCmd(t *testing.T) {
	guildID := "guild1"
	charID := "char1"

	newInteraction := func(attachments []*discordgo.MessageAttachment, optAttachment *discordgo.MessageAttachment) *discordgo.InteractionCreate {
		data := discordgo.ApplicationCommandInteractionData{}
		if optAttachment != nil {
			data.Options = []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "image", Type: discordgo.ApplicationCommandOptionAttachment, Value: optAttachment.ID},
			}
			data.Resolved = &discordgo.ApplicationCommandInteractionDataResolved{
				Attachments: map[string]*discordgo.MessageAttachment{optAttachment.ID: optAttachment},
			}
		}
		i := &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				ID:   "i1",
				Type: discordgo.InteractionApplicationCommand,
				Data: data,
			},
		}
		i.GuildID = guildID
		if attachments != nil {
			i.Message = &discordgo.Message{Attachments: attachments}
		}
		return i
	}

	t.Run("no active character", func(t *testing.T) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		defer os.Remove(dbPath)

		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}

		cmd := &setAvatarCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
		if err := cmd.Execute(context.Background(), s, newInteraction(nil, nil)); err == nil {
			t.Error("expected error without active character")
		}
		if content != responses.SetAvatar.NoCharacter {
			t.Errorf("unexpected response: %s", content)
		}
	})

	t.Run("no source provided", func(t *testing.T) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		defer os.Remove(dbPath)

		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{CharacterID: charID, DisplayName: "C"}, nil)
		cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, charID)

		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}

		cmd := &setAvatarCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
		if err := cmd.Execute(context.Background(), s, newInteraction(nil, nil)); err == nil {
			t.Error("expected error without source")
		}
		if content != responses.SetAvatar.MissingSource {
			t.Errorf("unexpected response: %s", content)
		}
	})

	t.Run("happy path", func(t *testing.T) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		defer os.Remove(dbPath)

		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{CharacterID: charID, DisplayName: "C"}, nil)
		cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, charID)

		tmpImg, _ := os.CreateTemp("", "setavatar_test*.png")
		tmpImg.Write([]byte("fake-image"))
		tmpImg.Close()
		defer os.Remove(tmpImg.Name())

		var savedURL string
		var avatarDataURI string
		mockImg := &mockImageClient{
			SaveImageFn: func(ctx context.Context, g, c, url string) (string, error) {
				if c != charID {
					t.Errorf("expected image keyed by character %s, got %s", charID, c)
				}
				savedURL = url
				return tmpImg.Name(), nil
			},
			ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
				return "data:image/png;base64,abc", nil
			},
		}
		cmdCtx.ImageClient = mockImg

		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}
		s.UpdateGuildAvatarFn = func(g string, dataURI string) error {
			avatarDataURI = dataURI
			return nil
		}

		attachments := []*discordgo.MessageAttachment{
			{URL: "https://cdn.discordapp.com/att/1.png", ContentType: "image/png"},
		}
		cmd := &setAvatarCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
		if err := cmd.Execute(context.Background(), s, newInteraction(attachments, nil)); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if savedURL != "https://cdn.discordapp.com/att/1.png" {
			t.Errorf("expected attachment URL used, got %q", savedURL)
		}
		card, _ := cmdCtx.Session.GetCharacterCard(context.Background(), guildID, charID)
		if card.ImageURL != "" {
			t.Errorf("expected no URL persisted for attachment avatars, got %q", card.ImageURL)
		}
		if avatarDataURI != "data:image/png;base64,abc" {
			t.Errorf("expected avatar update, got %q", avatarDataURI)
		}
		if content != responses.SetAvatar.Success {
			t.Errorf("unexpected response: %s", content)
		}
	})

	t.Run("attachment option happy path", func(t *testing.T) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		defer os.Remove(dbPath)

		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{CharacterID: charID, DisplayName: "C"}, nil)
		cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, charID)

		tmpImg, _ := os.CreateTemp("", "setavatar_test*.png")
		tmpImg.Write([]byte("fake-image"))
		tmpImg.Close()
		defer os.Remove(tmpImg.Name())

		var savedURL string
		mockImg := &mockImageClient{
			SaveImageFn: func(ctx context.Context, g, c, url string) (string, error) {
				savedURL = url
				return tmpImg.Name(), nil
			},
			ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
				return "data:image/png;base64,abc", nil
			},
		}
		cmdCtx.ImageClient = mockImg

		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			return nil
		}
		s.UpdateGuildAvatarFn = func(g string, dataURI string) error { return nil }

		opt := &discordgo.MessageAttachment{
			ID:       "999",
			Filename: "picked.png",
			URL:      "https://cdn.discordapp.com/att/opt.png",
		}
		cmd := &setAvatarCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
		if err := cmd.Execute(context.Background(), s, newInteraction(nil, opt)); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if savedURL != "https://cdn.discordapp.com/att/opt.png" {
			t.Errorf("expected option attachment URL used, got %q", savedURL)
		}
	})

	t.Run("oversized image rejected", func(t *testing.T) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		defer os.Remove(dbPath)

		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{CharacterID: charID, DisplayName: "C"}, nil)
		cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, charID)

		bigImg, _ := os.CreateTemp("", "setavatar_test_big*.png")
		bigImg.Write(make([]byte, maxAvatarBytes+1))
		bigImg.Close()
		defer os.Remove(bigImg.Name())

		mockImg := &mockImageClient{
			SaveImageFn: func(ctx context.Context, g, c, url string) (string, error) {
				return bigImg.Name(), nil
			},
		}
		cmdCtx.ImageClient = mockImg

		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}

		attachments := []*discordgo.MessageAttachment{
			{URL: "https://cdn.discordapp.com/att/big.png", ContentType: "image/png"},
		}
		cmd := &setAvatarCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
		if err := cmd.Execute(context.Background(), s, newInteraction(attachments, nil)); err == nil {
			t.Error("expected error for oversized image")
		}
		if content != responses.SetAvatar.TooLarge {
			t.Errorf("unexpected response: %s", content)
		}

		card, _ := cmdCtx.Session.GetCharacterCard(context.Background(), guildID, charID)
		if card.ImageURL != "" {
			t.Errorf("expected no image persisted for rejected upload, got %q", card.ImageURL)
		}
	})
}
