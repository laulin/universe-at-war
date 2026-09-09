package ai

import (
	"strings"
	"testing"
)

func TestEveryArtificialNameIsUsableAndItsOwn(t *testing.T) {
	const population = 10000
	seen := make(map[string]int, population)
	for index := range population {
		name := Name(index)
		if length := len([]rune(name)); length < 3 || length > 32 {
			t.Fatalf("Name(%d) = %q, %d characters long", index, name, length)
		}
		if strings.TrimSpace(name) != name {
			t.Fatalf("Name(%d) = %q, padded with spaces", index, name)
		}
		// An account is unique on its lowered form, so two names that differ
		// only by their case would collide where nothing could be done about it.
		lowered := strings.ToLower(name)
		if first, taken := seen[lowered]; taken {
			t.Fatalf("Name(%d) and Name(%d) both give %q", first, index, name)
		}
		seen[lowered] = index
	}
}

func TestAnArtificialNameIsAskedForBeforeTheFirst(t *testing.T) {
	if Name(-1) != Name(0) {
		t.Fatalf("Name(-1) = %q, want %q", Name(-1), Name(0))
	}
}
