// Package observability provides local structured logging and, later, internal
// metrics without requiring an external service.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// NewLogger creates the JSON logger used by commands and the server.
func NewLogger(output io.Writer, level string) (*slog.Logger, error) {
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		return nil, fmt.Errorf("observability: invalid log level %q", level)
	}

	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slogLevel})
	return slog.New(handler), nil
}
