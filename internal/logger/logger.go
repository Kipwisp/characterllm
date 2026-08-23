package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey string

const loggerKey ctxKey = "slog_logger"

// bracketHandler is a custom slog.Handler that formats logs with brackets.
type bracketHandler struct {
	level slog.Level
	out   io.Writer
	attrs []slog.Attr
}

func newBracketHandler(out io.Writer, level slog.Level) *bracketHandler {
	return &bracketHandler{
		level: level,
		out:   out,
	}
}

func (h *bracketHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *bracketHandler) Handle(ctx context.Context, r slog.Record) error {
	// Format Timestamp: [2026-08-17 01:58:40 PM]
	ts := r.Time.Format("2006-01-02 03:04:05 PM")

	// Format Level: INFO
	lvl := r.Level.String()
	lvl = strings.ToUpper(lvl)

	// Start building the log line
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s] %-5s %s", ts, lvl, r.Message)

	// Collect attributes from the handler itself (fixed attributes)
	var allAttrs []slog.Attr
	allAttrs = append(allAttrs, h.attrs...)

	// Collect attributes from the record (dynamic attributes)
	r.Attrs(func(a slog.Attr) bool {
		allAttrs = append(allAttrs, a)
		return true
	})

	// Append attributes in brackets if any exist: {key: val, key: val}
	if len(allAttrs) > 0 {
		sb.WriteString(" {")
		for i, attr := range allAttrs {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%s: %v", attr.Key, attr.Value.Any())
		}
		sb.WriteString("}")
	}

	fmt.Fprintf(h.out, "%s\n", sb.String())
	return nil
}

func (h *bracketHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &bracketHandler{
		level: h.level,
		out:   h.out,
		attrs: newAttrs,
	}
}

func (h *bracketHandler) WithGroup(name string) slog.Handler {
	// For simplicity, we flatten groups in the bracket handler
	return h
}

// Init initializes the global slog logger with the given level.
func Init(levelStr string) {
	var level slog.Level
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := newBracketHandler(os.Stdout, level)
	slog.SetDefault(slog.New(handler))
}

// WithRequestID creates a new logger that includes the provided request ID and other attributes.
func WithRequestID(reqID string, attrs ...any) *slog.Logger {
	args := make([]any, 0, 2+len(attrs))
	args = append(args, "request_id", reqID)
	args = append(args, attrs...)
	return slog.Default().With(args...)
}

// ToContext adds the provided logger to the context.
func ToContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext retrieves the logger from the context. If no logger is found, it returns the default logger.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
