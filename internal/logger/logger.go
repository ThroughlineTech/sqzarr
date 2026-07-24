package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type contextKey struct{}

// New creates a structured JSON logger writing to stdout, and additionally
// tees to logFilePath if non-empty — this backs the in-app Logs tab, which
// reads and clears that file directly. Level defaults to Info; set
// SQZARR_LOG_LEVEL=debug|info|warn|error to override.
//
// If the log file can't be opened, New still returns a working stdout-only
// logger along with the error, so callers can log a warning and continue.
func New(logFilePath string) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: levelFromEnv()}

	if logFilePath == "" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts)), nil
	}

	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts)), err
	}
	return slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stdout, f), opts)), nil
}

func levelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("SQZARR_LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithContext returns a logger stored in the context.
func WithContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// FromContext retrieves the logger from context, falling back to the default.
func FromContext(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}
