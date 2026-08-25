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
					Choices: []struct {
						Message Message `json:"message"`
					}{{Message: Message{Content: "Hello!", Reasoning: "I am greeting the user."}}},
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
			Choices: []struct {
				Message Message `json:"message"`
			}{{Message: Message{Content: "Retry Success", Reasoning: "Worked after retry"}}},
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
