package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"characterllm/internal/audit"
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
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
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

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
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

			cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
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
				FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
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

			cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
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
				FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
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

			cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
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
			sm.SaveImageCandidates(context.Background(), "tok1", tt.candidates)

			var capturedContent string
			s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
				capturedContent = response.Data.Content
				return nil
			}

			i := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Type: discordgo.InteractionMessageComponent,
					Data: discordgo.MessageComponentInteractionData{
						CustomID: setCharacterImagePrefix + "tok1",
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
	sm.SaveImageCandidates(context.Background(), "tok2", []string{selectedURL})

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
				CustomID: setCharacterImagePrefix + "tok2",
				Values:   []string{"0"},
			},
		},
	}

	(&createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}).handleImageSelection(context.Background(), s, i)

	// Verify candidates are cleared
	candidates, _ := sm.GetImageCandidates(context.Background(), "tok2")
	if len(candidates) != 0 {
		t.Errorf("Expected image candidates to be cleared, got %v", candidates)
	}
}

func runCreateCharacterWithImages(t *testing.T, cmdCtx *testDeps, s *mockDiscordSession, searchFn func(ctx context.Context, query string, limit int) ([]search.Image, error), fetchFn func(ctx context.Context, urls []string, limit int) ([]string, []string, []byte, error)) *discordgo.WebhookEdit {
	t.Helper()
	mockSynth := &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       research.AnalysisStatusOK,
				OfficialName: "Official",
				DisplayName:  "Display Name",
			}, "reasoning", "raw", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
			return &research.SynthesisResult{
				Status:      research.SynthesisStatusOK,
				PersonaSpec: "Persona",
			}, nil
		},
	}
	cmdCtx.Synthesizer = mockSynth
	cmdCtx.ImageClient = &mockImageClient{
		SearchImagesFn:    searchFn,
		FetchCandidatesFn: fetchFn,
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

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
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
		func(ctx context.Context, urls []string, limit int) ([]string, []string, []byte, error) {
			// Simulate the third candidate failing to fetch.
			return []string{"data:image/png;base64,a", "data:image/png;base64,b"}, urls[:2], []byte("fake-png"), nil
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

	menuToken := strings.TrimPrefix(menu.CustomID, setCharacterImagePrefix)
	candidates, err := cmdCtx.Session.GetImageCandidates(context.Background(), menuToken)
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
		func(ctx context.Context, urls []string, limit int) ([]string, []string, []byte, error) {
			return nil, nil, nil, fmt.Errorf("no images could be fetched")
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

// runCreateCharacterWithModelPick runs /createcharacter with the model-pick
// config flags and a synthesis that returns the given AvatarChoice.
func runCreateCharacterWithModelPick(t *testing.T, cmdCtx *testDeps, s *mockDiscordSession, modelPick, vision bool, avatarChoice int) (*discordgo.WebhookEdit, []string) {
	t.Helper()
	cmdCtx.Config.LLM.AvatarPick = modelPick
	cmdCtx.Config.LLM.Vision = vision

	var gotImages []string
	cmdCtx.Synthesizer = &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       research.AnalysisStatusOK,
				OfficialName: "Official",
				DisplayName:  "Display Name",
			}, "reasoning", "raw", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
			gotImages = avatarDataURIs
			raw := "Persona"
			if avatarChoice > 0 {
				raw = fmt.Sprintf("AVATAR: %d\n%s", avatarChoice, "Persona")
			}
			return &research.SynthesisResult{
				Status:          research.SynthesisStatusOK,
				PersonaSpec:     "Persona",
				AvatarChoice:    avatarChoice,
				RawResponse:     raw,
				SelectPrompt:    "Source select prompt marker",
				SelectResponse:  "PICK: 1\nAVATARS: 1,2",
				SelectLatency:   time.Second,
				SynthesisPrompt: "Rendered synthesis prompt marker",
			}, nil
		},
	}
	cmdCtx.ImageClient = &mockImageClient{
		SearchImagesFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return []search.Image{{URL: "http://a", Title: "A"}, {URL: "http://b", Title: "B"}}, nil
		},
		FetchCandidatesFn: func(ctx context.Context, urls []string, limit int) ([]string, []string, []byte, error) {
			return []string{"data:image/png;base64,a", "data:image/png;base64,b"}, urls, []byte("fake-png"), nil
		},
		SaveImageFn: func(ctx context.Context, guildID, characterID, url string) (string, error) {
			return "/tmp/img.jpg", nil
		},
		ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
			return "data:image/jpeg;base64,abc", nil
		},
	}

	var edit *discordgo.WebhookEdit
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, e *discordgo.WebhookEdit) (*discordgo.Message, error) {
		edit = e
		return nil, nil
	}
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error { return nil }

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

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	return edit, gotImages
}

func TestCreateCharacterCmd_ModelPickApplied(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	auditDir := t.TempDir()
	cmdCtx.Audit = audit.NewAuditLogger(auditDir, true)

	var capturedAvatar string
	s.UpdateGuildAvatarFn = func(guildID, dataURI string) error {
		capturedAvatar = dataURI
		return nil
	}

	edit, gotImages := runCreateCharacterWithModelPick(t, cmdCtx, s, true, true, 2)

	if len(gotImages) != 2 || !strings.HasPrefix(gotImages[0], "data:image/png;base64,") {
		t.Errorf("expected the individual candidate data URIs on the synthesis call, got %v", gotImages)
	}
	if capturedAvatar != "data:image/jpeg;base64,abc" {
		t.Errorf("expected the guild avatar to be updated with the cached image, got %q", capturedAvatar)
	}
	if edit == nil || !strings.Contains(*edit.Content, "Character set to **Display Name**!") {
		t.Fatalf("expected the final character-set message, got %+v", edit)
	}
	if strings.Contains(*edit.Content, "Please select a profile picture") {
		t.Errorf("expected no manual selection prompt when the model pick is applied, got %q", *edit.Content)
	}
	if edit.Components != nil && len(*edit.Components) != 0 {
		t.Errorf("expected no select menu when the model pick is applied, got %v", *edit.Components)
	}

	details, err := cmdCtx.Session.GetCharacterDetails(context.Background(), "guild1")
	if err != nil || details == nil {
		t.Fatalf("expected active character details: %v", err)
	}
	if details.ImageURL != "http://b" {
		t.Errorf("expected image_url to be the picked candidate, got %q", details.ImageURL)
	}

	if !auditDirContains(t, auditDir, "AVATAR: 2") {
		t.Error("expected the synthesis audit entry to log the model's AVATAR line")
	}
	if !auditDirContains(t, auditDir, "source_select") {
		t.Error("expected a source_select audit entry")
	}
	if !auditDirContains(t, auditDir, "Source select prompt marker") {
		t.Error("expected the source_select audit entry to carry the rendered prompt")
	}
	if !auditDirContains(t, auditDir, "Rendered synthesis prompt marker") {
		t.Error("expected the synthesis audit entry to carry the verbatim rendered prompt")
	}
}

// auditDirContains reports whether any audit file in dir contains s.
func auditDirContains(t *testing.T, dir, s string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range entries {
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(s)) {
			return true
		}
	}
	return false
}

func TestCreateCharacterCmd_ModelPickRespectsPreFilter(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.Config.LLM.AvatarPick = true
	cmdCtx.Config.LLM.Vision = true

	cmdCtx.Synthesizer = &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       research.AnalysisStatusOK,
				OfficialName: "Official",
				DisplayName:  "Display Name",
			}, "reasoning", "raw", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
			// The pre-filter kept only candidate 2; the pick is 1 within the
			// kept list, i.e. the second original candidate.
			return &research.SynthesisResult{
				Status:       research.SynthesisStatusOK,
				PersonaSpec:  "Persona",
				AvatarKept:   []int{2},
				AvatarChoice: 1,
			}, nil
		},
	}
	cmdCtx.ImageClient = &mockImageClient{
		SearchImagesFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return []search.Image{{URL: "http://a", Title: "A"}, {URL: "http://b", Title: "B"}}, nil
		},
		FetchCandidatesFn: func(ctx context.Context, urls []string, limit int) ([]string, []string, []byte, error) {
			return []string{"data:image/png;base64,a", "data:image/png;base64,b"}, urls, []byte("fake-png"), nil
		},
		SaveImageFn: func(ctx context.Context, guildID, characterID, url string) (string, error) {
			return "/tmp/img.jpg", nil
		},
		ImageToBase64Fn: func(ctx context.Context, path string) (string, error) {
			return "data:image/jpeg;base64,abc", nil
		},
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

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	details, err := cmdCtx.Session.GetCharacterDetails(context.Background(), "guild1")
	if err != nil || details == nil {
		t.Fatalf("expected active character details: %v", err)
	}
	if details.ImageURL != "http://b" {
		t.Errorf("expected the pick to index the pre-filtered list (http://b), got %q", details.ImageURL)
	}
}

func TestCreateCharacterCmd_ModelPickNoChoiceFallsBackToMenu(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var avatarCalls int
	s.UpdateGuildAvatarFn = func(guildID, dataURI string) error {
		avatarCalls++
		return nil
	}

	edit, gotImages := runCreateCharacterWithModelPick(t, cmdCtx, s, true, true, 0)

	if len(gotImages) != 2 {
		t.Errorf("expected the candidate images on the synthesis call, got %v", gotImages)
	}
	if avatarCalls != 0 {
		t.Error("expected no guild avatar update when the model makes no pick")
	}
	if edit == nil || !strings.Contains(*edit.Content, responses.CreateCharacter.PickFailed) {
		t.Fatalf("expected the pick-failure note, got %+v", edit)
	}
	if !strings.Contains(*edit.Content, responses.CreateCharacter.SelectPicture) {
		t.Errorf("expected the manual selection prompt, got %q", *edit.Content)
	}
	menu := findSelectMenu(t, *edit.Components)
	menuToken := strings.TrimPrefix(menu.CustomID, setCharacterImagePrefix)
	candidates, err := cmdCtx.Session.GetImageCandidates(context.Background(), menuToken)
	if err != nil || len(candidates) != 2 {
		t.Errorf("expected candidates saved for the manual menu, got %v (%v)", candidates, err)
	}
}

func TestCreateCharacterCmd_ModelPickOutOfRangeFallsBackToMenu(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var avatarCalls int
	s.UpdateGuildAvatarFn = func(guildID, dataURI string) error {
		avatarCalls++
		return nil
	}

	edit, _ := runCreateCharacterWithModelPick(t, cmdCtx, s, true, true, 5)

	if avatarCalls != 0 {
		t.Error("expected no guild avatar update for an out-of-range pick")
	}
	if edit == nil || !strings.Contains(*edit.Content, responses.CreateCharacter.PickFailed) {
		t.Fatalf("expected the pick-failure note, got %+v", edit)
	}
	findSelectMenu(t, *edit.Components)
}

func TestCreateCharacterCmd_ModelPickDisabledSendsNoImages(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var avatarCalls int
	s.UpdateGuildAvatarFn = func(guildID, dataURI string) error {
		avatarCalls++
		return nil
	}

	// The synthesizer may still report a pick, but without images on the
	// call it must be ignored.
	edit, gotImages := runCreateCharacterWithModelPick(t, cmdCtx, s, false, true, 2)

	if len(gotImages) != 0 {
		t.Errorf("expected no images on the synthesis call when model pick is disabled, got %v", gotImages)
	}
	if avatarCalls != 0 {
		t.Error("expected no guild avatar update when model pick is disabled")
	}
	if edit == nil || !strings.Contains(*edit.Content, responses.CreateCharacter.SelectPicture) {
		t.Fatalf("expected the manual selection prompt, got %+v", edit)
	}
	if strings.Contains(*edit.Content, responses.CreateCharacter.PickFailed) {
		t.Errorf("expected no pick-failure note when no pick was attempted, got %q", *edit.Content)
	}
}

func TestCreateCharacterCmd_VisionDisabledFallsBackToMenu(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var avatarCalls int
	s.UpdateGuildAvatarFn = func(guildID, dataURI string) error {
		avatarCalls++
		return nil
	}

	edit, gotImages := runCreateCharacterWithModelPick(t, cmdCtx, s, true, false, 2)

	if len(gotImages) != 0 {
		t.Errorf("expected no images on the synthesis call when vision is disabled, got %v", gotImages)
	}
	if avatarCalls != 0 {
		t.Error("expected no guild avatar update when vision is disabled")
	}
	if edit == nil || !strings.Contains(*edit.Content, responses.CreateCharacter.SelectPicture) {
		t.Fatalf("expected the manual selection prompt, got %+v", edit)
	}
	if strings.Contains(*edit.Content, responses.CreateCharacter.PickFailed) {
		t.Errorf("expected no pick-failure note when no pick was attempted, got %q", *edit.Content)
	}
}

func TestCreateCharacterCmd_UsesGreetingAsFirstMessage(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	personaSpec := "### Identity & Temperament\nBody.\n\n### Greeting\nHey, I'm the character."

	mockSynth := &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       research.AnalysisStatusOK,
				OfficialName: "Official",
				DisplayName:  "Display Name",
			}, "reasoning", "raw", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
			return &research.SynthesisResult{Status: research.SynthesisStatusOK, PersonaSpec: personaSpec}, nil
		},
	}
	cmdCtx.Synthesizer = mockSynth
	// No image candidates: the flow takes the plain final-message path.
	cmdCtx.ImageClient = &mockImageClient{
		SearchImagesFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			return nil, nil
		},
	}

	var capturedContent string
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		capturedContent = *edit.Content
		return nil, nil
	}
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error { return nil }

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
	i.GuildID = guildID

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if capturedContent != "Hey, I'm the character." {
		t.Errorf("expected the greeting as the first message, got %q", capturedContent)
	}
}

func TestCreateCharacterCmd_ImageSearchQueryIncludesSeries(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var gotQuery string
	cmdCtx.Synthesizer = &mockSynthesizer{
		AnalyzeInputFn: func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
			return &research.AnalysisResult{
				Status:       research.AnalysisStatusOK,
				OfficialName: "Barrett",
				DisplayName:  "Barrett",
				Series:       "Some Show",
			}, "reasoning", "raw", nil
		},
		FetchCharacterFn: func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
			return &research.SynthesisResult{Status: research.SynthesisStatusOK, PersonaSpec: "Persona"}, nil
		},
	}
	cmdCtx.ImageClient = &mockImageClient{
		SearchImagesFn: func(ctx context.Context, query string, limit int) ([]search.Image, error) {
			gotQuery = query
			return nil, nil
		},
	}
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error { return nil }

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "description", Value: "Barrett", Type: discordgo.ApplicationCommandOptionString},
				},
			},
		},
	}
	i.GuildID = "guild1"

	cmd := &createCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: cmdCtx.Synthesizer, audit: cmdCtx.Audit, config: cmdCtx.Config}
	if err := cmd.Execute(context.Background(), s, i); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if gotQuery != "Barrett (Some Show) profile picture" {
		t.Errorf("expected series in the image search query, got %q", gotQuery)
	}
}
