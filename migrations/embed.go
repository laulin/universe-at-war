// Package migrations exposes the versioned SQL schema embedded in the binary.
package migrations

import "embed"

// Files contains every published schema migration.
//
//go:embed *.sql
var Files embed.FS
