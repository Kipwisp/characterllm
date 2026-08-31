package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGenerateResponse_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 100*time.Millisecond)
	start := time.Now()
	_, _, err := client.GenerateResponse(context.Background(), []Message{{Role: "user", Content: "hi"}}, "test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// With the 100ms client timeout the full retry sequence (3 attempts + 1s/2s backoff)
	// finishes in ~3.3s; without it the server's 2s sleep per attempt would take ~9s.
	if elapsed > 5*time.Second {
		t.Errorf("expected request to abort quickly, took %v", elapsed)
	}
}

func TestImageTokenEstimateFor(t *testing.T) {
	tests := []struct {
		maxEdge int
		want    int
		wantErr bool
	}{
		{-1, 0, true},
		{0, 0, true},
		{256, 150, false}, // 125 scales below the floor
		{512, 500, false},
		{1024, 2000, false},
	}
	for _, tt := range tests {
		got, err := ImageTokenEstimateFor(tt.maxEdge)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ImageTokenEstimateFor(%d): expected error, got %d", tt.maxEdge, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ImageTokenEstimateFor(%d): unexpected error: %v", tt.maxEdge, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ImageTokenEstimateFor(%d) = %d, want %d", tt.maxEdge, got, tt.want)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		messages       []Message
		expectedTokens int
		wantRemote     bool
	}{
		{
			name: "remote_supported",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/tokenize" {
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]int{"tokens": 100})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			messages:       []Message{{Role: "user", Content: "Hello world"}},
			expectedTokens: 100,
			wantRemote:     true,
		},
		{
			name: "remote_unsupported_fallback",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			messages:       []Message{{Role: "user", Content: "Hello world"}}, // 11 chars / 4 = 2
			expectedTokens: 2,
			wantRemote:     false,
		},
		{
			name: "remote_error_fallback",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			messages:       []Message{{Role: "user", Content: "Hello world"}},
			expectedTokens: 2,
			wantRemote:     false,
		},
		{
			name: "local_approximation_reasoning",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			messages: []Message{
				{Role: "user", Content: "Hello", Reasoning: "Thinking..."}, // 5 + 11 = 16 chars / 4 = 4
			},
			expectedTokens: 4,
			wantRemote:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			client := NewClient(server.URL, 30*time.Second).(*OpenAIClient)
			ctx := context.Background()

			tokens := client.EstimateTokens(ctx, tt.messages)

			if tokens != tt.expectedTokens {
				t.Errorf("EstimateTokens() = %v, want %v", tokens, tt.expectedTokens)
			}

			if tt.wantRemote && !client.tokenizationSupported {
				t.Errorf("expected tokenization to be supported")
			}
			if !tt.wantRemote && client.tokenizationSupported {
				t.Errorf("expected tokenization to be unsupported")
			}
		})
	}
}

func TestTokenizationCaching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second).(*OpenAIClient)
	ctx := context.Background()

	// First call should trigger the network request to verify support
	client.EstimateTokens(ctx, []Message{{Role: "user", Content: "test"}})
	if !client.tokenizationTested {
		t.Error("expected tokenizationTested to be true after first call")
	}

	// Now change the server to support tokenization
	// Since it's cached as unsupported, the client should NOT make this call
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not have been called after caching unsupported status")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]int{"tokens": 100})
	})

}

func TestPing(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "ping_success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name: "ping_failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			client := NewClient(server.URL, 30*time.Second)
			_, err := client.Ping(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateResponse(t *testing.T) {
	tests := []struct {
		name           string
		handler        func(w http.ResponseWriter, r *http.Request)
		messages       []Message
		model          string
		expectedResp   string
		expectedReason string
		wantErr        bool
	}{
		{
			name: "generate_success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(LlamaResponse{
					Choices: []llamaChoice{{Message: llamaMessage{Role: "assistant", Content: json.RawMessage(`"Hello!"`), Reasoning: "I am greeting the user."}}},
				})
			},
			messages:       []Message{{Role: "user", Content: "Hi"}},
			model:          "test-model",
			expectedResp:   "Hello!",
			expectedReason: "I am greeting the user.",
			wantErr:        false,
		},
		{
			name: "generate_bad_request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("bad request"))
			},
			messages: []Message{{Role: "user", Content: "Hi"}},
			model:    "test-model",
			wantErr:  true,
		},
		{
			name: "generate_no_choices",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(LlamaResponse{Choices: nil})
			},
			messages: []Message{{Role: "user", Content: "Hi"}},
			model:    "test-model",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			client := NewClient(server.URL, 30*time.Second)
			resp, reason, err := client.GenerateResponse(context.Background(), tt.messages, tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if resp != tt.expectedResp {
					t.Errorf("expected response %q, got %q", tt.expectedResp, resp)
				}
				if reason != tt.expectedReason {
					t.Errorf("expected reasoning %q, got %q", tt.expectedReason, reason)
				}
			}
		})
	}
}

func TestGenerateResponse_Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(LlamaResponse{
			Choices: []llamaChoice{{Message: llamaMessage{Role: "assistant", Content: json.RawMessage(`"Retry Success"`), Reasoning: "Worked after retry"}}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second)
	resp, reason, err := client.GenerateResponse(context.Background(), []Message{{Role: "user", Content: "Hi"}}, "model")

	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if resp != "Retry Success" {
		t.Errorf("expected 'Retry Success', got %q", resp)
	}
	if reason != "Worked after retry" {
		t.Errorf("expected 'Worked after retry', got %q", reason)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestToLlamaMessages_Plain(t *testing.T) {
	msgs, err := toLlamaMessages([]Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(msgs[0].Content); got != `"hi"` {
		t.Errorf("expected plain string content, got %s", got)
	}
	if msgs[0].Role != "user" {
		t.Errorf("unexpected role: %q", msgs[0].Role)
	}
}

func TestToLlamaMessages_WithImages(t *testing.T) {
	msgs, err := toLlamaMessages([]Message{
		{Role: "user", Content: "look at this", Images: []string{"data:image/jpeg;base64,abc"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(msgs[0].Content, &parts); err != nil {
		t.Fatal(err)
	}
	if msgs[0].Role != "user" || len(parts) != 2 {
		t.Fatalf("unexpected structure: role=%q parts=%+v", msgs[0].Role, parts)
	}
	if parts[0].Type != "text" || parts[0].Text != "look at this" {
		t.Errorf("unexpected text part: %+v", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL.URL != "data:image/jpeg;base64,abc" {
		t.Errorf("unexpected image part: %+v", parts[1])
	}
}

func TestLlamaMessageText_StringContent(t *testing.T) {
	var w llamaMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"hello"}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Role != "assistant" || w.text() != "hello" {
		t.Errorf("unexpected message: role=%q text=%q", w.Role, w.text())
	}
}

func TestLlamaMessageText_PartsContent(t *testing.T) {
	var w llamaMessage
	raw := `{"role":"user","content":[{"type":"text","text":"part one "},{"type":"image_url","image_url":{"url":"x"}},{"type":"text","text":"part two"}]}`
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	if w.text() != "part one part two" {
		t.Errorf("expected text parts concatenated, got %q", w.text())
	}
}

func TestOpenAIClient_AuthHeader(t *testing.T) {
	var seenAuth, seenNoAuth int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer test-key":
			seenAuth++
		case "":
			seenNoAuth++
		default:
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(LlamaResponse{
				Choices: []llamaChoice{{Message: llamaMessage{Role: "assistant", Content: json.RawMessage(`"ok"`)}}},
			})
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, 30*time.Second).(*OpenAIClient)
	client.APIKey = "test-key"

	if _, err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
	if _, _, err := client.GenerateResponse(context.Background(), []Message{{Role: "user", Content: "hi"}}, "model"); err != nil {
		t.Fatalf("GenerateResponse() error: %v", err)
	}
	if seenAuth != 2 {
		t.Errorf("expected 2 authenticated requests, got %d", seenAuth)
	}
	if seenNoAuth != 0 {
		t.Errorf("expected no unauthenticated requests, got %d", seenNoAuth)
	}

	noAuth := NewClient(server.URL, 30*time.Second).(*OpenAIClient)
	if _, _, err := noAuth.GenerateResponse(context.Background(), []Message{{Role: "user", Content: "hi"}}, "model"); err != nil {
		t.Fatalf("GenerateResponse() error: %v", err)
	}
	if seenNoAuth != 1 {
		t.Errorf("expected 1 unauthenticated request, got %d", seenNoAuth)
	}
}
