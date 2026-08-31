package commands

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"characterllm/internal/responses"
	"characterllm/internal/version"

	"github.com/bwmarrin/discordgo"
)

func TestStatusCommand_Online(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	expectedLatency := 100 * time.Millisecond
	var capturedEmbed *discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(response.Data.Embeds))
		}
		capturedEmbed = response.Data.Embeds[0]
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

	if capturedEmbed.Title != responses.Status.Title {
		t.Errorf("Expected embed title %q, got %q", responses.Status.Title, capturedEmbed.Title)
	}
	if capturedEmbed.Color != 0x57F287 {
		t.Errorf("Expected online embed color 0x57F287, got 0x%X", capturedEmbed.Color)
	}
	if len(capturedEmbed.Fields) != 3 {
		t.Fatalf("Expected 3 fields, got %d", len(capturedEmbed.Fields))
	}
	if capturedEmbed.Fields[0].Value != "✅ Online" {
		t.Errorf("Expected state %q, got %q", "✅ Online", capturedEmbed.Fields[0].Value)
	}
	expectedLatencyStr := fmt.Sprintf("%v", expectedLatency)
	if capturedEmbed.Fields[1].Value != expectedLatencyStr {
		t.Errorf("Expected latency %q, got %q", expectedLatencyStr, capturedEmbed.Fields[1].Value)
	}
	if capturedEmbed.Fields[2].Name != responses.Status.Version {
		t.Errorf("Expected version field name %q, got %q", responses.Status.Version, capturedEmbed.Fields[2].Name)
	}
	if capturedEmbed.Fields[2].Value != version.String() {
		t.Errorf("Expected version %q, got %q", version.String(), capturedEmbed.Fields[2].Value)
	}
}

func TestStatusCommand_Offline(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var capturedEmbed *discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(response.Data.Embeds))
		}
		capturedEmbed = response.Data.Embeds[0]
		return nil
	}

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

	if capturedEmbed.Color != 0xED4245 {
		t.Errorf("Expected offline embed color 0xED4245, got 0x%X", capturedEmbed.Color)
	}
	if len(capturedEmbed.Fields) != 3 {
		t.Fatalf("Expected 3 fields, got %d", len(capturedEmbed.Fields))
	}
	if capturedEmbed.Fields[0].Value != "❌ Offline" {
		t.Errorf("Expected state %q, got %q", "❌ Offline", capturedEmbed.Fields[0].Value)
	}
	if capturedEmbed.Fields[1].Value != "connection failed" {
		t.Errorf("Expected error %q, got %q", "connection failed", capturedEmbed.Fields[1].Value)
	}
	if capturedEmbed.Fields[2].Name != responses.Status.Version {
		t.Errorf("Expected version field name %q, got %q", responses.Status.Version, capturedEmbed.Fields[2].Name)
	}
	if capturedEmbed.Fields[2].Value != version.String() {
		t.Errorf("Expected version %q, got %q", version.String(), capturedEmbed.Fields[2].Value)
	}
}
