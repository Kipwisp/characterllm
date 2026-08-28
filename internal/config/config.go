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
	Search  SearchConfig
	Images  ImageConfig
	Invite  InviteConfig
	Ambient AmbientConfig
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
	// AvatarPick lets a vision LLM choose the character's avatar from the
	// candidate row during /createcharacter, instead of the user picking
	// from the select menu. Only effective when Vision is also enabled.
	AvatarPick bool
}

type PromptConfig struct {
	SystemPath      string
	CompactionPath  string
	SynthesisPath   string
	AnalyzerPath    string
	EditSectionPath string
}

type SearchConfig struct {
	Provider       string
	SearXNGURL     string
	SearXNGEngines string
	// MaxResults bounds the number of search results per query.
	MaxResults int
}

type ImageConfig struct {
	CacheDir string
}

type InviteConfig struct {
	// CommandEnabled registers the /invite slash command.
	CommandEnabled bool
}

type AmbientConfig struct {
	// Enabled starts the ambient scheduler and registers /setambientchannel.
	Enabled bool
	// MinSeconds/MaxSeconds bound the random sleep between ambient ticks.
	MinSeconds int
	MaxSeconds int
	// ReplyCount is how many recent channel messages the reply mode reads.
	ReplyCount int
	// TickProbability is the per-tick chance an ambient turn actually runs.
	TickProbability float64
	// ReplyProbability is the chance the bot joins a user's threaded reply
	// in the ambient channel.
	ReplyProbability float64
}

type GeneralConfig struct {
	LogLevel  string
	TopicRate string
	// ConversationLog toggles the per-guild conversation audit files.
	ConversationLog bool
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
			MaxContext:          getEnvInt("LLM_MAX_CONTEXT", 10000),
			CompactionThreshold: getEnvFloat("LLM_COMPACTION_THRESHOLD", 0.9),
			RecentMemoryWindow:  getEnvInt("LLM_RECENT_MEMORY_WINDOW", 15),
			SummaryMaxTokens:    getEnvInt("LLM_SUMMARY_MAX_TOKENS", 2048),
			TimeoutSeconds:      getEnvInt("LLM_TIMEOUT_SECONDS", 120),
			Vision:              getEnvBool("LLM_VISION", false),
			MaxImages:           getEnvInt("LLM_MAX_IMAGES", 2),
			AvatarPick:          getEnvBool("LLM_AVATAR_PICK", false),
		},
		Prompts: PromptConfig{
			SystemPath:      getEnv("SYSTEM_PROMPT_PATH", "prompts/system_prompt.md"),
			CompactionPath:  getEnv("COMPACTION_PROMPT_PATH", "prompts/compaction_prompt.md"),
			SynthesisPath:   getEnv("SYNTHESIS_PROMPT_PATH", "prompts/synthesis_prompt.md"),
			AnalyzerPath:    getEnv("ANALYZER_PROMPT_PATH", "prompts/analyzer_prompt.md"),
			EditSectionPath: getEnv("EDIT_SECTION_PROMPT_PATH", "prompts/edit_section_prompt.md"),
		},
		Search: SearchConfig{
			Provider:       getEnv("SEARCH_PROVIDER", "searxng"),
			SearXNGURL:     getEnv("SEARXNG_URL", "http://localhost:8080"),
			SearXNGEngines: getEnv("SEARXNG_ENGINES", ""),
			MaxResults:     getEnvInt("MAX_SEARCH_RESULTS", 3),
		},
		Images: ImageConfig{
			CacheDir: getEnv("IMAGE_CACHE_DIR", "images/cache"),
		},
		Invite: InviteConfig{
			CommandEnabled: getEnvBool("INVITE_COMMAND_ENABLED", true),
		},
		Ambient: loadAmbientConfig(),
		General: GeneralConfig{
			LogLevel:        getEnv("LOG_LEVEL", "INFO"),
			TopicRate:       getEnv("TOPIC_RATE", "10000"),
			ConversationLog: getEnvBool("CONVERSATION_LOG", true),
		},
	}
}

func loadAmbientConfig() AmbientConfig {
	c := AmbientConfig{
		Enabled:          getEnvBool("AMBIENT_ENABLED", true),
		MinSeconds:       getEnvInt("AMBIENT_MIN_SECONDS", 14400),
		MaxSeconds:       getEnvInt("AMBIENT_MAX_SECONDS", 21600),
		ReplyCount:       getEnvInt("AMBIENT_REPLY_COUNT", 5),
		TickProbability:  getEnvFloat("AMBIENT_TICK_PROBABILITY", 0.5),
		ReplyProbability: getEnvFloat("AMBIENT_REPLY_PROBABILITY", 0.1),
	}
	if c.MaxSeconds < c.MinSeconds {
		c.MaxSeconds = c.MinSeconds
	}
	for _, p := range []*float64{&c.TickProbability, &c.ReplyProbability} {
		if *p < 0 {
			*p = 0
		} else if *p > 1 {
			*p = 1
		}
	}
	return c
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
