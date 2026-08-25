package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"characterllm/internal/research"
	"characterllm/internal/responses"
	"characterllm/internal/search"
	"characterllm/internal/session"
	"github.com/bwmarrin/discordgo"
)

func TestSetCharacterCmd_CachedAlias(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	charID := "char123"
	displayName := "Test Character"
	alias := "testalias"

	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: displayName,
	}, []string{alias})

	// Mock Image Client to avoid real search/cache
	mockImg := &mockImageClient{
		SearchImagesFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return []search.Image{{URL: "http://example.com/img.jpg", Title: "Img"}}, nil
		},
		SaveImageFn: func(ctx context.Context, guildID, characterID, url string) (string, error) {
			return "/tmp/img.jpg", nil
		},
		ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
			return "data:image/jpeg;base64,abc", nil
		},
	}
	cmdCtx.ImageClient = mockImg

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	nicknameUpdated := false
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error {
		if nickname == displayName {
			nicknameUpdated = true
		}
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{
						Name:  "prompt",
						Value: alias,
						Type:  discordgo.ApplicationCommandOptionString,
					},
				},
			},
		},
	}
	i.GuildID = guildID

	cmd := &setCharacterCmd{}
	err := cmd.Execute(context.Background(), cmdCtx, s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(capturedContent, "Creating") {
		t.Errorf("Expected response to contain 'Creating', got %q", capturedContent)
	}

	if !nicknameUpdated {
		t.Error("Expected bot nickname to be updated")
	}

	active, err := sm.GetCharacterDetails(context.Background(), guildID)
	if err != nil {
		t.Fatalf("GetCharacterDetails failed: %v", err)
	}
	if active == nil || active.CharacterID != charID {
		t.Errorf("Expected active character %s, got %v", charID, active)
	}
}

func TestSetCharacterCmd_NewCharacter_Success(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	userInput := "Character Name"
	officialName := "Official Character Name"
	displayName := "Display Name"
	personaSpec := "Persona specification text"

	// Mock Synthesizer
	mockSynth := &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       "OK",
				OfficialName: officialName,
				DisplayName:  displayName,
				Series:       "Series Name",
				Aliases:      []string{"alias1"},
			}, "analysis reasoning", "raw response", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
			return &research.SynthesisResult{
				Status:       "OK",
				PersonaSpec:  personaSpec,
				Reasoning:    "synthesis reasoning",
				ResearchData: "some data",
			}, nil
		},
	}
	cmdCtx.Synthesizer = mockSynth

	// Mock Image Client
	mockImg := &mockImageClient{
		SearchImagesFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return []search.Image{{URL: "http://example.com/img.jpg", Title: "Img"}}, nil
		},
		SaveImageFn: func(ctx context.Context, guildID, characterID, url string) (string, error) {
			return "/tmp/img.jpg", nil
		},
		ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
			return "data:image/jpeg;base64,abc", nil
		},
	}
	cmdCtx.ImageClient = mockImg

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		capturedContent = *edit.Content
		return nil, nil
	}
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error {
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{
						Name:  "prompt",
						Value: userInput,
						Type:  discordgo.ApplicationCommandOptionString,
					},
				},
			},
		},
	}
	i.GuildID = guildID

	cmd := &setCharacterCmd{}
	err := cmd.Execute(context.Background(), cmdCtx, s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if capturedContent == "" {
		t.Error("Expected captured content in response, got empty string")
	}
}

func TestSetCharacterCmd_AnalysisFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		ambiguities    []string
		expectedString string
	}{
		{"Unknown", "UNKNOWN", nil, "I couldn't find any reliable information"},
		{"Ambiguous", "AMBIGUOUS", []string{"Char A", "Char B"}, "I found multiple characters"},
		{"Injection", "INJECTION", nil, "Nice try! I'm not falling for that prompt injection"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdCtx, s, dbPath := setupCommandTest(t)
			defer os.Remove(dbPath)

			guildID := "guild1"
			userInput := "Some Input"

			mockSynth := &mockSynthesizer{
				AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
					return &research.AnalysisResult{
						Status:      tt.status,
						Ambiguities: tt.ambiguities,
					}, "reasoning", "raw", nil
				},
			}
			cmdCtx.Synthesizer = mockSynth

			var capturedContent string
			s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
				capturedContent = *edit.Content
				return nil, nil
			}

			i := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Type: discordgo.InteractionApplicationCommand,
					Data: discordgo.ApplicationCommandInteractionData{
						Options: []*discordgo.ApplicationCommandInteractionDataOption{
							{
								Name:  "prompt",
								Value: userInput,
								Type:  discordgo.ApplicationCommandOptionString,
							},
						},
					},
				},
			}
			i.GuildID = guildID

			cmd := &setCharacterCmd{}
			err := cmd.Execute(context.Background(), cmdCtx, s, i)

			if err == nil {
				t.Error("Expected error for analysis failure, got nil")
			}
			if !strings.Contains(capturedContent, tt.expectedString) {
				t.Errorf("Expected response to contain %q, got %q", tt.expectedString, capturedContent)
			}
		})
	}
}

func TestSetCharacterCmd_SynthesisFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		ambiguities    []string
		expectedString string
	}{
		{"Unknown", "UNKNOWN", nil, "I couldn't find any reliable information"},
		{"Ambiguous", "AMBIGUOUS", []string{"Char A", "Char B"}, "I found multiple characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdCtx, s, dbPath := setupCommandTest(t)
			defer os.Remove(dbPath)

			guildID := "guild1"
			userInput := "Some Input"

			mockSynth := &mockSynthesizer{
				AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
					return &research.AnalysisResult{
						Status:       "OK",
						OfficialName: "Official",
						DisplayName:  "Display",
					}, "reasoning", "raw", nil
				},
				FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
					return &research.SynthesisResult{
						Status:      tt.status,
						Ambiguities: tt.ambiguities,
					}, nil
				},
			}
			cmdCtx.Synthesizer = mockSynth

			var capturedContent string
			s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
				capturedContent = *edit.Content
				return nil, nil
			}

			i := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Type: discordgo.InteractionApplicationCommand,
					Data: discordgo.ApplicationCommandInteractionData{
						Options: []*discordgo.ApplicationCommandInteractionDataOption{
							{
								Name:  "prompt",
								Value: userInput,
								Type:  discordgo.ApplicationCommandOptionString,
							},
						},
					},
				},
			}
			i.GuildID = guildID

			cmd := &setCharacterCmd{}
			err := cmd.Execute(context.Background(), cmdCtx, s, i)

			if err == nil {
				t.Error("Expected error for synthesis failure, got nil")
			}
			if !strings.Contains(capturedContent, tt.expectedString) {
				t.Errorf("Expected response to contain %q, got %q", tt.expectedString, capturedContent)
			}
		})
	}
}

func TestSetCharacterCmd_ImageSearchFailures(t *testing.T) {
	tests := []struct {
		name           string
		searchFn       func(ctx context.Context, query string, limit int) ([]search.Image, error)
		expectedString string
	}{
		{
			name: "No Results",
			searchFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
				return []search.Image{}, nil
			},
			expectedString: "Character set to **Display Name**!",
		},
		{
			name: "Search Error",
			searchFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
				return nil, fmt.Errorf("search error")
			},
			expectedString: "Character set to **Display Name**!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdCtx, s, dbPath := setupCommandTest(t)
			defer os.Remove(dbPath)

			guildID := "guild1"
			userInput := "Character Name"

			mockSynth := &mockSynthesizer{
				AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
					return &research.AnalysisResult{
						Status:       "OK",
						OfficialName: "Official",
						DisplayName:  "Display Name",
					}, "reasoning", "raw", nil
				},
				FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
					return &research.SynthesisResult{
						Status:      "OK",
						PersonaSpec: "Persona",
					}, nil
				},
			}
			cmdCtx.Synthesizer = mockSynth

			mockImg := &mockImageClient{
				SearchImagesFn: tt.searchFn,
			}
			cmdCtx.ImageClient = mockImg

			var capturedContent string
			s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
				capturedContent = *edit.Content
				return nil, nil
			}
			s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error {
				return nil
			}

			i := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Type: discordgo.InteractionApplicationCommand,
					Data: discordgo.ApplicationCommandInteractionData{
						Options: []*discordgo.ApplicationCommandInteractionDataOption{
							{
								Name:  "prompt",
								Value: userInput,
								Type:  discordgo.ApplicationCommandOptionString,
							},
						},
					},
				},
			}
			i.GuildID = guildID

			cmd := &setCharacterCmd{}
			err := cmd.Execute(context.Background(), cmdCtx, s, i)

			if err != nil && tt.name == "No Results" {
				t.Errorf("Unexpected error for No Results: %v", err)
			}
			if !strings.Contains(capturedContent, tt.expectedString) {
				t.Errorf("Expected response to contain %q, got %q", tt.expectedString, capturedContent)
			}
		})
	}
}

func TestHandleSetCharacterImage_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		candidates     []string
		values         []string
		expectedString string
	}{
		{"Empty Candidates", []string{}, []string{"0"}, responses.SetCharacter.ImageExpired},
		{"Invalid Index", []string{"url1"}, []string{"1"}, responses.SetCharacter.ImageInvalid},
		{"Invalid Index Neg", []string{"url1"}, []string{"-1"}, responses.SetCharacter.ImageInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdCtx, s, dbPath := setupCommandTest(t)
			defer os.Remove(dbPath)

			guildID := "guild1"
			sm := cmdCtx.Session
			sm.SaveImageCandidates(context.Background(), guildID, tt.candidates)

			var capturedContent string
			s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
				capturedContent = response.Data.Content
				return nil
			}

			i := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Type: discordgo.InteractionMessageComponent,
					Data: discordgo.MessageComponentInteractionData{
						CustomID: "select_char_image",
						Values:   tt.values,
					},
				},
			}
			i.GuildID = guildID

			HandleSetCharacterImage(context.Background(), cmdCtx, s, i)

			if !strings.Contains(capturedContent, tt.expectedString) {
				t.Errorf("Expected response to contain %q, got %q", tt.expectedString, capturedContent)
			}
		})
	}
}

func TestHandleSetCharacterImage_Success(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	charID := "char123"
	selectedURL := "http://example.com/img.jpg"
	imagePath := "/tmp/img.jpg"
	base64Img := "data:image/jpeg;base64,abc"

	// Setup character and image candidates
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "Test Character",
	}, []string{})
	sm.SetActiveCharacter(context.Background(), guildID, charID)
	sm.SetCharacterImage(context.Background(), guildID, selectedURL)
	sm.SaveImageCandidates(context.Background(), guildID, []string{selectedURL})

	// Mock Image Client
	mockImg := &mockImageClient{
		SaveImageFn: func(ctx context.Context, guildID, characterID, url string) (string, error) {
			return imagePath, nil
		},
		ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
			return base64Img, nil
		},
	}
	cmdCtx.ImageClient = mockImg

	// Mock Session for Avatar Update
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		return nil
	}
	s.GetTokenFn = func() string {
		return "mock-token"
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionMessageComponent,
			GuildID: guildID,
			Message: &discordgo.Message{
				GuildID: guildID,
			},
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "select_char_image",
				Values:   []string{"0"},
			},
		},
	}

	HandleSetCharacterImage(context.Background(), cmdCtx, s, i)

	// Verify candidates are cleared
	candidates, _ := sm.GetImageCandidates(context.Background(), guildID)
	if len(candidates) != 0 {
		t.Errorf("Expected image candidates to be cleared, got %v", candidates)
	}
}
