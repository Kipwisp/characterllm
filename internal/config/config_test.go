package config

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Save original environment to restore later
	originalEnv := make(map[string]string)
	envVars := []string{
		"DISCORD_TOKEN", "CLIENT_ID", "MAIN_GUILD", "MAIN_CHANNEL",
		"LLM_URL", "LLM_MODEL", "LLM_MAX_RETRIES", "LLM_MAX_CONTEXT", "LLM_COMPACTION_THRESHOLD", "LLM_RECENT_MEMORY_WINDOW", "LLM_VISION", "LLM_MAX_IMAGES",
		"SYSTEM_PROMPT_PATH", "COMPACTION_PROMPT_PATH", "SYNTHESIS_PROMPT_PATH", "ANALYZER_PROMPT_PATH", "EDIT_SECTION_PROMPT_PATH",
		"SEARCH_PROVIDER", "SEARXNG_URL", "SEARXNG_ENGINES", "MAX_SEARCH_RESULTS", "IMAGE_CACHE_DIR", "LLM_AVATAR_PICK",
		"INVITE_COMMAND_ENABLED",
		"LOG_LEVEL", "TOPIC_RATE", "CONVERSATION_LOG",
	}

	for _, v := range envVars {
		if val, ok := os.LookupEnv(v); ok {
			originalEnv[v] = val
		}
	}

	defer func() {
		for _, v := range envVars {
			os.Unsetenv(v)
		}
		for k, v := range originalEnv {
			os.Setenv(k, v)
		}
	}()

	// Set test values
	os.Setenv("DISCORD_TOKEN", "test-token")
	os.Setenv("CLIENT_ID", "test-client")
	os.Setenv("MAIN_GUILD", "test-guild")
	os.Setenv("MAIN_CHANNEL", "test-channel")
	os.Setenv("LLM_URL", "http://test-llm")
	os.Setenv("LLM_MODEL", "test-model")
	os.Setenv("LLM_MAX_RETRIES", "5")
	os.Setenv("LLM_MAX_CONTEXT", "8192")
	os.Setenv("LLM_COMPACTION_THRESHOLD", "0.5")
	os.Setenv("LLM_RECENT_MEMORY_WINDOW", "20")
	os.Setenv("LLM_VISION", "true")
	os.Setenv("LLM_MAX_IMAGES", "4")
	os.Setenv("SYSTEM_PROMPT_PATH", "test/sys.md")
	os.Setenv("COMPACTION_PROMPT_PATH", "test/comp.md")
	os.Setenv("SYNTHESIS_PROMPT_PATH", "test/synth.md")
	os.Setenv("EDIT_SECTION_PROMPT_PATH", "test/edit.md")
	os.Setenv("ANALYZER_PROMPT_PATH", "test/anal.md")
	os.Setenv("SEARCH_PROVIDER", "test-provider")
	os.Setenv("SEARXNG_URL", "http://test-searxng")
	os.Setenv("SEARXNG_ENGINES", "google,bing")
	os.Setenv("MAX_SEARCH_RESULTS", "10")
	os.Setenv("IMAGE_CACHE_DIR", "test/cache")
	os.Setenv("LLM_AVATAR_PICK", "true")
	os.Setenv("LOG_LEVEL", "DEBUG")
	os.Setenv("TOPIC_RATE", "5000")
	os.Setenv("INVITE_COMMAND_ENABLED", "true")
	os.Setenv("CONVERSATION_LOG", "false")

	cfg := LoadConfig()

	if cfg.Discord.Token != "test-token" {
		t.Errorf("Expected DISCORD_TOKEN test-token, got %s", cfg.Discord.Token)
	}
	if cfg.Discord.ClientID != "test-client" {
		t.Errorf("Expected CLIENT_ID test-client, got %s", cfg.Discord.ClientID)
	}
	if cfg.Discord.MainGuild != "test-guild" {
		t.Errorf("Expected MAIN_GUILD test-guild, got %s", cfg.Discord.MainGuild)
	}
	if cfg.Discord.MainChannel != "test-channel" {
		t.Errorf("Expected MAIN_CHANNEL test-channel, got %s", cfg.Discord.MainChannel)
	}
	if cfg.LLM.URL != "http://test-llm" {
		t.Errorf("Expected LLM_URL http://test-llm, got %s", cfg.LLM.URL)
	}
	if cfg.LLM.Model != "test-model" {
		t.Errorf("Expected LLM_MODEL test-model, got %s", cfg.LLM.Model)
	}
	if !cfg.LLM.Vision {
		t.Error("Expected LLM_VISION true")
	}
	if cfg.LLM.MaxImages != 4 {
		t.Errorf("Expected LLM_MAX_IMAGES 4, got %d", cfg.LLM.MaxImages)
	}
	if cfg.LLM.MaxRetries != 5 {
		t.Errorf("Expected LLM_MAX_RETRIES 5, got %d", cfg.LLM.MaxRetries)
	}
	if cfg.LLM.MaxContext != 8192 {
		t.Errorf("Expected LLM_MAX_CONTEXT 8192, got %d", cfg.LLM.MaxContext)
	}
	if cfg.LLM.CompactionThreshold != 0.5 {
		t.Errorf("Expected LLM_COMPACTION_THRESHOLD 0.5, got %f", cfg.LLM.CompactionThreshold)
	}
	if cfg.LLM.RecentMemoryWindow != 20 {
		t.Errorf("Expected LLM_RECENT_MEMORY_WINDOW 20, got %d", cfg.LLM.RecentMemoryWindow)
	}
	if cfg.Prompts.SystemPath != "test/sys.md" {
		t.Errorf("Expected SYSTEM_PROMPT_PATH test/sys.md, got %s", cfg.Prompts.SystemPath)
	}
	if cfg.Prompts.CompactionPath != "test/comp.md" {
		t.Errorf("Expected COMPACTION_PROMPT_PATH test/comp.md, got %s", cfg.Prompts.CompactionPath)
	}
	if cfg.Prompts.SynthesisPath != "test/synth.md" {
		t.Errorf("Expected SYNTHESIS_PROMPT_PATH test/synth.md, got %s", cfg.Prompts.SynthesisPath)
	}
	if cfg.Prompts.AnalyzerPath != "test/anal.md" {
		t.Errorf("Expected ANALYZER_PROMPT_PATH test/anal.md, got %s", cfg.Prompts.AnalyzerPath)
	}
	if cfg.Prompts.EditSectionPath != "test/edit.md" {
		t.Errorf("Expected EDIT_SECTION_PROMPT_PATH test/edit.md, got %s", cfg.Prompts.EditSectionPath)
	}
	if cfg.Search.Provider != "test-provider" {
		t.Errorf("Expected SEARCH_PROVIDER test-provider, got %s", cfg.Search.Provider)
	}
	if cfg.Search.SearXNGURL != "http://test-searxng" {
		t.Errorf("Expected SEARXNG_URL http://test-searxng, got %s", cfg.Search.SearXNGURL)
	}
	if cfg.Search.SearXNGEngines != "google,bing" {
		t.Errorf("Expected SEARXNG_ENGINES google,bing, got %s", cfg.Search.SearXNGEngines)
	}
	if cfg.Search.MaxResults != 10 {
		t.Errorf("Expected MAX_SEARCH_RESULTS 10, got %d", cfg.Search.MaxResults)
	}
	if cfg.Images.CacheDir != "test/cache" {
		t.Errorf("Expected IMAGE_CACHE_DIR test/cache, got %s", cfg.Images.CacheDir)
	}
	if !cfg.LLM.AvatarPick {
		t.Error("Expected LLM_AVATAR_PICK true")
	}
	if !cfg.Invite.CommandEnabled {
		t.Error("Expected INVITE_COMMAND_ENABLED true")
	}
	if cfg.General.LogLevel != "DEBUG" {
		t.Errorf("Expected LOG_LEVEL DEBUG, got %s", cfg.General.LogLevel)
	}
	if cfg.General.TopicRate != "5000" {
		t.Errorf("Expected TOPIC_RATE 5000, got %s", cfg.General.TopicRate)
	}
	if cfg.General.ConversationLog {
		t.Error("Expected CONVERSATION_LOG false")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// Clear environment variables to test defaults
	envVars := []string{
		"DISCORD_TOKEN", "CLIENT_ID", "MAIN_GUILD", "MAIN_CHANNEL",
		"LLM_URL", "LLM_MODEL", "LLM_MAX_RETRIES", "LLM_MAX_CONTEXT", "LLM_COMPACTION_THRESHOLD", "LLM_RECENT_MEMORY_WINDOW", "LLM_VISION", "LLM_MAX_IMAGES",
		"SYSTEM_PROMPT_PATH", "COMPACTION_PROMPT_PATH", "SYNTHESIS_PROMPT_PATH", "ANALYZER_PROMPT_PATH", "EDIT_SECTION_PROMPT_PATH",
		"SEARCH_PROVIDER", "SEARXNG_URL", "SEARXNG_ENGINES", "MAX_SEARCH_RESULTS", "IMAGE_CACHE_DIR", "LLM_AVATAR_PICK",
		"INVITE_COMMAND_ENABLED",
		"LOG_LEVEL", "TOPIC_RATE", "CONVERSATION_LOG",
	}

	originalEnv := make(map[string]string)
	for _, v := range envVars {
		if val, ok := os.LookupEnv(v); ok {
			originalEnv[v] = val
		}
		os.Unsetenv(v)
	}

	defer func() {
		for k, v := range originalEnv {
			os.Setenv(k, v)
		}
	}()

	cfg := LoadConfig()

	if cfg.Discord.MainChannel != "general" {
		t.Errorf("Expected default MAIN_CHANNEL general, got %s", cfg.Discord.MainChannel)
	}
	if cfg.LLM.URL != "http://localhost:8080/v1/chat/completions" {
		t.Errorf("Expected default LLM_URL, got %s", cfg.LLM.URL)
	}
	if cfg.LLM.MaxRetries != 2 {
		t.Errorf("Expected default LLM_MAX_RETRIES 2, got %d", cfg.LLM.MaxRetries)
	}
	if cfg.LLM.MaxContext != 10000 {
		t.Errorf("Expected default LLM_MAX_CONTEXT 10000, got %d", cfg.LLM.MaxContext)
	}
	if cfg.LLM.CompactionThreshold != 0.9 {
		t.Errorf("Expected default LLM_COMPACTION_THRESHOLD 0.9, got %f", cfg.LLM.CompactionThreshold)
	}
	if cfg.LLM.RecentMemoryWindow != 15 {
		t.Errorf("Expected default LLM_RECENT_MEMORY_WINDOW 15, got %d", cfg.LLM.RecentMemoryWindow)
	}
	if cfg.LLM.Vision {
		t.Error("Expected default LLM_VISION false")
	}
	if cfg.LLM.MaxImages != 2 {
		t.Errorf("Expected default LLM_MAX_IMAGES 2, got %d", cfg.LLM.MaxImages)
	}
	if cfg.Prompts.SystemPath != "prompts/system_prompt.md" {
		t.Errorf("Expected default SYSTEM_PROMPT_PATH, got %s", cfg.Prompts.SystemPath)
	}
	if cfg.Search.Provider != "searxng" {
		t.Errorf("Expected default SEARCH_PROVIDER searxng, got %s", cfg.Search.Provider)
	}
	if cfg.Search.SearXNGURL != "http://localhost:8080" {
		t.Errorf("Expected default SEARXNG_URL, got %s", cfg.Search.SearXNGURL)
	}
	if cfg.Search.SearXNGEngines != "" {
		t.Errorf("Expected default SEARXNG_ENGINES, got %s", cfg.Search.SearXNGEngines)
	}
	if cfg.Search.MaxResults != 3 {
		t.Errorf("Expected default MAX_SEARCH_RESULTS 3, got %d", cfg.Search.MaxResults)
	}
	if cfg.Images.CacheDir != "images/cache" {
		t.Errorf("Expected default IMAGE_CACHE_DIR, got %s", cfg.Images.CacheDir)
	}
	if cfg.LLM.AvatarPick {
		t.Error("Expected default LLM_AVATAR_PICK false")
	}
	if cfg.Invite.CommandEnabled {
		t.Error("Expected default INVITE_COMMAND_ENABLED false")
	}
	if cfg.General.LogLevel != "INFO" {
		t.Errorf("Expected default LOG_LEVEL INFO, got %s", cfg.General.LogLevel)
	}
	if !cfg.General.ConversationLog {
		t.Error("Expected default CONVERSATION_LOG true")
	}
}
