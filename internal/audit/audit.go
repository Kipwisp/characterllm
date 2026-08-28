package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"characterllm/internal/logger"
)

// Kind classifies the type of LLM exchange recorded in the audit trail.
type Kind string

const (
	KindChat         Kind = "chat"
	KindAmbient      Kind = "ambient"
	KindAmbientReply Kind = "ambient_reply"
	KindAnalysis     Kind = "analysis"
	KindSynthesis    Kind = "synthesis"
	KindCompaction   Kind = "compaction"
	KindEdit         Kind = "edit"
)

// Turn is one LLM exchange to append to the audit trail.
type Turn struct {
	Kind      Kind
	Model     string
	Latency   time.Duration
	System    string // full system prompt for chat turns; recorded on first sight and whenever it changes
	Prompt    string
	Reasoning string
	Response  string
}

// AuditLogger writes per-guild, per-character audit files as append-only transcripts.
type AuditLogger struct {
	logDir  string
	enabled bool

	mu             sync.Mutex
	lastSystemHash map[string]string // key: guildID_charID_threadID
}

// NewAuditLogger creates a new AuditLogger that writes to logDir. When
// enabled is false, Log is a no-op.
func NewAuditLogger(logDir string, enabled bool) *AuditLogger {
	return &AuditLogger{
		logDir:         logDir,
		enabled:        enabled,
		lastSystemHash: make(map[string]string),
	}
}

// Log appends one LLM exchange to the conversation audit file for the guild.
func (a *AuditLogger) Log(ctx context.Context, guildID, threadID, charID, reqID string, t Turn) {
	if !a.enabled {
		return
	}
	if _, err := os.Stat(a.logDir); os.IsNotExist(err) {
		os.MkdirAll(a.logDir, 0755)
	}

	filename := fmt.Sprintf("%s/%s_%s.log", a.logDir, guildID, charID)
	if threadID != "" {
		filename = fmt.Sprintf("%s/%s_%s_%s.log", a.logDir, guildID, charID, threadID)
	}
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.FromContext(ctx).Error("error opening audit log file", "error", err)
		return
	}
	defer f.Close()

	header := fmt.Sprintf("%s | kind=%s | req=%s | %s",
		time.Now().Format(time.RFC3339), t.Kind, reqID, t.Latency)
	if t.Model != "" {
		header += fmt.Sprintf(" | model=%s", t.Model)
	}
	if threadID != "" {
		header += fmt.Sprintf(" | thread=%s", threadID)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n─── %s ───\n", header)
	buf.WriteString(a.systemPromptBlock(guildID, threadID, charID, t.System))
	writeSection(&buf, "prompt", t.Prompt)
	writeSection(&buf, "reasoning", t.Reasoning)
	writeSection(&buf, "response", t.Response)

	if _, err := f.Write(buf.Bytes()); err != nil {
		logger.FromContext(ctx).Error("error writing to audit log file", "error", err)
	}
}

// systemPromptBlock returns a "system prompt" section when the prompt is
// new for the conversation or has changed since the last recorded version,
// and memoizes its hash so unchanged prompts are not re-logged.
func (a *AuditLogger) systemPromptBlock(guildID, threadID, charID, system string) string {
	if system == "" {
		return ""
	}
	key := fmt.Sprintf("%s_%s_%s", guildID, charID, threadID)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(system)))

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastSystemHash[key] == hash {
		return ""
	}
	a.lastSystemHash[key] = hash

	var buf bytes.Buffer
	writeSection(&buf, "system prompt", system)
	return buf.String()
}

// writeSection writes a labeled block with the content indented, skipping empty content.
func writeSection(buf *bytes.Buffer, name, content string) {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return
	}
	fmt.Fprintf(buf, "%s:\n%s\n", name, indent(content))
}

// indent prefixes every line of s with four spaces.
func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
