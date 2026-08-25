package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestBracketHandler(t *testing.T) {
	var buf bytes.Buffer
	level := slog.LevelInfo
	handler := newBracketHandler(&buf, level)
	logger := slog.New(handler)

	t.Run("Simple log", func(t *testing.T) {
		buf.Reset()
		logger.Info("Test message")
		got := buf.String()
		if !strings.Contains(got, "INFO Test message") {
			t.Errorf("Unexpected log format: %q", got)
		}
		if !strings.HasPrefix(got, "[") {
			t.Errorf("Log should start with timestamp bracket: %q", got)
		}
	})

	t.Run("Log with attributes", func(t *testing.T) {
		buf.Reset()
		logger.Info("User login", "user_id", 123, "ip", "1.2.3.4")
		got := buf.String()
		if !strings.Contains(got, "INFO User login {user_id: 123, ip: 1.2.3.4}") {
			t.Errorf("Unexpected log format with attributes: %q", got)
		}
	})

	t.Run("Log level filtering", func(t *testing.T) {
		buf.Reset()
		logger.Debug("Debug message")
		if buf.Len() > 0 {
			t.Errorf("Debug message should have been filtered out, got: %q", buf.String())
		}
	})

	t.Run("Log with fixed attributes (WithAttrs)", func(t *testing.T) {
		buf.Reset()
		hWithAttrs := handler.WithAttrs([]slog.Attr{slog.String("component", "auth")})
		loggerWithAttrs := slog.New(hWithAttrs)
		loggerWithAttrs.Info("Auth success")
		got := buf.String()
		if !strings.Contains(got, "INFO Auth success {component: auth}") {
			t.Errorf("Unexpected log format with fixed attributes: %q", got)
		}
	})
}

func TestContextLogger(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	ctx = ToContext(ctx, logger)
	retrieved := FromContext(ctx)

	if retrieved != logger {
		t.Errorf("Retrieved logger from context does not match the one added")
	}

	retrievedDefault := FromContext(context.Background())
	if retrievedDefault == nil {
		t.Error("FromContext should return default logger if none found in context")
	}
}

func TestWithRequestID(t *testing.T) {
	// We can't easily test the output of slog.Default() without replacing the handler
	// but we can verify it doesn't panic and returns a logger.
	reqID := "req-123"
	logger := WithRequestID(reqID, "extra", "val")
	if logger == nil {
		t.Error("WithRequestID returned nil")
	}
}
