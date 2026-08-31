// Package config provides configuration loading and management for the bot.
package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	Discord  DiscordConfig
	LLM      LLMConfig
	Prompts  PromptConfig
	Search   SearchConfig
	Research ResearchConfig
	Images   ImageConfig
	Invite   InviteConfig
	Ambient  AmbientConfig
	General  GeneralConfig
}

type DiscordConfig struct {
	Token    string
	ClientID string
}

type LLMConfig struct {
	URL string
	// APIKey, when set, is sent as a Bearer Authorization header on every
	// LLM request.
	APIKey              string
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
	SystemPath       string
	CompactionPath   string
	SynthesisPath    string
	AnalyzerPath     string
	EditSectionPath  string
	SourceSelectPath string
}

type SearchConfig struct {
	Provider       string
	SearXNGURL     string
	SearXNGEngines string
	// MaxResults bounds the number of search results per query.
	MaxResults int
}

type ResearchConfig struct {
	// MaxSourceChars caps the scraped source page's length (in characters)
	// included in the synthesis prompt, below the context-derived scrape
	// budget. Zero leaves the budget as the only cap.
	MaxSourceChars int
}

type ImageConfig struct {
	CacheDir string
	// MaxImageEdge caps the long edge (in pixels) of processed images.
	// Loaded via getEnvPositiveInt, so it is always positive.
	MaxImageEdge int
	// MaxImageSearchResults bounds the number of image candidates searched
	// per character.
	MaxImageSearchResults int
}

type InviteConfig struct {
	// CommandEnabled registers the /invite slash command.
	CommandEnabled bool
}

type AmbientConfig struct {
	// Enabled starts the ambient scheduler and registers /addambientchannel.
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
	LogLevel string
	// ConversationLog toggles the per-guild conversation audit files.
	ConversationLog bool
	// CommandsAdminOnly registers every slash command with the
	// Administrator permission requirement, so only members with an admin
	// role can see and use the bot's commands.
	CommandsAdminOnly bool
}

// LoadConfig loads configuration from .env file and environment variables.
func LoadConfig() *Config {
	_ = godotenv.Load()

	return &Config{
		Discord: DiscordConfig{
			Token:    getEnv("DISCORD_TOKEN", ""),
			ClientID: getEnv("CLIENT_ID", ""),
		},
		LLM: LLMConfig{
			URL:                 getEnv("LLM_URL", "http://localhost:8080/v1/chat/completions"),
			APIKey:              getEnv("LLM_API_KEY", ""),
			Model:               getEnv("LLM_MODEL", ""),
			MaxRetries:          getEnvPositiveInt("LLM_MAX_RETRIES", 2),
			MaxContext:          getEnvPositiveInt("LLM_MAX_CONTEXT", 10000),
			CompactionThreshold: getEnvFloatClamped("LLM_COMPACTION_THRESHOLD", 0.9, 0, 1),
			RecentMemoryWindow:  getEnvPositiveInt("LLM_RECENT_MEMORY_WINDOW", 15),
			SummaryMaxTokens:    getEnvPositiveInt("LLM_SUMMARY_MAX_TOKENS", 2048),
			TimeoutSeconds:      getEnvPositiveInt("LLM_TIMEOUT_SECONDS", 120),
			Vision:              getEnvBool("LLM_VISION", false),
			MaxImages:           getEnvPositiveInt("LLM_MAX_IMAGES", 2),
			AvatarPick:          getEnvBool("LLM_AVATAR_PICK", false),
		},
		Prompts: PromptConfig{
			SystemPath:       getEnv("SYSTEM_PROMPT_PATH", "prompts/system_prompt.md"),
			CompactionPath:   getEnv("COMPACTION_PROMPT_PATH", "prompts/compaction_prompt.md"),
			SynthesisPath:    getEnv("SYNTHESIS_PROMPT_PATH", "prompts/synthesis_prompt.md"),
			AnalyzerPath:     getEnv("ANALYZER_PROMPT_PATH", "prompts/analyzer_prompt.md"),
			EditSectionPath:  getEnv("EDIT_SECTION_PROMPT_PATH", "prompts/edit_section_prompt.md"),
			SourceSelectPath: getEnv("SOURCE_SELECT_PROMPT_PATH", "prompts/source_select_prompt.md"),
		},
		Search: SearchConfig{
			Provider:       getEnv("SEARCH_PROVIDER", "searxng"),
			SearXNGURL:     getEnv("SEARXNG_URL", "http://localhost:8080"),
			SearXNGEngines: getEnv("SEARXNG_ENGINES", ""),
			MaxResults:     getEnvPositiveInt("MAX_SEARCH_RESULTS", 5),
		},
		Research: ResearchConfig{
			MaxSourceChars: getEnvPositiveInt("RESEARCH_MAX_SOURCE_CHARS", 0),
		},
		Images: ImageConfig{
			CacheDir:              getEnv("IMAGE_CACHE_DIR", "data/images/cache"),
			MaxImageEdge:          getEnvPositiveInt("IMAGE_MAX_EDGE", 512),
			MaxImageSearchResults: getEnvPositiveInt("MAX_IMAGE_SEARCH_RESULTS", 10),
		},
		Invite: InviteConfig{
			CommandEnabled: getEnvBool("INVITE_COMMAND_ENABLED", true),
		},
		Ambient: loadAmbientConfig(),
		General: GeneralConfig{
			LogLevel:          getEnv("LOG_LEVEL", "INFO"),
			ConversationLog:   getEnvBool("CONVERSATION_LOG", false),
			CommandsAdminOnly: getEnvBool("COMMANDS_ADMIN_ONLY", false),
		},
	}
}

func loadAmbientConfig() AmbientConfig {
	c := AmbientConfig{
		Enabled:          getEnvBool("AMBIENT_ENABLED", true),
		MinSeconds:       getEnvPositiveInt("AMBIENT_MIN_SECONDS", 14400),
		MaxSeconds:       getEnvPositiveInt("AMBIENT_MAX_SECONDS", 21600),
		ReplyCount:       getEnvPositiveInt("AMBIENT_REPLY_COUNT", 5),
		TickProbability:  getEnvFloatClamped("AMBIENT_TICK_PROBABILITY", 0.5, 0, 1),
		ReplyProbability: getEnvFloatClamped("AMBIENT_REPLY_PROBABILITY", 0.1, 0, 1),
	}
	if c.MaxSeconds < c.MinSeconds {
		c.MaxSeconds = c.MinSeconds
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

// getEnvPositiveInt is like getEnvInt but also falls back when the value is
// not a positive number.
func getEnvPositiveInt(key string, fallback int) int {
	i := getEnvInt(key, fallback)
	if i <= 0 {
		return fallback
	}
	return i
}

// getEnvFloatClamped is like getEnvFloat but clamps the value to [min, max].
func getEnvFloatClamped(key string, fallback, min, max float64) float64 {
	f := getEnvFloat(key, fallback)
	if f < min {
		return min
	}
	if f > max {
		return max
	}
	return f
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
