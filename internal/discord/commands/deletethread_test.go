package commands

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestDeleteThreadCmd_ConfirmAndDelete(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	ctx := context.Background()
	setupActiveCharacter(t, cmdCtx.Session, guildID)
	if err := cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "char1"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	first, err := cmdCtx.Session.CreateThread(ctx, guildID, "char1", "Side quest")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}
	second, err := cmdCtx.Session.CreateThread(ctx, guildID, "char1", "Distant star")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}
	cmdCtx.Session.SaveMessage(ctx, guildID, first.ThreadID, "user", "doomed")

	var capturedEmbed *discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) > 0 {
			capturedEmbed = response.Data.Embeds[0]
		}
		return nil
	}

	cmd := &deleteThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}

	t.Run("confirmation offers delete for a non-last thread", func(t *testing.T) {
		i := newThreadOptionInteraction("deletethread", stringOption("thread", first.ThreadID))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if capturedEmbed == nil || capturedEmbed.Title != "Side quest" {
			t.Fatalf("expected confirmation embed titled Side quest, got %+v", capturedEmbed)
		}
		if !strings.Contains(capturedEmbed.Description, "permanently deletes") {
			t.Errorf("expected a deletion warning, got %q", capturedEmbed.Description)
		}
		var messages string
		for _, f := range capturedEmbed.Fields {
			if f.Name == "Messages" {
				messages = f.Value
			}
		}
		if messages != "1" {
			t.Errorf("expected the Messages field to report 1 message, got %q", messages)
		}
	})

	t.Run("confirm deletes the thread and its history", func(t *testing.T) {
		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}
		i := newComponentInteraction(guildID, deleteThreadConfirmID(first.ThreadID))
		cmd.handleDeleteConfirm(ctx, s, i)

		if content != "Deleted thread **Side quest**." {
			t.Errorf("unexpected reply %q", content)
		}
		if th, _ := cmdCtx.Session.GetThread(ctx, guildID, "char1", first.ThreadID); th != nil {
			t.Error("expected the thread row to be gone")
		}
		count, _ := cmdCtx.Session.GetHistoryCount(ctx, guildID, first.ThreadID)
		if count != 0 {
			t.Errorf("expected the deleted thread's history to be empty, got %d", count)
		}
	})

	t.Run("deleting the active thread hands the pointer to a survivor", func(t *testing.T) {
		active, err := cmdCtx.Session.GetActiveThreadID(ctx, guildID, "char1")
		if err != nil || active != second.ThreadID {
			t.Fatalf("expected active thread %s, got %q (err %v)", second.ThreadID, active, err)
		}
		i := newComponentInteraction(guildID, deleteThreadConfirmID(second.ThreadID))
		cmd.handleDeleteConfirm(ctx, s, i)
		active, err = cmdCtx.Session.GetActiveThreadID(ctx, guildID, "char1")
		if err != nil || active != "1" {
			t.Errorf("expected active pointer handed to the default thread, got %q (err %v)", active, err)
		}
	})

	t.Run("cancel leaves the thread untouched", func(t *testing.T) {
		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}
		i := newComponentInteraction(guildID, deleteThreadCancelID(""))
		cmd.handleDeleteCancel(ctx, s, i)
		if content != "Thread deletion cancelled." {
			t.Errorf("unexpected reply %q", content)
		}
		if th, _ := cmdCtx.Session.GetThread(ctx, guildID, "char1", "1"); th == nil {
			t.Error("expected the default thread to survive the cancel")
		}
	})
}

func TestDeleteThreadCmd_LastThreadClears(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	ctx := context.Background()
	setupActiveCharacter(t, cmdCtx.Session, guildID)
	if err := cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "char1"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	cmdCtx.Session.SaveMessage(ctx, guildID, "1", "user", "legacy")

	var capturedEmbed *discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) > 0 {
			capturedEmbed = response.Data.Embeds[0]
		}
		return nil
	}

	cmd := &deleteThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}

	t.Run("confirmation offers a clear for the last thread", func(t *testing.T) {
		i := newThreadOptionInteraction("deletethread", stringOption("thread", "current"))
		if err := cmd.Execute(ctx, s, i); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if capturedEmbed == nil || !strings.Contains(capturedEmbed.Description, "clears its conversation") {
			t.Fatalf("expected a clear warning, got %+v", capturedEmbed)
		}
	})

	t.Run("confirm clears the conversation but keeps the thread", func(t *testing.T) {
		var content string
		s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
			content = response.Data.Content
			return nil
		}
		i := newComponentInteraction(guildID, deleteThreadConfirmID("1"))
		cmd.handleDeleteConfirm(ctx, s, i)

		if content != "Cleared the conversation in **Thread 1**." {
			t.Errorf("unexpected reply %q", content)
		}
		count, _ := cmdCtx.Session.CountCharacterThreads(ctx, guildID, "char1")
		if count != 1 {
			t.Errorf("expected the last thread to survive, got %d", count)
		}
		if th, _ := cmdCtx.Session.GetThread(ctx, guildID, "char1", "1"); th == nil {
			t.Error("expected the last thread row to survive")
		}
		hist, _ := cmdCtx.Session.GetHistoryCount(ctx, guildID, "1")
		if hist != 0 {
			t.Errorf("expected the cleared thread history to be empty, got %d", hist)
		}
	})
}

func TestDeleteThreadCmd_UnknownThread(t *testing.T) {
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

	cmd := &deleteThreadCmd{session: cmdCtx.Session, lock: func(string, string) func() { return func() {} }}
	i := newThreadOptionInteraction("deletethread", stringOption("thread", "99"))
	if err := cmd.Execute(ctx, s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if content != "That thread doesn't exist for the active character." {
		t.Errorf("unexpected reply %q", content)
	}
}
