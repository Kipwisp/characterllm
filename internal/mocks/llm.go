// Package mocks provides shared test doubles for the project's interfaces.
// Mocks use function fields: set only the behavior a test needs, and leave the
// rest unset (unset fields return zero values).
package mocks

import (
	"context"
	"time"

	"characterllm/internal/llm"
)

// MockLLMClient is a configurable test double for llm.LLMClient.
type MockLLMClient struct {
	GenerateResponseFn func(ctx context.Context, messages []llm.Message, model string) (string, string, error)
	EstimateTokensFn   func(ctx context.Context, messages []llm.Message) int
	PingFn             func(ctx context.Context) (time.Duration, error)
}

func (m *MockLLMClient) GenerateResponse(ctx context.Context, messages []llm.Message, model string) (string, string, error) {
	if m.GenerateResponseFn == nil {
		return "", "", nil
	}
	return m.GenerateResponseFn(ctx, messages, model)
}

func (m *MockLLMClient) EstimateTokens(ctx context.Context, messages []llm.Message) int {
	if m.EstimateTokensFn == nil {
		return 0
	}
	return m.EstimateTokensFn(ctx, messages)
}

func (m *MockLLMClient) Ping(ctx context.Context) (time.Duration, error) {
	if m.PingFn == nil {
		return 0, nil
	}
	return m.PingFn(ctx)
}
