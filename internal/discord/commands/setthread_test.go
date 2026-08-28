package commands

import (
	"context"
	"os"
	"strings"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func TestSetThreadCmd_Execute(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	ctx := context.Background()
	setupActiveCharacter(t, cmdCtx.Session, guildID)
	if err := cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "char1"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	created, err := cmdCtx.Session.CreateThread(ctx, guildID, "char1", "Side quest")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &setThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}

	t.Run("switch by thread ID", func(t *testing.T) {
		i := newThreadOptionInteraction("setthread", stringOption("thread", created.ThreadID))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		active, err := cmdCtx.Session.GetActiveThreadID(ctx, guildID, "char1")
		if err != nil || active != created.ThreadID {
			t.Fatalf("expected active thread %s, got %q (err %v)", created.ThreadID, active, err)
		}
		if content != "Now chatting in **Side quest**." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("switch appends the thread's last bot message", func(t *testing.T) {
		cmdCtx.Session.SaveMessage(ctx, guildID, created.ThreadID, "user", "what was I thinking?")
		cmdCtx.Session.SaveMessage(ctx, guildID, created.ThreadID, "assistant", "You were plotting.")
		cmdCtx.Session.SaveMessage(ctx, guildID, created.ThreadID, "user", "later")
		i := newThreadOptionInteraction("setthread", stringOption("thread", "1"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		i = newThreadOptionInteraction("setthread", stringOption("thread", created.ThreadID))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !strings.HasSuffix(content, "\n\nYou were plotting.") {
			t.Errorf("expected the last bot message appended, got %q", content)
		}
	})

	t.Run("switch to an empty thread falls back to the greeting", func(t *testing.T) {
		if err := cmdCtx.Session.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
			CharacterID: "char1",
			DisplayName: "Miles",
			Description: "### Identity\nSomeone\n\n### Greeting\nHey, it's me.",
		}); err != nil {
			t.Fatalf("SaveCharacterCard failed: %v", err)
		}
		fresh, err := cmdCtx.Session.CreateThread(ctx, guildID, "char1", "Fresh start")
		if err != nil {
			t.Fatalf("CreateThread failed: %v", err)
		}
		i := newThreadOptionInteraction("setthread", stringOption("thread", fresh.ThreadID))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !strings.HasSuffix(content, "\n\nHey, it's me.") {
			t.Errorf("expected the greeting appended, got %q", content)
		}
	})

	t.Run("switch back to the default thread", func(t *testing.T) {
		i := newThreadOptionInteraction("setthread", stringOption("thread", "1"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		active, err := cmdCtx.Session.GetActiveThreadID(ctx, guildID, "char1")
		if err != nil || active != "1" {
			t.Fatalf("expected active thread %q, got %q (err %v)", "1", active, err)
		}
	})

	t.Run("current key is not a switch target", func(t *testing.T) {
		i := newThreadOptionInteraction("setthread", stringOption("thread", "current"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if content != "That thread doesn't exist for the active character." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("unknown thread is reported", func(t *testing.T) {
		i := newThreadOptionInteraction("setthread", stringOption("thread", "99"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if content != "That thread doesn't exist for the active character." {
			t.Errorf("unexpected reply %q", content)
		}
	})
}

func TestSetThreadCmd_NoCharacter(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	i := newThreadOptionInteraction("setthread", stringOption("thread", "1"))
	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &setThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if content == "" {
		t.Error("expected a no-character reply")
	}
}
