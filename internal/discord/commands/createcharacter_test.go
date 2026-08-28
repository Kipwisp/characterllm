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

func TestCreateCharacterCmd_NewCharacter_Success(t *testing.T) {
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
				Status:       research.AnalysisStatusOK,
				OfficialName: officialName,
				DisplayName:  displayName,
				Series:       "Series Name",
			}, "analysis reasoning", "raw response", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
			return &research.SynthesisResult{
				Status:       research.SynthesisStatusOK,
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
						Name:  "description",
						Value: userInput,
						Type:  discordgo.ApplicationCommandOptionString,
					},
				},
			},
		},
	}
	i.GuildID = guildID

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit}
	err := cmd.Execute(context.Background(), s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if capturedContent == "" {
		t.Error("Expected captured content in response, got empty string")
	}
}

func TestCreateCharacterCmd_AnalysisFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         research.AnalysisStatus
		ambiguities    []string
		expectedString string
	}{
		{"Unknown", research.AnalysisStatusUnknown, nil, "I couldn't find any reliable information"},
		{"Ambiguous", research.AnalysisStatusAmbiguous, []string{"Char A", "Char B"}, "I found multiple characters"},
		{"Injection", research.AnalysisStatusInjection, nil, "Nice try! I'm not falling for that prompt injection"},
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
								Name:  "description",
								Value: userInput,
								Type:  discordgo.ApplicationCommandOptionString,
							},
						},
					},
				},
			}
			i.GuildID = guildID

			cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit}
			err := cmd.Execute(context.Background(), s, i)

			if err == nil {
				t.Error("Expected error for analysis failure, got nil")
			}
			if !strings.Contains(capturedContent, tt.expectedString) {
				t.Errorf("Expected response to contain %q, got %q", tt.expectedString, capturedContent)
			}
		})
	}
}

func TestCreateCharacterCmd_SynthesisFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         research.SynthesisStatus
		ambiguities    []string
		expectedString string
	}{
		{"Unknown", research.SynthesisStatusUnknown, nil, "I couldn't find any reliable information"},
		{"Ambiguous", research.SynthesisStatusAmbiguous, []string{"Char A", "Char B"}, "I found multiple characters"},
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
						Status:       research.AnalysisStatusOK,
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
								Name:  "description",
								Value: userInput,
								Type:  discordgo.ApplicationCommandOptionString,
							},
						},
					},
				},
			}
			i.GuildID = guildID

			cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit}
			err := cmd.Execute(context.Background(), s, i)

			if err == nil {
				t.Error("Expected error for synthesis failure, got nil")
			}
			if !strings.Contains(capturedContent, tt.expectedString) {
				t.Errorf("Expected response to contain %q, got %q", tt.expectedString, capturedContent)
			}
		})
	}
}

func TestCreateCharacterCmd_ImageSearchFailures(t *testing.T) {
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
						Status:       research.AnalysisStatusOK,
						OfficialName: "Official",
						DisplayName:  "Display Name",
					}, "reasoning", "raw", nil
				},
				FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
					return &research.SynthesisResult{
						Status:      research.SynthesisStatusOK,
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
								Name:  "description",
								Value: userInput,
								Type:  discordgo.ApplicationCommandOptionString,
							},
						},
					},
				},
			}
			i.GuildID = guildID

			cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit}
			err := cmd.Execute(context.Background(), s, i)

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

			(&createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}).handleImageSelection(context.Background(), s, i)

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
	})
	sm.SetActiveCharacter(context.Background(), guildID, charID)
	sm.SetCharacterImage(context.Background(), guildID, charID, selectedURL)
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

	(&createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}).handleImageSelection(context.Background(), s, i)

	// Verify candidates are cleared
	candidates, _ := sm.GetImageCandidates(context.Background(), guildID)
	if len(candidates) != 0 {
		t.Errorf("Expected image candidates to be cleared, got %v", candidates)
	}
}

func runCreateCharacterWithImages(t *testing.T, cmdCtx *testDeps, s *mockDiscordSession, searchFn func(ctx context.Context, query string, limit int) ([]search.Image, error), composeFn func(ctx context.Context, urls []string, limit int) ([]byte, []string, error)) *discordgo.WebhookEdit {
	t.Helper()
	mockSynth := &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       research.AnalysisStatusOK,
				OfficialName: "Official",
				DisplayName:  "Display Name",
			}, "reasoning", "raw", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
			return &research.SynthesisResult{
				Status:      research.SynthesisStatusOK,
				PersonaSpec: "Persona",
			}, nil
		},
	}
	cmdCtx.Synthesizer = mockSynth
	cmdCtx.ImageClient = &mockImageClient{
		SearchImagesFn: searchFn,
		ComposeRowFn:   composeFn,
	}

	var edit *discordgo.WebhookEdit
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, e *discordgo.WebhookEdit) (*discordgo.Message, error) {
		edit = e
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
					{Name: "description", Value: "Character Name", Type: discordgo.ApplicationCommandOptionString},
				},
			},
		},
	}
	i.GuildID = "guild1"

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	return edit
}

func TestCreateCharacterCmd_AvatarOptionsRow(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	edit := runCreateCharacterWithImages(t, cmdCtx, s,
		func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return []search.Image{
				{URL: "http://a", Title: "A"},
				{URL: "http://b", Title: "B"},
				{URL: "http://c", Title: "C"},
			}, nil
		},
		func(ctx context.Context, urls []string, limit int) ([]byte, []string, error) {
			// Simulate the third candidate failing to fetch.
			return []byte("fake-png"), urls[:2], nil
		},
	)

	if edit == nil {
		t.Fatal("expected an interaction response edit")
	}

	if len(*edit.Embeds) != 1 {
		t.Fatalf("expected exactly one embed, got %d", len(*edit.Embeds))
	}
	if e := (*edit.Embeds)[0]; e.Image == nil || e.Image.URL != "attachment://avatar_options.png" {
		t.Errorf("expected embed to reference the row attachment, got %v", e.Image)
	}
	if len(edit.Files) != 1 || edit.Files[0].Name != "avatar_options.png" {
		t.Errorf("expected one file named avatar_options.png, got %v", edit.Files)
	}

	menu := findSelectMenu(t, *edit.Components)
	if len(menu.Options) != 2 || menu.Options[0].Value != "0" || menu.Options[1].Value != "1" {
		t.Errorf("unexpected select menu options: %v", menu.Options)
	}
	if menu.Options[0].Description != "A" || menu.Options[1].Description != "B" {
		t.Errorf("expected option descriptions from included urls, got %q, %q", menu.Options[0].Description, menu.Options[1].Description)
	}

	candidates, err := cmdCtx.Session.GetImageCandidates(context.Background(), "guild1")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0] != "http://a" || candidates[1] != "http://b" {
		t.Errorf("expected candidates to be the included urls, got %v", candidates)
	}
}

func TestCreateCharacterCmd_ComposeFailureFallsBackToPlainMessage(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	edit := runCreateCharacterWithImages(t, cmdCtx, s,
		func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return []search.Image{{URL: "http://a", Title: "A"}}, nil
		},
		func(ctx context.Context, urls []string, limit int) ([]byte, []string, error) {
			return nil, nil, fmt.Errorf("no images could be fetched")
		},
	)

	if edit == nil {
		t.Fatal("expected an interaction response edit")
	}
	if !strings.Contains(*edit.Content, "Character set to **Display Name**!") {
		t.Errorf("expected plain fallback message, got %q", *edit.Content)
	}
	if edit.Embeds != nil && len(*edit.Embeds) != 0 {
		t.Errorf("expected no embeds on fallback, got %v", *edit.Embeds)
	}
}

func findSelectMenu(t *testing.T, components []discordgo.MessageComponent) *discordgo.SelectMenu {
	t.Helper()
	for _, c := range components {
		row, ok := c.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, inner := range row.Components {
			if menu, ok := inner.(discordgo.SelectMenu); ok {
				return &menu
			}
		}
	}
	t.Fatal("no select menu found in components")
	return nil
}
