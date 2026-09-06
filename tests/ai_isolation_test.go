package tests

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestArtificialPlayersReachTheWorldOnlyThroughUseCases is a structural proof:
// the packages that decide for an artificial player cannot see a database, a
// repository or the truth of another player, because they do not import any.
func TestArtificialPlayersReachTheWorldOnlyThroughUseCases(t *testing.T) {
	forbidden := []string{
		"universeatwar/internal/storage",
		"universeatwar/internal/auth",
		"universeatwar/internal/web",
		"database/sql",
		"modernc.org/sqlite",
	}
	for _, directory := range []string{"../internal/ai", "../internal/domain/ai"} {
		for path, imports := range importsOf(t, directory) {
			for _, imported := range imports {
				for _, banned := range forbidden {
					if imported == banned || strings.HasPrefix(imported, banned+"/") {
						t.Fatalf("%s imports %s: an artificial player must go through the use cases", path, imported)
					}
				}
			}
		}
	}
	// The rules of an artificial player stay pure: no application layer either.
	for path, imports := range importsOf(t, "../internal/domain/ai") {
		for _, imported := range imports {
			if strings.HasPrefix(imported, "universeatwar/internal/app") {
				t.Fatalf("%s imports %s: the rules must not know the use cases", path, imported)
			}
		}
	}
}

// importsOf reads the imports of every Go file of a directory, tests included:
// a back door opened for a test would still be a back door.
func importsOf(t *testing.T, directory string) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		var imports []string
		for _, specification := range file.Imports {
			value, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			imports = append(imports, value)
		}
		found[path] = imports
	}
	if len(found) == 0 {
		t.Fatalf("%s holds no Go file: the isolation proof would be empty", directory)
	}
	return found
}
