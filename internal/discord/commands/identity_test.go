package commands

import (
	"context"
	"errors"
	"os"
	"testing"

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

	setup := func(t *testing.T) (*mockCommandContext, *mockDiscordSession, *mockImageClient, *identityCalls) {
		cmdCtx, s, dbPath := setupCommandTest(t)
		t.Cleanup(func() { os.Remove(dbPath) })

		sm := cmdCtx.Session
		sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID: charID,
			DisplayName: "Display Name",
		}, nil)
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

		if err := SyncGuildIdentity(context.Background(), cmdCtx, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.nickSet || calls.nickname != "Display Name" {
			t.Errorf("expected nickname sync, got %+v", calls)
		}
		if !calls.avatarSet || calls.avatar != "data:image/png;base64,abc" {
			t.Errorf("expected avatar sync, got %+v", calls)
		}
	})

	t.Run("no image: nickname only", func(t *testing.T) {
		cmdCtx, s, mockImg, calls := setup(t)
		mockImg.GetImageFn = func(g, c string) (string, error) {
			return "", errors.New("no cached image")
		}

		if err := SyncGuildIdentity(context.Background(), cmdCtx, s, guildID); err != nil {
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

		if err := SyncGuildIdentity(context.Background(), cmdCtx, s, guildID); err != nil {
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

		if err := SyncGuildIdentity(context.Background(), cmdCtx, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.avatarSet || calls.avatar != "data:image/png;base64,abc" {
			t.Errorf("expected avatar from cache, got %+v", calls)
		}
	})

	t.Run("no active character: no-op", func(t *testing.T) {
		cmdCtx, s, _, calls := setup(t)

		if err := SyncGuildIdentity(context.Background(), cmdCtx, s, "empty-guild"); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if calls.nickSet || calls.avatarSet {
			t.Errorf("expected no sync calls, got %+v", calls)
		}
	})
}

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

		cmd := Get("setavatar")
		if err := cmd.Execute(context.Background(), cmdCtx, s, newInteraction(nil, nil)); err == nil {
			t.Error("expected error without active character")
		}
		if content != "No active character in this server. Use /setcharacter first." {
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

		cmd := Get("setavatar")
		if err := cmd.Execute(context.Background(), cmdCtx, s, newInteraction(nil, nil)); err == nil {
			t.Error("expected error without source")
		}
		if content != "Provide an image via the image option or an attachment." {
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
		cmd := Get("setavatar")
		if err := cmd.Execute(context.Background(), cmdCtx, s, newInteraction(attachments, nil)); err != nil {
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
		if content != "Avatar updated successfully!" {
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
		cmd := Get("setavatar")
		if err := cmd.Execute(context.Background(), cmdCtx, s, newInteraction(nil, opt)); err != nil {
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
		cmd := Get("setavatar")
		if err := cmd.Execute(context.Background(), cmdCtx, s, newInteraction(attachments, nil)); err == nil {
			t.Error("expected error for oversized image")
		}
		if content != "That image is too large to use as a Discord avatar." {
			t.Errorf("unexpected response: %s", content)
		}

		card, _ := cmdCtx.Session.GetCharacterCard(context.Background(), guildID, charID)
		if card.ImageURL != "" {
			t.Errorf("expected no image persisted for rejected upload, got %q", card.ImageURL)
		}
	})
}
