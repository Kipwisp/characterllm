package commands

import (
	"context"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/images"
	"characterllm/internal/llm"
	"characterllm/internal/mocks"
	"characterllm/internal/research"
	"characterllm/internal/search"
	"characterllm/internal/session"
)

type mockLLMClient = mocks.MockLLMClient

type mockSynthesizer struct {
	AnalyzeInputFn   func(ctx context.Context, input string) (*research.AnalysisResult, string, string, error)
	FetchCharacterFn func(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error)
}

func (m *mockSynthesizer) AnalyzeInput(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
	if m.AnalyzeInputFn == nil {
		return nil, "", "", nil
	}
	return m.AnalyzeInputFn(ctx, input)
}

func (m *mockSynthesizer) FetchCharacter(ctx context.Context, analysis *research.AnalysisResult) (*research.SynthesisResult, error) {
	if m.FetchCharacterFn == nil {
		return nil, nil
	}
	return m.FetchCharacterFn(ctx, analysis)
}

type mockImageClient = mocks.MockImageClient

type mockDiscordSession = mocks.MockDiscordSession

type mockCommandContext struct {
	Session     *session.Manager
	LLM         llm.LLMClient
	Config      *config.Config
	Audit       *audit.AuditLogger
	Search      search.SearchProvider
	ImageSearch search.ImageSearchProvider
	Synthesizer research.Synthesizer
	ImageClient images.ImageClient
}

func (m *mockCommandContext) GetSession() *session.Manager             { return m.Session }
func (m *mockCommandContext) GetLLM() llm.LLMClient                    { return m.LLM }
func (m *mockCommandContext) GetConfig() *config.Config                { return m.Config }
func (m *mockCommandContext) GetAudit() *audit.AuditLogger             { return m.Audit }
func (m *mockCommandContext) GetSearchProvider() search.SearchProvider { return m.Search }
func (m *mockCommandContext) GetImageSearchProvider() search.ImageSearchProvider {
	return m.ImageSearch
}
func (m *mockCommandContext) GetSynthesizer() research.Synthesizer { return m.Synthesizer }
func (m *mockCommandContext) GetImageClient() images.ImageClient   { return m.ImageClient }
