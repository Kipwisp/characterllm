package commands

import (
	"characterllm/internal/llm"
	"context"
	"os"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func TestClearThreadCmd_Execute(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	ctx := context.Background()
	cmdCtx.Session.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: "char1", DisplayName: "Miles"})
	cmdCtx.Session.SetActiveCharacter(ctx, guildID, "char1")

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	i.GuildID = guildID

	cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "char1")
	// Seed history on both the default thread and a created thread.
	cmdCtx.Session.SaveMessage(ctx, guildID, "1", llm.RoleUser, "default hello")
	thread, err := cmdCtx.Session.CreateThread(ctx, guildID, "char1", "Side quest")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}
	cmdCtx.Session.SaveMessage(ctx, guildID, thread.ThreadID, llm.RoleUser, "side hello")

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &clearThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}
	if err := cmd.Execute(ctx, s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if content != "Thread cleared." {
		t.Errorf("unexpected reply %q", content)
	}

	// The active (created) thread is cleared; the default thread's history survives.
	count, err := cmdCtx.Session.GetHistoryCount(ctx, guildID, thread.ThreadID)
	if err != nil {
		t.Fatalf("GetHistoryCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected active thread history cleared, got %d", count)
	}
	count, err = cmdCtx.Session.GetHistoryCount(ctx, guildID, "1")
	if err != nil {
		t.Fatalf("GetHistoryCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected other thread history untouched, got %d", count)
	}
}

func TestClearThreadCmd_NoCharacter(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	i.GuildID = "guild1"

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	cmd := &clearThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if content == "" {
		t.Error("expected a no-character reply")
	}
}
