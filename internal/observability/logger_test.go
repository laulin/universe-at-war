package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLoggerUsesJSONAndConfiguredLevel(t *testing.T) {
	var output bytes.Buffer
	logger, err := NewLogger(&output, "info")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	logger.Debug("hidden")
	logger.Info("started", slog.String("component", "test"))
	got := output.String()
	if strings.Contains(got, "hidden") {
		t.Fatalf("logger output contains debug entry: %s", got)
	}
	if !strings.Contains(got, `"msg":"started"`) || !strings.Contains(got, `"component":"test"`) {
		t.Fatalf("logger output = %q, want structured fields", got)
	}
}

func TestNewLoggerRejectsUnknownLevel(t *testing.T) {
	if _, err := NewLogger(&bytes.Buffer{}, "verbose"); err == nil {
		t.Fatal("NewLogger() error = nil, want invalid level error")
	}
}
