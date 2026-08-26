package commands

import (
	"context"
	"errors"
	"os"
	"testing"

	"characterllm/internal/session"
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
		}, nil)

		if err := SyncGuildIdentity(context.Background(), cmdCtx.Session, cmdCtx.ImageClient, s, guildID); err != nil {
			t.Fatalf("SyncGuildIdentity failed: %v", err)
		}
		if !calls.nickSet {
			t.Fatal("expected nickname sync")
		}
		if len([]rune(calls.nickname)) != discordNickLimit {
			t.Errorf("expected nickname truncated to %d runes, got %d: %q", discordNickLimit, len([]rune(calls.nickname)), calls.nickname)
		}
		if calls.nickname != string([]rune(longName)[:discordNickLimit]) {
			t.Errorf("expected prefix truncation, got %q", calls.nickname)
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
