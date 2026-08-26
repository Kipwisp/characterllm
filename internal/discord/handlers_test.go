package discord

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/prompts"
	"characterllm/internal/session"
	"github.com/bwmarrin/discordgo"
)

func setupHandlers(t *testing.T) (*Handlers, *mockLLMClient, *session.Manager, string) {
	tmpDb, err := os.CreateTemp("", "session_test*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpDbName := tmpDb.Name()
	tmpDb.Close()

	sm, err := session.NewManager(tmpDbName, "Default Prompt")
	if err != nil {
		t.Fatal(err)
	}

	tmpLogDir, err := os.MkdirTemp("", "audit_test*")
	if err != nil {
		t.Fatal(err)
	}

	llmMock := &mockLLMClient{}
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "gpt-4",
			MaxContext:          4096,
			CompactionThreshold: 0.8,
			RecentMemoryWindow:  10,
			SummaryMaxTokens:    1024,
			MaxImages:         2,
		},
		Images: config.ImageConfig{
			Provider:   "searxng",
			SearXNGURL: "http://localhost:8080",
		},
	}

	auditLogger := audit.NewAuditLogger(tmpLogDir)

	ps := &prompts.Set{
		System:     "[CHARACTER_DETAILS] is a helpful bot.[SUMMARY_CONTEXT]",
		Compaction: "Summarize the following: [LENGTH_LIMIT]",
	}

	h, err := NewHandlers(llmMock, sm, cfg, auditLogger, ps)
	if err != nil {
		t.Fatal(err)
	}

	return h, llmMock, sm, tmpDbName
}

func TestHandleMessageCreate_NoMention(t *testing.T) {
	h, _, _, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "Hello world",
			Author:  &discordgo.User{Bot: false},
		},
	}

	h.handleMessageCreate(s, m)
}

func TestHandleMessageCreate_BotAuthor(t *testing.T) {
	h, _, _, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	sentReply := false
	s := &mockDiscordSession{
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			sentReply = true
			return nil, nil
		},
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "Hello world",
			Author:  &discordgo.User{Bot: true},
		},
	}
	m.GuildID = "guild1"

	h.handleMessageCreate(s, m)

	if sentReply {
		t.Error("Should not have sent a reply to a bot")
	}
}

func TestHandleMessageCreate_NoCharacterSet(t *testing.T) {
	h, _, _, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	sentReply := false
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			sentReply = true
			return nil, nil
		},
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> Hi!",
			Author:  &discordgo.User{Bot: false},
		},
	}
	m.GuildID = "guild1"

	h.handleMessageCreate(s, m)

	if !sentReply {
		t.Error("Should have sent a reply indicating no character set")
	}
}

func TestHandleMessageCreate_ReplyToBot(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)

	sentResponse := ""
	s := &mockDiscordSession{
		GetUserIDFn: func() string { return "bot123" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			sentResponse = content
			return nil, nil
		},
	}

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "Hello reply!", "Reasoning", nil
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "Hi!",
			Author:  &discordgo.User{Bot: false},
			ReferencedMessage: &discordgo.Message{
				Author: &discordgo.User{ID: "bot123"},
			},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	if sentResponse != "Hello reply!" {
		t.Errorf("Expected 'Hello reply!', got %s", sentResponse)
	}
}

func TestHandleMessageCreate_VisionAttachmentForwarded(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)
	h.BotConfig.LLM.Vision = true

	called := false
	h.imageClient = &mockImageClient{
		ImageToDataURIFn: func(ctx context.Context, url string) (string, error) {
			called = true
			if url != "https://cdn.discordapp.com/att/1.png" {
				t.Errorf("unexpected URL: %s", url)
			}
			return "data:image/jpeg;base64,abc", nil
		},
	}

	var captured []llm.Message
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		captured = messages
		return "OK", "", nil
	}

	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			return nil, nil
		},
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> what is this?",
			Author:  &discordgo.User{Bot: false},
			Attachments: []*discordgo.MessageAttachment{
				{URL: "https://cdn.discordapp.com/att/1.png", ContentType: "image/png"},
				{URL: "https://cdn.discordapp.com/att/2.pdf", ContentType: "application/pdf"},
			},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	if !called {
		t.Fatal("expected the image client to be called")
	}
	last := captured[len(captured)-1]
	if len(last.Images) != 1 || last.Images[0] != "data:image/jpeg;base64,abc" {
		t.Errorf("expected the attachment data URI on the current message, got %v", last.Images)
	}
}

func TestHandleMessageCreate_ImageNotesStrippedAndPersisted(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)
	h.BotConfig.LLM.Vision = true

	h.imageClient = &mockImageClient{
		ImageToDataURIFn: func(ctx context.Context, url string) (string, error) {
			return "data:image/jpeg;base64,abc", nil
		},
	}

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "Nice shot! <image_note>a golden retriever lying on a beach</image_note>", "", nil
	}

	var sentContent string
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			sentContent = content
			return nil, nil
		},
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> what is this?",
			Author:  &discordgo.User{Bot: false},
			Attachments: []*discordgo.MessageAttachment{
				{URL: "https://cdn.discordapp.com/att/1.png", ContentType: "image/png"},
			},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	if sentContent != "Nice shot!" {
		t.Errorf("expected image note stripped from the sent message, got %q", sentContent)
	}

	history, err := sm.GetHistory(ctx, guildID, "", 10, 0)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	var userRow, assistantRow string
	for _, msg := range history {
		switch msg.Role {
		case "user":
			userRow = msg.Content
		case "assistant":
			assistantRow = msg.Content
		}
	}
	if assistantRow != "Nice shot!" {
		t.Errorf("expected assistant row without the note, got %q", assistantRow)
	}
	if !strings.Contains(userRow, "[Image: a golden retriever lying on a beach]") {
		t.Errorf("expected the image note attached to the user row, got %q", userRow)
	}
}

func TestHandleMessageCreate_MaxImagesCapsForwardedAttachments(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)
	h.BotConfig.LLM.Vision = true

	var fetched []string
	h.imageClient = &mockImageClient{
		ImageToDataURIFn: func(ctx context.Context, url string) (string, error) {
			fetched = append(fetched, url)
			return "data:image/jpeg;base64,abc", nil
		},
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "OK", "", nil
	}
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			return nil, nil
		},
	}

	t.Run("cap 2 of 3 attachments", func(t *testing.T) {
		h.BotConfig.LLM.MaxImages = 2
		fetched = nil

		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				Content: "<@123> pics",
				Author:  &discordgo.User{Bot: false},
				Attachments: []*discordgo.MessageAttachment{
					{URL: "https://cdn.discordapp.com/att/1.png", ContentType: "image/png"},
					{URL: "https://cdn.discordapp.com/att/2.png", ContentType: "image/png"},
					{URL: "https://cdn.discordapp.com/att/3.png", ContentType: "image/png"},
				},
			},
		}
		m.GuildID = guildID

		h.handleMessageCreate(s, m)

		if len(fetched) != 2 || fetched[0] != "https://cdn.discordapp.com/att/1.png" || fetched[1] != "https://cdn.discordapp.com/att/2.png" {
			t.Errorf("expected the first two attachments forwarded, got %v", fetched)
		}
	})

	t.Run("cap 0 forwards nothing", func(t *testing.T) {
		h.BotConfig.LLM.MaxImages = 0
		fetched = nil

		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				Content: "<@123> pics",
				Author:  &discordgo.User{Bot: false},
				Attachments: []*discordgo.MessageAttachment{
					{URL: "https://cdn.discordapp.com/att/1.png", ContentType: "image/png"},
				},
			},
		}
		m.GuildID = guildID

		h.handleMessageCreate(s, m)

		if len(fetched) != 0 {
			t.Errorf("expected no attachments forwarded, got %v", fetched)
		}
	})
}

func TestHandleMessageCreate_VisionDisabledIgnoresAttachments(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)
	// Vision stays false (the setupHandlers default).

	called := false
	h.imageClient = &mockImageClient{
		ImageToDataURIFn: func(ctx context.Context, url string) (string, error) {
			called = true
			return "data:image/jpeg;base64,abc", nil
		},
	}

	var captured []llm.Message
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		captured = messages
		return "OK", "", nil
	}

	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			return nil, nil
		},
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> what is this?",
			Author:  &discordgo.User{Bot: false},
			Attachments: []*discordgo.MessageAttachment{
				{URL: "https://cdn.discordapp.com/att/1.png", ContentType: "image/png"},
			},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	if called {
		t.Error("image client must not be called when vision is disabled")
	}
	last := captured[len(captured)-1]
	if len(last.Images) != 0 {
		t.Errorf("expected no images forwarded, got %v", last.Images)
	}
}

func TestHandleMessageCreate_MemberNickname(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)

	var capturedMessages []llm.Message
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			return nil, nil
		},
	}

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		capturedMessages = messages
		return "OK", "Reasoning", nil
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> Hello!",
			Author:  &discordgo.User{Username: "user1"},
			Member:  &discordgo.Member{Nick: "CoolNick"},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	found := false
	for _, msg := range capturedMessages {
		if msg.Content == "CoolNick: Hello!" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected message 'CoolNick: Hello!' in messages, got %v", capturedMessages)
	}
}

func TestHandleMessageCreate_Success(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)

	sentResponse := ""
	var capturedMessages []llm.Message
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		GetUserIDFn:      func() string { return "bot123" },
		ChannelTypingFn:  func(channelID string) error { return nil },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			sentResponse = content
			return nil, nil
		},
	}

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		capturedMessages = messages
		return "Hello from LLM!", "Reasoning", nil
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> Hello!",
			Author:  &discordgo.User{Bot: false},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	if sentResponse != "Hello from LLM!" {
		t.Errorf("Expected 'Hello from LLM!', got %s", sentResponse)
	}

	// The prompt must be exactly [system, current]: the saved current message
	// must not also appear in the retrieved history.
	if len(capturedMessages) != 2 {
		t.Fatalf("Expected 2 prompt messages (system + current), got %d", len(capturedMessages))
	}
	last := capturedMessages[len(capturedMessages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Hello!") {
		t.Errorf("Expected prompt to end with current user message, got %v", last)
	}
}

func TestHandleMessageCreate_SystemPromptSubstitution(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)

	var capturedMessages []llm.Message
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			return nil, nil
		},
	}

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		capturedMessages = messages
		return "OK", "Reasoning", nil
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> Hello!",
			Author:  &discordgo.User{Bot: false},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	found := false
	for _, msg := range capturedMessages {
		if msg.Role == "system" && msg.Content == "A test character is a helpful bot." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected substituted system prompt, got %v", capturedMessages)
	}
}

func TestHandleMessageCreate_HistoryError(t *testing.T) {
	h, _, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	os.Remove(dbPath)

	sentError := false
	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			sentError = true
			return nil, nil
		},
	}

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> Hello!",
			Author:  &discordgo.User{Bot: false},
		},
	}
	m.GuildID = guildID

	h.handleMessageCreate(s, m)

	if !sentError {
		t.Error("Should have sent an error message when history retrieval failed")
	}
}

func TestProcessChat_Error(t *testing.T) {
	h, llmMock, _, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	sentError := false
	s := &mockDiscordSession{
		ChannelMessageSendFn: func(channelID string, content string) (*discordgo.Message, error) {
			sentError = true
			return nil, nil
		},
	}

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "", "", fmt.Errorf("llm error")
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "Hi",
			Author:  &discordgo.User{Bot: false},
		},
	}
	m.GuildID = "guild1"

	err := h.processChat(context.Background(), s, m, "char1", "prompt", []llm.Message{}, "req1")

	if err == nil {
		t.Error("Expected error, got nil")
	}
	if !sentError {
		t.Error("Expected error message to be sent to Discord")
	}
}

// TestCompaction_TriggeredThroughHandler pins the end-to-end wiring: a prompt
// that exceeds the soft target must result in the handler invoking compaction
// after the reply. The compaction and prompt-assembly logic itself is covered
// by the tests in internal/conversation.
func TestCompaction_TriggeredThroughHandler(t *testing.T) {
	h, llmMock, sm, dbPath := setupHandlers(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	charID := "char1"
	sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{})
	sm.SetActiveCharacter(ctx, guildID, charID)

	h.BotConfig.LLM.CompactionThreshold = 0.1
	h.BotConfig.LLM.MaxContext = 1000
	h.BotConfig.LLM.RecentMemoryWindow = 2
	h.BotConfig.LLM.SummaryMaxTokens = 20

	llmMock.EstimateTokensFn = func(ctx context.Context, messages []llm.Message) int {
		return len(messages) * 20
	}

	for i := 0; i < 10; i++ {
		sm.SaveMessage(ctx, guildID, "", "user", "Msg")
	}

	compactionCalls := 0
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		if len(messages) > 0 && strings.HasPrefix(messages[0].Content, "Summarize the following:") {
			compactionCalls++
			return "SUMMARY_TEXT", "Reasoning", nil
		}
		return "Response", "Reasoning", nil
	}

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@123> current",
			Author:  &discordgo.User{Bot: false},
		},
	}
	m.GuildID = guildID

	s := &mockDiscordSession{
		GetUserMentionFn: func() string { return "<@123>" },
		ChannelMessageSendReplyFn: func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
			return nil, nil
		},
	}

	h.handleMessageCreate(s, m)

	if compactionCalls != 1 {
		t.Errorf("Expected exactly 1 compaction generation call, got %d", compactionCalls)
	}
	summary, err := sm.GetSummary(ctx, guildID, "")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary != "SUMMARY_TEXT" {
		t.Errorf("Expected summary to be stored after handler-triggered compaction, got %q", summary)
	}
}
