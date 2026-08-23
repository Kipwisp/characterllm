package audit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"characterllm/internal/llm"
	"characterllm/internal/logger"
)

// AuditLogger handles the writing of conversational audit trails to files.
type AuditLogger struct {
	logDir string
}

// NewAuditLogger creates a new AuditLogger with the specified directory.
func NewAuditLogger(logDir string) *AuditLogger {
	return &AuditLogger{
		logDir: logDir,
	}
}

// LogConversation writes the chat turn and model reasoning to a guild-specific log file for debugging.
func (a *AuditLogger) LogConversation(ctx context.Context, guildID string, charID string, prompt string, reasoning string, response string, history []llm.Message, latency time.Duration, reqID string) {
	if _, err := os.Stat(a.logDir); os.IsNotExist(err) {
		os.MkdirAll(a.logDir, 0755)
	}

	filename := fmt.Sprintf("%s/%s_%s.log", a.logDir, guildID, charID)
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.FromContext(ctx).Error("error opening log file", "error", err)
		return
	}
	defer f.Close()

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n================================================================================\n")
	fmt.Fprintf(&buf, "TIMESTAMP: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&buf, "REQUEST_ID: %s | GUILD: %s | CHAR: %s | LATENCY: %v\n", reqID, guildID, charID, latency)
	fmt.Fprintf(&buf, "--------------------------------------------------------------------------------\n")

	buf.WriteString("HISTORY:\n")
	for _, msg := range history {
		fmt.Fprintf(&buf, "  [%s] %s\n", msg.Role, msg.Content)
	}

	fmt.Fprintf(&buf, "\nPROMPT: %s\n", prompt)
	if reasoning != "" {
		fmt.Fprintf(&buf, "\nREASONING:\n%s\n", reasoning)
	}
	fmt.Fprintf(&buf, "\nRESPONSE:\n%s\n", response)
	fmt.Fprintf(&buf, "================================================================================\n")

	if _, err := f.Write(buf.Bytes()); err != nil {
		logger.FromContext(ctx).Error("error writing to log file", "error", err)
	}
}
