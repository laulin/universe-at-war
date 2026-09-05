package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestWithWriteTxRollsBackOnErrorAndCommitsOnSuccess(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "tx.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	sentinel := errors.New("business rule refused the command")
	err = withWriteTx(ctx, database.Write(), "test", func(transaction *sql.Tx) error {
		if _, execErr := transaction.ExecContext(ctx, insertProbe); execErr != nil {
			return execErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("withWriteTx() error = %v, want the business error unchanged", err)
	}
	assertProbeCount(t, ctx, database, 0)

	if err := withWriteTx(ctx, database.Write(), "test", func(transaction *sql.Tx) error {
		_, execErr := transaction.ExecContext(ctx, insertProbe)
		return execErr
	}); err != nil {
		t.Fatalf("withWriteTx() error = %v", err)
	}
	assertProbeCount(t, ctx, database, 1)
}

const insertProbe = `
	INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, created_at)
	VALUES ('1', 'probe', 'probe-1', 'hash', '2042-09-10T11:12:13Z')
`

func assertProbeCount(t *testing.T, ctx context.Context, database *Database, want int) {
	t.Helper()
	var count int
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM idempotency_keys").Scan(&count); err != nil {
		t.Fatalf("count idempotency keys: %v", err)
	}
	if count != want {
		t.Fatalf("idempotency keys = %d, want %d", count, want)
	}
}
