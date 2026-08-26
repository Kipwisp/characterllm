package commands

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"characterllm/internal/responses"
	"github.com/bwmarrin/discordgo"
)

func TestStatusCommand_Online(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	expectedLatency := 100 * time.Millisecond
	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmdCtx.LLM.(*mockLLMClient).PingFn = func(ctx context.Context) (time.Duration, error) {
		return expectedLatency, nil
	}

	cmd := &statusCmd{llm: cmdCtx.LLM}
	err := cmd.Execute(context.Background(), s, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expectedResponse := fmt.Sprintf(responses.Status.Online, expectedLatency)
	if capturedContent != expectedResponse {
		t.Errorf("Expected response %q, got %q", expectedResponse, capturedContent)
	}

}

func TestStatusCommand_Offline(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.LLM.(*mockLLMClient).PingFn = func(ctx context.Context) (time.Duration, error) {
		return 0, fmt.Errorf("connection failed")
	}

	cmd := &statusCmd{llm: cmdCtx.LLM}
	err := cmd.Execute(context.Background(), s, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	})

	if err == nil {
		t.Error("Expected error for offline LLM, got nil")
	}
}
