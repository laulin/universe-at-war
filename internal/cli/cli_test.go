package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "1.2.3"}

	if code := runner.Run(context.Background(), []string{"version"}); code != 0 {
		t.Fatalf("Run(version) code = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "Universe At War 1.2.3" {
		t.Fatalf("version output = %q", got)
	}
}

func TestMigrateThenDoctor(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "campaign.db")
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "test"}

	if code := runner.Run(context.Background(), []string{"migrate", "--database", databasePath}); code != 0 {
		t.Fatalf("Run(migrate) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "schema version 1") {
		t.Fatalf("migrate output = %q, want schema version", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"doctor", "--database", databasePath}); code != 0 {
		t.Fatalf("Run(doctor) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "integrity: ok") {
		t.Fatalf("doctor output = %q, want integrity status", stdout.String())
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "test"}

	if code := runner.Run(context.Background(), []string{"unknown"}); code != 2 {
		t.Fatalf("Run(unknown) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q, want unknown command error", stderr.String())
	}
}

func TestMissingCommandPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "test"}

	if code := runner.Run(context.Background(), nil); code != 2 {
		t.Fatalf("Run(nil) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}
