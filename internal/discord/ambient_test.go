package discord

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/conversation"
	"characterllm/internal/llm"
	"characterllm/internal/session"
	"characterllm/internal/testkit"

	"github.com/bwmarrin/discordgo"
)

// rollQueue replays deterministic values from Roll, holding the last one.
type rollQueue struct {
	values []float64
	i      int
}

func (r *rollQueue) next() float64 {
	v := r.values[r.i]
	if r.i+1 < len(r.values) {
		r.i++
	}
	return v
}

func setupAmbient(t *testing.T, cfg config.AmbientConfig) (*Ambient, *mockDiscordSession, *mockLLMClient, *session.Manager, string, *rollQueue) {
	t.Helper()
	env := testkit.NewEnv(t)
	env.Config.Ambient = cfg
	auditDir := t.TempDir()
	auditLogger := audit.NewAuditLogger(auditDir, true)

	s := &mockDiscordSession{
		GetUserIDFn: func() string { return "bot1" },
	}
	chat := &Chat{
		LLM:           env.LLM,
		Session:       env.Session,
		Config:        env.Config,
		Audit:         auditLogger,
		ImageClient:   &mockImageClient{},
		PromptBuilder: conversation.NewPromptBuilder(env.LLM, env.Session, env.Config, env.Prompts),
		Compactor:     conversation.NewCompactor(env.LLM, env.Session, env.Config, auditLogger, env.Prompts),
		Locks:         NewConversationLocks(),
	}
	rolls := &rollQueue{}
	a := &Ambient{
		Session:     env.Session,
		Chat:        chat,
		Config:      env.Config,
		ImageClient: chat.ImageClient,
		Discord:     s,
		Roll:        rolls.next,
	}
	return a, s, env.LLM, env.Session, auditDir, rolls
}

func setActiveCharacter(t *testing.T, sm *session.Manager, guildID string) {
	t.Helper()
	ctx := context.Background()
	charID := "char1"
	if err := sm.SaveCharacterCard(ctx, guildID, &session.CharacterCard{CharacterID: charID, DisplayName: "Test", Description: "A test character"}); err != nil {
		t.Fatalf("SaveCharacterCard failed: %v", err)
	}
	if err := sm.SetActiveCharacter(ctx, guildID, charID); err != nil {
		t.Fatalf("SetActiveCharacter failed: %v", err)
	}
}

func TestAmbientTick_TopicMode(t *testing.T) {
	a, s, llmMock, sm, auditDir, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.1} // topic mode

	var sentChannel, sentContent string
	var replyCalled bool
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		sentChannel, sentContent = channelID, content
		return nil, nil
	}
	s.ChannelMessageSendReplyFn = func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
		replyCalled = true
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "A fresh thought.", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	if replyCalled {
		t.Error("expected a plain channel send, got a reply")
	}
	if sentChannel != "chan1" || sentContent != "A fresh thought." {
		t.Errorf("unexpected send: channel=%q content=%q", sentChannel, sentContent)
	}

	ctx := context.Background()
	history, err := sm.GetHistory(ctx, guildID, "1", 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d (err %v)", len(history), err)
	}
	if history[0].Role != "user" || history[0].Content != ambientTopicCue {
		t.Errorf("unexpected user row: %+v", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "A fresh thought." {
		t.Errorf("unexpected assistant row: %+v", history[1])
	}

	data, err := os.ReadFile(auditDir + "/guild1_char1_1.log")
	if err != nil {
		t.Fatalf("audit file missing: %v", err)
	}
	if !strings.Contains(string(data), "kind=ambient") {
		t.Errorf("audit file should record kind=ambient: %s", data)
	}
}

func TestAmbientTick_ReplyMode(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		if channelID != "chan1" || limit != 5 {
			t.Errorf("unexpected ChannelMessages args: %q limit=%d", channelID, limit)
		}
		// The API returns messages newest-first.
		return []*discordgo.Message{
			{Content: "barely", Author: &discordgo.User{ID: "b1", Username: "Bob"}},
			{Content: "did anyone finish the race?", Author: &discordgo.User{ID: "a1", Username: "Alice"}},
			{Content: "bot chatter", Author: &discordgo.User{ID: "bot1", Username: "bot"}},
		}, nil
	}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "I got a DNF.", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	ctx := context.Background()
	history, err := sm.GetHistory(ctx, guildID, "1", 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d (err %v)", len(history), err)
	}
	want := "Messages in this channel just now:\nAlice: did anyone finish the race?\nBob: barely\nReply to this conversation in character."
	if history[0].Role != "user" || history[0].Content != want {
		t.Errorf("unexpected user row:\n%s\nwant:\n%s", history[0].Content, want)
	}
}

func TestAmbientTick_EmptyTranscriptFallsBackToTopic(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		return []*discordgo.Message{
			{Content: "bot chatter", Author: &discordgo.User{ID: "bot1", Username: "bot"}},
		}, nil
	}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "hi", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	ctx := context.Background()
	history, err := sm.GetHistory(ctx, guildID, "1", 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d (err %v)", len(history), err)
	}
	if history[0].Content != ambientTopicCue {
		t.Errorf("expected topic cue fallback, got %q", history[0].Content)
	}
}

func TestAmbientTick_TranscriptExcludesAddressedMessages(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		// The API returns messages newest-first.
		return []*discordgo.Message{
			{
				ID:               "m4",
				Content:          "right?",
				Author:           &discordgo.User{ID: "c1", Username: "Cara"},
				MessageReference: &discordgo.MessageReference{MessageID: "m1"},
			},
			{
				ID:      "m3",
				Content: "what's up?",
				Author:  &discordgo.User{ID: "b1", Username: "Bob"},
				Mentions: []*discordgo.User{
					{ID: "bot1", Username: "bot"},
					{ID: "b1", Username: "Bob"},
				},
			},
			{ID: "m2", Content: "the weather is nice", Author: &discordgo.User{ID: "a1", Username: "Alice"}},
			{ID: "m1", Content: "an earlier bot line", Author: &discordgo.User{ID: "bot1", Username: "bot"}},
		}, nil
	}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "nice", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	ctx := context.Background()
	history, err := sm.GetHistory(ctx, guildID, "1", 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d (err %v)", len(history), err)
	}
	want := "Messages in this channel just now:\nAlice: the weather is nice\nReply to this conversation in character."
	if history[0].Content != want {
		t.Errorf("unexpected user row:\n%s\nwant:\n%s", history[0].Content, want)
	}
}

func TestAmbientTick_AllAddressedTranscriptFallsBackToTopic(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		return []*discordgo.Message{
			{
				ID:      "m1",
				Content: "what's up?",
				Author:  &discordgo.User{ID: "b1", Username: "Bob"},
				Mentions: []*discordgo.User{
					{ID: "bot1", Username: "bot"},
				},
			},
		}, nil
	}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "hi", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	ctx := context.Background()
	history, err := sm.GetHistory(ctx, guildID, "1", 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d (err %v)", len(history), err)
	}
	if history[0].Content != ambientTopicCue {
		t.Errorf("expected topic cue fallback, got %q", history[0].Content)
	}
}

func TestAmbientTick_TranscriptFetchFailure(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		return nil, fmt.Errorf("api down")
	}
	llmCalls := 0
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		llmCalls++
		return "hi", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	if llmCalls != 0 {
		t.Error("expected no LLM call when the transcript fetch fails")
	}
}

func TestAmbientTick_NoActiveCharacter(t *testing.T) {
	a, s, llmMock, _, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	rolls.values = []float64{0.1}

	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		t.Error("unexpected send")
		return nil, nil
	}
	llmCalls := 0
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		llmCalls++
		return "hi", "", nil
	}

	a.tick(context.Background(), "guild-nobody", []string{"chan1"})

	if llmCalls != 0 {
		t.Error("expected no LLM call without an active character")
	}
}

func TestAmbientTick_TranscriptImages(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	a.Config.LLM.Vision = true
	a.Config.LLM.MaxImages = 2
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		// The API returns messages newest-first.
		return []*discordgo.Message{
			{
				Content: "one more",
				Author:  &discordgo.User{ID: "c1", Username: "Cara"},
				Attachments: []*discordgo.MessageAttachment{
					{URL: "https://x/3.png", ContentType: "image/png"},
				},
			},
			{
				Content: "and this",
				Author:  &discordgo.User{ID: "b1", Username: "Bob"},
				Attachments: []*discordgo.MessageAttachment{
					{URL: "https://x/2.jpg", ContentType: "image/jpeg"},
					{URL: "https://x/notes.txt", ContentType: "text/plain"},
				},
			},
			{
				Content: "look at this",
				Author:  &discordgo.User{ID: "a1", Username: "Alice"},
				Attachments: []*discordgo.MessageAttachment{
					{URL: "https://x/1.png", ContentType: "image/png"},
				},
			},
		}, nil
	}
	a.ImageClient = &mockImageClient{
		ImageToDataURIFn: func(ctx context.Context, url string) (string, error) {
			return "data:" + url, nil
		},
	}
	var lastPrompt []llm.Message
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		lastPrompt = messages
		return "nice photos", "", nil
	}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	current := lastPrompt[len(lastPrompt)-1]
	if len(current.Images) != 2 || current.Images[0] != "data:https://x/1.png" || current.Images[1] != "data:https://x/2.jpg" {
		t.Errorf("unexpected images in prompt: %v", current.Images)
	}
}

func TestAmbientTick_TranscriptImagesVisionDisabled(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	a.Config.LLM.Vision = false
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.9} // reply mode

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		return []*discordgo.Message{
			{
				Content: "look at this",
				Author:  &discordgo.User{ID: "a1", Username: "Alice"},
				Attachments: []*discordgo.MessageAttachment{
					{URL: "https://x/1.png", ContentType: "image/png"},
				},
			},
		}, nil
	}
	a.ImageClient = &mockImageClient{
		ImageToDataURIFn: func(ctx context.Context, url string) (string, error) {
			t.Error("ImageToDataURI should not be called with vision disabled")
			return "", nil
		},
	}
	var lastPrompt []llm.Message
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		lastPrompt = messages
		return "ok", "", nil
	}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	if images := lastPrompt[len(lastPrompt)-1].Images; len(images) != 0 {
		t.Errorf("expected no images with vision disabled, got %v", images)
	}
}

func TestAmbientRun_ProbabilityGateAndShutdown(t *testing.T) {
	a, s, _, sm, _, rolls := setupAmbient(t, config.AmbientConfig{TickProbability: 0})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	if err := sm.AddAmbientChannel(context.Background(), guildID, "chan1"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}
	rolls.values = []float64{0.5}

	var sends int32
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		atomic.AddInt32(&sends, 1)
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancellation")
	}
	if n := atomic.LoadInt32(&sends); n != 0 {
		t.Errorf("expected no sends with TickProbability 0, got %d", n)
	}
}

func TestAmbientRun_ClearDuringSleep(t *testing.T) {
	// The guild's first deadline is ~1s out; the channel is cleared while
	// Run sleeps until it. The wake must not tick the cleared guild.
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{MinSeconds: 0, MaxSeconds: 1, TickProbability: 1})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	if err := sm.AddAmbientChannel(context.Background(), guildID, "chan1"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}
	rolls.values = []float64{0.99, 0.1} // first roll: ~1s deadline; then topic mode

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "hi", "", nil
	}
	var sends int32
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		atomic.AddInt32(&sends, 1)
		return nil, nil
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		sm.ClearAmbientChannels(context.Background(), guildID)
	}()

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(runCtx)
		close(done)
	}()
	time.Sleep(1500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancellation")
	}

	if n := atomic.LoadInt32(&sends); n != 0 {
		t.Errorf("expected no sends after the channel was cleared, got %d", n)
	}
}

func TestAmbientRun_PerGuildScheduling(t *testing.T) {
	// Intervals up to 1 second so every guild is due several times within
	// the observation window; TickProbability 1 so the gate never skips.
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{MinSeconds: 0, MaxSeconds: 1, TickProbability: 1})
	ctx := context.Background()
	for _, guildID := range []string{"guild-a", "guild-b"} {
		setActiveCharacter(t, sm, guildID)
		if err := sm.AddAmbientChannel(ctx, guildID, "chan-"+guildID); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
	}
	rolls.values = []float64{0.1} // topic mode

	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "hi", "", nil
	}
	var mu sync.Mutex
	sent := map[string]int{}
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		mu.Lock()
		sent[channelID]++
		mu.Unlock()
		return nil, nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		a.Run(runCtx)
		close(done)
	}()
	time.Sleep(3500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancellation")
	}

	mu.Lock()
	defer mu.Unlock()
	for _, channelID := range []string{"chan-guild-a", "chan-guild-b"} {
		if sent[channelID] == 0 {
			t.Errorf("expected %s to speak at least once, sends: %v", channelID, sent)
		}
	}
}

func TestAmbientTick_TopicModeMultiChannel(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	// Mode flip: topic; fallback pick: index 1 of the sorted [chan1 chan2].
	rolls.values = []float64{0.1, 0.9}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		return []*discordgo.Channel{
			{ID: "chan1", Name: "alpha", Type: discordgo.ChannelTypeGuildText},
			{ID: "chan2", Name: "lobby", Type: discordgo.ChannelTypeGuildText},
		}, nil
	}
	var sentChannel, sentContent string
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		sentChannel, sentContent = channelID, content
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "CHANNEL: 1\nLet us start with something.", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1", "chan2"})

	if sentChannel != "chan1" {
		t.Errorf("expected the send routed to the CHANNEL: line's channel chan1, got %q", sentChannel)
	}
	if sentContent != "Let us start with something." {
		t.Errorf("expected the CHANNEL: line stripped from the visible message, got %q", sentContent)
	}

	ctx := context.Background()
	history, err := sm.GetHistory(ctx, guildID, "1", 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d (err %v)", len(history), err)
	}
	wantCue := ambientTopicCue + "\n" + ambientChannelListLabel + "\n1. #alpha\n2. #lobby\n" +
		ambientChannelPickInstruction
	if history[0].Role != "user" || history[0].Content != wantCue {
		t.Errorf("unexpected user row:\n%s\nwant:\n%s", history[0].Content, wantCue)
	}
	if history[1].Role != "assistant" || history[1].Content != "Let us start with something." {
		t.Errorf("expected the stored assistant row stripped, got %q", history[1].Content)
	}
}

func TestAmbientTick_TopicModeMultiChannelNoLine(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	// Mode flip: topic; fallback pick: index 0 of the sorted [chan1 chan2].
	rolls.values = []float64{0.1, 0.25}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		return []*discordgo.Channel{
			{ID: "chan1", Name: "alpha", Type: discordgo.ChannelTypeGuildText},
			{ID: "chan2", Name: "lobby", Type: discordgo.ChannelTypeGuildText},
		}, nil
	}
	var sentChannel, sentContent string
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		sentChannel, sentContent = channelID, content
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "Just a thought.", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1", "chan2"})

	if sentChannel != "chan1" {
		t.Errorf("expected the fallback channel chan1, got %q", sentChannel)
	}
	if sentContent != "Just a thought." {
		t.Errorf("expected the message intact, got %q", sentContent)
	}
}

func TestAmbientTick_TopicModeMultiChannelUnknownChannel(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	// Mode flip: topic; fallback pick: index 1 of the sorted [chan1 chan2].
	rolls.values = []float64{0.1, 0.9}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		return []*discordgo.Channel{
			{ID: "chan1", Name: "alpha", Type: discordgo.ChannelTypeGuildText},
			{ID: "chan2", Name: "lobby", Type: discordgo.ChannelTypeGuildText},
		}, nil
	}
	var sentChannel, sentContent string
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		sentChannel, sentContent = channelID, content
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "CHANNEL: 9\nA wandering line.", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1", "chan2"})

	if sentChannel != "chan2" {
		t.Errorf("expected the fallback channel chan2, got %q", sentChannel)
	}
	if sentContent != "A wandering line." {
		t.Errorf("expected the unusable CHANNEL: line stripped, got %q", sentContent)
	}
}

func TestAmbientTick_TopicModeMultiChannelNamesUnresolvable(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.1, 0.9}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		return nil, fmt.Errorf("api down")
	}
	var cueSent string
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		cueSent = messages[len(messages)-1].Content
		return "CHANNEL: 2\nvia raw id", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1", "chan2"})

	want := ambientTopicCue + "\n" + ambientChannelListLabel + "\n1. chan1\n2. chan2\n" +
		ambientChannelPickInstruction
	if cueSent != want {
		t.Errorf("expected the cue to fall back to raw channel IDs:\n%s\nwant:\n%s", cueSent, want)
	}
}

func TestAmbientTick_TranscriptModePicksChannel(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	// Mode flip: transcript; channel pick: index 1 of the sorted [chan1 chan2].
	rolls.values = []float64{0.9, 0.9}

	s.ChannelMessagesFn = func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
		if channelID != "chan2" {
			t.Errorf("expected the transcript read from the picked channel chan2, got %q", channelID)
		}
		return []*discordgo.Message{
			{Content: "hello", Author: &discordgo.User{ID: "a1", Username: "Alice"}},
		}, nil
	}
	var sentChannel string
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		sentChannel = channelID
		return nil, nil
	}
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		return "hi", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1", "chan2"})

	if sentChannel != "chan2" {
		t.Errorf("expected the reply posted to the picked channel chan2, got %q", sentChannel)
	}
}

func TestAmbientTick_TopicModeSingleChannelNoList(t *testing.T) {
	a, s, llmMock, sm, _, rolls := setupAmbient(t, config.AmbientConfig{ReplyCount: 5})
	guildID := "guild1"
	setActiveCharacter(t, sm, guildID)
	rolls.values = []float64{0.1}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		t.Error("GuildChannels should not be called for a single-channel topic turn")
		return nil, nil
	}
	var sentChannel string
	s.ChannelMessageSendFn = func(channelID, content string) (*discordgo.Message, error) {
		sentChannel = channelID
		return nil, nil
	}
	var lastPrompt []llm.Message
	llmMock.GenerateResponseFn = func(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
		lastPrompt = messages
		return "hi", "", nil
	}

	a.tick(context.Background(), guildID, []string{"chan1"})

	if sentChannel != "chan1" {
		t.Errorf("expected the send in chan1, got %q", sentChannel)
	}
	if got := lastPrompt[len(lastPrompt)-1].Content; got != ambientTopicCue {
		t.Errorf("expected the plain topic cue, got %q", got)
	}
}

func TestSplitChannelLine(t *testing.T) {
	t.Run("channel line", func(t *testing.T) {
		number, rest, ok := splitChannelLine("CHANNEL: 2\nbody")
		if !ok || number != "2" || rest != "body" {
			t.Errorf("got number=%q rest=%q ok=%v", number, rest, ok)
		}
	})
	t.Run("no line", func(t *testing.T) {
		if _, _, ok := splitChannelLine("just a message"); ok {
			t.Error("did not expect a CHANNEL: line")
		}
	})
	t.Run("line is not the first line", func(t *testing.T) {
		if _, _, ok := splitChannelLine("body\nCHANNEL: 2"); ok {
			t.Error("did not expect a CHANNEL: line when it is not first")
		}
	})
	t.Run("line only, no body", func(t *testing.T) {
		number, rest, ok := splitChannelLine("CHANNEL: 2")
		if !ok || number != "2" || rest != "" {
			t.Errorf("got number=%q rest=%q ok=%v", number, rest, ok)
		}
	})
}

func TestExtractTranscript_FileAttachments(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Vision: true, MaxImages: 2}}
	// Discord returns messages newest-first; extractTranscript reverses the
	// slice in place, so each call gets a fresh one.
	newestFirst := func() []*discordgo.Message {
		return []*discordgo.Message{
		{ID: "4", Author: &discordgo.User{ID: "a4", Username: "Dan"}, Content: "plain text"},
		{ID: "3", Author: &discordgo.User{ID: "a3", Username: "Cara"}, Content: "a photo",
			Attachments: []*discordgo.MessageAttachment{{URL: "https://img/1.png", ContentType: "image/png"}}},
		{ID: "2", Author: &discordgo.User{ID: "a2", Username: "Bob"},
			Attachments: []*discordgo.MessageAttachment{{Filename: "archive.zip", ContentType: "application/zip"}}},
		{ID: "1", Author: &discordgo.User{ID: "a1", Username: "Alice"}, Content: "check this out",
			Attachments: []*discordgo.MessageAttachment{{Filename: "report.pdf", ContentType: "application/pdf"}}},
		}
	}

	lines, imageURLs := extractTranscript(newestFirst(), "bot1", cfg)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "Alice: check this out [File: report.pdf]" {
		t.Errorf("unexpected first line %q", lines[0])
	}
	if lines[1] != "Bob: [File: archive.zip]" {
		t.Errorf("expected the file-only message to carry a marker line, got %q", lines[1])
	}
	if lines[2] != "Cara: a photo" {
		t.Errorf("expected the image message to keep its text without a file marker, got %q", lines[2])
	}
	if lines[3] != "Dan: plain text" {
		t.Errorf("unexpected fourth line %q", lines[3])
	}
	if len(imageURLs) != 1 || imageURLs[0] != "https://img/1.png" {
		t.Errorf("expected the single image URL collected, got %v", imageURLs)
	}

	// With vision disabled the image is dropped and the file markers remain.
	lines, imageURLs = extractTranscript(newestFirst(), "bot1", &config.Config{LLM: config.LLMConfig{MaxImages: 2}})
	if len(lines) != 4 || lines[2] != "Cara: a photo" {
		t.Errorf("vision disabled: expected the text lines unchanged, got %v", lines)
	}
	if len(imageURLs) != 0 {
		t.Errorf("vision disabled: expected no image URLs, got %v", imageURLs)
	}

	fileOnly := []*discordgo.Message{{ID: "9", Author: &discordgo.User{ID: "a2", Username: "Bob"},
		Attachments: []*discordgo.MessageAttachment{{Filename: "x.tar.gz", ContentType: "application/gzip"}}}}
	lines, _ = extractTranscript(fileOnly, "bot1", cfg)
	if len(lines) != 1 || lines[0] != "Bob: [File: x.tar.gz]" {
		t.Errorf("expected the file-only message to yield a marker line, got %v", lines)
	}
}
