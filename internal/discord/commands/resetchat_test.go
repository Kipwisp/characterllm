package commands

import (
	"context"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestResetChatCmd_Execute(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	i.GuildID = guildID

	// Seed some history
	cmdCtx.Session.SaveMessage(context.Background(), guildID, "", "user", "Hello")

	respondCalled := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		respondCalled = true
		return nil
	}

	cmd := &resetChatCmd{}
	err := cmd.Execute(context.Background(), cmdCtx, s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !respondCalled {
		t.Error("InteractionRespond was not called")
	}

	count, err := cmdCtx.Session.GetHistoryCount(context.Background(), guildID, "")
	if err != nil {
		t.Fatalf("GetHistoryCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected history count 0, got %d", count)
	}
}
