package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// withWriteTx runs one command inside the serialized write transaction and
// commits only when it succeeds. Business errors are returned unchanged so that
// callers keep using errors.Is on the domain sentinels.
func withWriteTx(ctx context.Context, database *sql.DB, operation string, command func(*sql.Tx) error) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin: %w", operation, err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := command(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", operation, err)
	}
	return nil
}
