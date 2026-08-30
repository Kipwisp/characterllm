// Main entry point for the LLMCharacter Discord bot.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/conversation"
	"characterllm/internal/discord"
	"characterllm/internal/discord/commands"
	"characterllm/internal/images"
	"characterllm/internal/llm"
	"characterllm/internal/logger"
	"characterllm/internal/prompts"
	"characterllm/internal/research"
	"characterllm/internal/safehttp"
	"characterllm/internal/scrape"
	"characterllm/internal/search"
	"characterllm/internal/session"
)

// database file
const sessionDBPath = "data/bot_sessions.db"

func main() {
	cfg := config.LoadConfig()

	// Initialize structured logging
	logger.Init(cfg.General.LogLevel)
	slog.Info("starting CharacterLLM bot")

	if cfg.Discord.Token == "" {
		slog.Error("Discord token is missing! Set it in .env or via DISCORD_TOKEN environment variable")
		os.Exit(1)
	}

	if cfg.Discord.ClientID == "" {
		slog.Error("Discord client ID is missing! Set it in .env or via CLIENT_ID environment variable")
		os.Exit(1)
	}

	// Load prompt templates; fail fast if any file is missing or unreadable
	promptSet, err := prompts.Load(cfg.Prompts.SystemPath, cfg.Prompts.CompactionPath, cfg.Prompts.SynthesisPath, cfg.Prompts.AnalyzerPath, cfg.Prompts.EditSectionPath, cfg.Prompts.SourceSelectPath)
	if err != nil {
		slog.Error("failed to load prompt files", "error", err)
		os.Exit(1)
	}

	// Initialize LLM Client
	llmClient := llm.NewClient(cfg.LLM.URL, time.Duration(cfg.LLM.TimeoutSeconds)*time.Second)
	imageTokenEstimate, err := llm.ImageTokenEstimateFor(cfg.Images.MaxImageEdge)
	if err != nil {
		slog.Error("invalid IMAGE_MAX_EDGE", "error", err)
		os.Exit(1)
	}
	llmClient.(*llm.OpenAIClient).ImageTokenEstimate = imageTokenEstimate

	// Initialize Session Manager
	sessionMgr, err := session.NewManager(sessionDBPath, promptSet.System)
	if err != nil {
		slog.Error("failed to initialize session manager", "error", err)
		os.Exit(1)
	}
	defer sessionMgr.Close()

	// Setup Discord Handlers (Pass config for model name)
	auditLogger := audit.NewAuditLogger("logs", cfg.General.ConversationLog)

	searchProvider, imageSearchProvider, err := search.NewProvider(cfg.Search.Provider, cfg.Search.SearXNGURL, cfg.Search.SearXNGEngines)
	if err != nil {
		slog.Error("failed to initialize search provider", "error", err)
		os.Exit(1)
	}

	imageClient := images.NewImageClient(imageSearchProvider, cfg.Images.CacheDir, cfg.Images.MaxImageEdge)
	if imageClient == nil {
		slog.Error("failed to initialize image client")
		os.Exit(1)
	}

	locks := discord.NewConversationLocks()

	synthesizer := research.NewSynthesizer(searchProvider, llmClient, cfg, promptSet, scrape.New(safehttp.NewFetcher()))

	commandRegistry := commands.New(commands.Deps{
		Session:     sessionMgr,
		LLM:         llmClient,
		Model:       cfg.LLM.Model,
		Audit:       auditLogger,
		ImageClient: imageClient,
		Synthesizer: synthesizer,
		Config:      cfg,
		Lock:        locks.Lock,
	})

	chat := &discord.Chat{
		LLM:           llmClient,
		Session:       sessionMgr,
		Config:        cfg,
		Audit:         auditLogger,
		ImageClient:   imageClient,
		PromptBuilder: conversation.NewPromptBuilder(llmClient, sessionMgr, cfg, promptSet),
		Compactor:     conversation.NewCompactor(llmClient, sessionMgr, cfg, auditLogger, promptSet),
		Locks:         locks,
	}

	router := &discord.Router{
		Chat:            chat,
		CommandRegistry: commandRegistry,
	}

	// Initialize and Start Discord Bot
	bot, err := discord.NewBot(cfg.Discord.Token, router)

	if err != nil {
		slog.Error("failed to initialize bot", "error", err)
		os.Exit(1)
	}

	err = bot.Start()
	if err != nil {
		slog.Error("failed to start bot", "error", err)
		os.Exit(1)
	}

	bot.RegisterCommands(commandRegistry.Definitions())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Ambient.Enabled {
		ambient := discord.NewAmbient(sessionMgr, chat, cfg, imageClient, discord.NewSessionWrapper(bot.Session))
		go ambient.Run(ctx)
		slog.Info("ambient scheduler started")
	}

	slog.Info("bot is now running. Press CTRL-C to exit.")

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	cancel()

	slog.Info("shutting down")
}
