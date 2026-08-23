// Main entry point for the LLMCharacter Discord bot.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/discord"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
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

	// Initialize LLM Client
	llmClient := llm.NewClient(cfg.LLM.URL)

	// Initialize Session Manager
	var defaultPrompt string
	if cfg.Prompts.SystemPath != "" {
		content, err := os.ReadFile(cfg.Prompts.SystemPath)
		if err != nil {
			slog.Warn("could not read system prompt file", "path", cfg.Prompts.SystemPath, "error", err)
		} else {
			defaultPrompt = string(content)
		}
	}

	sessionMgr, err := session.NewManager("bot_sessions.db", defaultPrompt)
	if err != nil {
		slog.Error("failed to initialize session manager", "error", err)
		os.Exit(1)
	}
	defer sessionMgr.Close()

	// Setup Discord Handlers (Pass config for model name)
	auditLogger := audit.NewAuditLogger("logs")
	handlers := discord.NewHandlers(llmClient, sessionMgr, cfg, auditLogger)

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
