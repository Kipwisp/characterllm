package commands

import (
	"context"
	"os"
	"strings"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func newThreadOptionInteraction(name string, opts ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    name,
				Options: opts,
			},
		},
	}
	i.GuildID = "guild1"
	return i
}

func setupActiveCharacter(t *testing.T, sm *session.Manager, guildID string) {
	t.Helper()
	ctx := context.Background()
	if err := sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: "char1", DisplayName: "Miles"}); err != nil {
		t.Fatalf("SaveCharacterCard failed: %v", err)
	}
	if err := sm.SetActiveCharacter(ctx, guildID, "char1"); err != nil {
		t.Fatalf("SetActiveCharacter failed: %v", err)
	}
}

func TestNewThreadCmd_Execute(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	ctx := context.Background()
	setupActiveCharacter(t, cmdCtx.Session, guildID)
	if err := cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "char1"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &newThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}

	t.Run("explicit name", func(t *testing.T) {
		i := newThreadOptionInteraction("newthread", stringOption("name", "The Long Game"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		thread, err := cmdCtx.Session.GetThread(ctx, guildID, "char1", "2")
		if err != nil || thread == nil || thread.Name != "The Long Game" || !thread.Active {
			t.Fatalf("expected active thread 2 named The Long Game, got %+v (err %v)", thread, err)
		}
		if content != "Created thread **The Long Game** — now active." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("default numbered name skips taken names", func(t *testing.T) {
		i := newThreadOptionInteraction("newthread")
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		// The default name fills the first free number (Thread 1 is taken by
		// the default thread, so this one is Thread 2) on thread ID 3.
		thread, err := cmdCtx.Session.GetThread(ctx, guildID, "char1", "3")
		if err != nil || thread == nil || thread.Name != "Thread 2" || !thread.Active {
			t.Fatalf("expected active thread 3 named Thread 2, got %+v (err %v)", thread, err)
		}
	})

	t.Run("duplicate name is rejected without creating", func(t *testing.T) {
		i := newThreadOptionInteraction("newthread", stringOption("name", "The Long Game"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if content != "The active character already has a thread named **The Long Game**." {
			t.Errorf("unexpected reply %q", content)
		}
		count, _ := cmdCtx.Session.CountCharacterThreads(ctx, guildID, "char1")
		if count != 3 {
			t.Errorf("expected no new thread, got %d threads", count)
		}
	})
}

func TestNewThreadCmd_AppendsGreeting(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	ctx := context.Background()
	if err := cmdCtx.Session.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles",
		Description: "### Identity\nSomeone\n\n### Greeting\nYo. What's up?",
	}); err != nil {
		t.Fatalf("SaveCharacterCard failed: %v", err)
	}
	if err := cmdCtx.Session.SetActiveCharacter(ctx, guildID, "char1"); err != nil {
		t.Fatalf("SetActiveCharacter failed: %v", err)
	}
	if err := cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "char1"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &newThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}
	i := newThreadOptionInteraction("newthread", stringOption("name", "Fresh"))
	if err := cmd.Execute(ctx, s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.HasSuffix(content, "\n\nYo. What's up?") {
		t.Errorf("expected the greeting appended, got %q", content)
	}
}

func TestNewThreadCmd_NoCharacter(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	i := newThreadOptionInteraction("newthread", stringOption("name", "X"))
	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &newThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if content == "" {
		t.Error("expected a no-character reply")
	}
}
