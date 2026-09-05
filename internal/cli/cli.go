// Package cli implements the local operational command surface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	storagesqlite "universeatwar/internal/storage/sqlite"
)

const defaultDatabasePath = "universe-at-war.db"

// Runner executes commands without terminating the process, which keeps the
// command surface testable.
type Runner struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

// Run executes one command and returns a process exit code.
func (r Runner) Run(ctx context.Context, arguments []string) int {
	if len(arguments) == 0 {
		r.usage()
		return 2
	}

	switch arguments[0] {
	case "version":
		fmt.Fprintf(r.Stdout, "Universe At War %s\n", r.Version)
		return 0
	case "migrate":
		return r.runMigrate(ctx, arguments[1:])
	case "doctor":
		return r.runDoctor(ctx, arguments[1:])
	case "help", "-h", "--help":
		r.usage()
		return 0
	default:
		fmt.Fprintf(r.Stderr, "unknown command %q\n", arguments[0])
		r.usage()
		return 2
	}
}

func (r Runner) runMigrate(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", defaultDatabasePath, "path to the SQLite database")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "migrate: unexpected positional arguments")
		return 2
	}

	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("migrate", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return r.commandError("migrate", err)
	}
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		return r.commandError("migrate", err)
	}
	fmt.Fprintf(r.Stdout, "migrations: ok (schema version %d)\n", version)
	return 0
}

func (r Runner) runDoctor(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", defaultDatabasePath, "path to the SQLite database")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "doctor: unexpected positional arguments")
		return 2
	}

	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("doctor", err)
	}
	defer database.Close()
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		return r.commandError("doctor", errors.New("database is not migrated"))
	}
	if err := database.CheckIntegrity(ctx); err != nil {
		return r.commandError("doctor", err)
	}
	fmt.Fprintf(r.Stdout, "schema version: %d\nintegrity: ok\n", version)
	return 0
}

func (r Runner) commandError(command string, err error) int {
	fmt.Fprintf(r.Stderr, "%s: %v\n", command, err)
	return 1
}

func (r Runner) usage() {
	fprintln(r.Stderr, `Usage: universe-at-war <command> [options]

Commands:
  migrate  apply embedded SQLite migrations
  doctor   verify database schema and integrity
  version  print application version`)
}

func fprintln(writer io.Writer, value string) {
	_, _ = fmt.Fprintln(writer, value)
}
