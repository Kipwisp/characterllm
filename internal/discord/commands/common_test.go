package commands

import (
	"testing"

	"characterllm/internal/testkit"
)

func setupCommandTest(t *testing.T) (*testDeps, *mockDiscordSession, string) {
	env := testkit.NewEnv(t)

	ctx := &testDeps{
		Session: env.Session,
		LLM:     env.LLM,
		Config:  env.Config,
		Audit:   env.Audit,
	}

	return ctx, &mockDiscordSession{}, env.DBPath
}
