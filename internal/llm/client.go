// Package llm provides a client for interacting with LLM servers (e.g. llama.cpp).
package llm

import (
	"context"
	"time"
)

// Message represents a single turn in a conversation.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning_content,omitempty"`
}

// LLMClient defines the interface for interacting with an LLM server.
type LLMClient interface {
	Ping(ctx context.Context) (time.Duration, error)
	GenerateResponse(ctx context.Context, messages []Message, model string) (string, string, error)
	EstimateTokens(ctx context.Context, messages []Message) int
}
