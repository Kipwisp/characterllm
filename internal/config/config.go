// Package config provides configuration loading and management for the bot.
package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	Discord DiscordConfig
	LLM     LLMConfig
	Prompts PromptConfig
	Images  ImageConfig
	General GeneralConfig
}

type DiscordConfig struct {
	Token       string
	ClientID    string
	MainGuild   string
	MainChannel string
}

type LLMConfig struct {
	URL                 string
	Model               string
	MaxRetries          int
	MaxContext          int
	CompactionThreshold float64
	RecentMemoryWindow  int
	SummaryMaxTokens    int
	TimeoutSeconds      int
	// Vision indicates the model accepts image content parts. When false,
	// image attachments on user messages are ignored.
	Vision bool
	// MaxImages bounds how many image attachments per message are forwarded
	// to the model.
	MaxImages int
}

type PromptConfig struct {
	SystemPath     string
	CompactionPath string
	SynthesisPath  string
	AnalyzerPath   string
}

type ImageConfig struct {
	Provider   string
	SearXNGURL string
	MaxResults int
	CacheDir   string
}

type GeneralConfig struct {
	LogLevel     string
	TopicRate    string
	BirthdayHour string
}

// LoadConfig loads configuration from .env file and environment variables.
func LoadConfig() *Config {
	_ = godotenv.Load()

	return &Config{
		Discord: DiscordConfig{
			Token:       getEnv("DISCORD_TOKEN", ""),
			ClientID:    getEnv("CLIENT_ID", ""),
			MainGuild:   getEnv("MAIN_GUILD", ""),
			MainChannel: getEnv("MAIN_CHANNEL", "general"),
		},
		LLM: LLMConfig{
			URL:                 getEnv("LLM_URL", "http://localhost:8080/v1/chat/completions"),
			Model:               getEnv("LLM_MODEL", ""),
			MaxRetries:          getEnvInt("LLM_MAX_RETRIES", 2),
			MaxContext:          getEnvInt("LLM_MAX_CONTEXT", 4096),
			CompactionThreshold: getEnvFloat("LLM_COMPACTION_THRESHOLD", 0.9),
			RecentMemoryWindow:  getEnvInt("LLM_RECENT_MEMORY_WINDOW", 15),
			SummaryMaxTokens:    getEnvInt("LLM_SUMMARY_MAX_TOKENS", 1024),
			TimeoutSeconds:      getEnvInt("LLM_TIMEOUT_SECONDS", 120),
			Vision:              getEnvBool("LLM_VISION", false),
			MaxImages:           getEnvInt("LLM_MAX_IMAGES", 2),
		},
		Prompts: PromptConfig{
			SystemPath:     getEnv("SYSTEM_PROMPT_PATH", "prompts/system_prompt.md"),
			CompactionPath: getEnv("COMPACTION_PROMPT_PATH", "prompts/compaction_prompt.md"),
			SynthesisPath:  getEnv("SYNTHESIS_PROMPT_PATH", "prompts/synthesis_prompt.md"),
			AnalyzerPath:   getEnv("ANALYZER_PROMPT_PATH", "prompts/analyzer_prompt.md"),
		},
		Images: ImageConfig{
			Provider:   getEnv("IMAGE_PROVIDER", "searxng"),
			SearXNGURL: getEnv("SEARXNG_URL", "http://localhost:8080"),
			MaxResults: getEnvInt("MAX_SEARCH_RESULTS", 3),
			CacheDir:   getEnv("IMAGE_CACHE_DIR", "images/cache"),
		},
		General: GeneralConfig{
			LogLevel:     getEnv("LOG_LEVEL", "INFO"),
			TopicRate:    getEnv("TOPIC_RATE", "10000"),
			BirthdayHour: getEnv("BIRTHDAY_HOUR", "12"),
		},
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	val := getEnv(key, "")
	if val == "" {
		return fallback
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return i
}

func getEnvFloat(key string, fallback float64) float64 {
	val := getEnv(key, "")
	if val == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getEnvBool(key string, fallback bool) bool {
	val := getEnv(key, "")
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
}
