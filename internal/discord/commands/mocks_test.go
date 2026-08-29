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
	FetchCharacterFn func(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error)
	RewriteSectionFn func(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error)
}

func (m *mockSynthesizer) AnalyzeInput(ctx context.Context, input string) (*research.AnalysisResult, string, string, error) {
	if m.AnalyzeInputFn == nil {
		return nil, "", "", nil
	}
	return m.AnalyzeInputFn(ctx, input)
}

func (m *mockSynthesizer) FetchCharacter(ctx context.Context, analysis *research.AnalysisResult, avatarDataURIs []string) (*research.SynthesisResult, error) {
	if m.FetchCharacterFn == nil {
		return nil, nil
	}
	return m.FetchCharacterFn(ctx, analysis, avatarDataURIs)
}

func (m *mockSynthesizer) RewriteSection(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error) {
	if m.RewriteSectionFn == nil {
		return nil, nil
	}
	return m.RewriteSectionFn(ctx, req)
}

type mockImageClient = mocks.MockImageClient

type mockDiscordSession = mocks.MockDiscordSession

// testDeps holds the raw dependencies used to construct commands directly in tests.
type testDeps struct {
	Session     *session.Manager
	LLM         llm.LLMClient
	Config      *config.Config
	Audit       *audit.AuditLogger
	Search      search.SearchProvider
	ImageSearch search.ImageSearchProvider
	Synthesizer research.Synthesizer
	ImageClient images.ImageClient
}
