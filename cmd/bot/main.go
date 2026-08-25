// Main entry point for the LLMCharacter Discord bot.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/discord"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/prompts"
	"characterllm/internal/session"
)

func main() {
	cfg := config.LoadConfig()

	// Initialize structured logging
	logger.Init(cfg.General.LogLevel)
	slog.Info("starting CharacterLLM bot")

	if cfg.Discord.Token == "" {
		slog.Error("Discord token is missing! Set it in .env or via DISCORD_TOKEN environment variable")
		os.Exit(1)
	}

	// Load prompt templates; fail fast if any file is missing or unreadable
	promptSet, err := prompts.Load(cfg.Prompts.SystemPath, cfg.Prompts.CompactionPath, cfg.Prompts.SynthesisPath, cfg.Prompts.AnalyzerPath)
	if err != nil {
		slog.Error("failed to load prompt files", "error", err)
		os.Exit(1)
	}

	// Initialize LLM Client
	llmClient := llm.NewClient(cfg.LLM.URL, time.Duration(cfg.LLM.TimeoutSeconds)*time.Second)

	// Initialize Session Manager
	sessionMgr, err := session.NewManager("bot_sessions.db", promptSet.System)
	if err != nil {
		slog.Error("failed to initialize session manager", "error", err)
		os.Exit(1)
	}
	defer sessionMgr.Close()

	// Setup Discord Handlers (Pass config for model name)
	auditLogger := audit.NewAuditLogger("logs")
	handlers, err := discord.NewHandlers(llmClient, sessionMgr, cfg, auditLogger, promptSet)
	if err != nil {
		slog.Error("failed to initialize discord handlers", "error", err)
		os.Exit(1)
	}

	// Initialize and Start Discord Bot
	bot, err := discord.NewBot(cfg.Discord.Token, handlers)

	if err != nil {
		slog.Error("failed to initialize bot", "error", err)
		os.Exit(1)
	}

	err = bot.Start()
	if err != nil {
		slog.Error("failed to start bot", "error", err)
		os.Exit(1)
	}

	slog.Info("bot is now running. Press CTRL-C to exit.")

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("shutting down")
}
