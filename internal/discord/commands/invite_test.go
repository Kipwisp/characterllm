package commands

import (
	"context"
	"fmt"
	"testing"

	"characterllm/internal/responses"

	"github.com/bwmarrin/discordgo"
)

func TestInviteCommand_WithClientID(t *testing.T) {
	var capturedContent string
	s := &mockDiscordSession{}
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	clientID := "698656958244192276"
	cmd := &inviteCmd{clientID: clientID}
	err := cmd.Execute(context.Background(), s, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := fmt.Sprintf(responses.Invite.Link,
		"https://discord.com/oauth2/authorize?client_id="+clientID+"&permissions=274945133568&integration_type=0&scope=bot")
	if capturedContent != expected {
		t.Errorf("Expected response %q, got %q", expected, capturedContent)
	}
}
