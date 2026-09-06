package phalanx

import (
	"errors"
	"testing"

	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
)

func TestRadiusGrowsWithTheSquareOfTheLevel(t *testing.T) {
	tests := map[int]int{0: -1, 1: 0, 2: 3, 3: 8, 4: 15, 5: 24}
	for level, want := range tests {
		if got := Radius(level); got != want {
			t.Fatalf("Radius(%d) = %d, want %d", level, got, want)
		}
	}
}

func TestInRangeFollowsTheTopology(t *testing.T) {
	topology := rules.Default().Topology
	from := universe.Coordinate{Galaxy: 1, System: 1, Position: 8}
	tests := []struct {
		name  string
		to    universe.Coordinate
		level int
		want  error
	}{
		{name: "its own system at level one", to: universe.Coordinate{Galaxy: 1, System: 1, Position: 3}, level: 1},
		{name: "the next system needs level two", to: universe.Coordinate{Galaxy: 1, System: 2, Position: 3}, level: 1, want: ErrOutOfRange},
		{name: "level two reaches three systems", to: universe.Coordinate{Galaxy: 1, System: 4, Position: 3}, level: 2},
		{name: "level two stops at four", to: universe.Coordinate{Galaxy: 1, System: 5, Position: 3}, level: 2, want: ErrOutOfRange},
		{name: "the galaxy wraps around", to: universe.Coordinate{Galaxy: 1, System: 99, Position: 3}, level: 2},
		{name: "another galaxy is never visible", to: universe.Coordinate{Galaxy: 2, System: 1, Position: 3}, level: 5, want: ErrOtherGalaxy},
		{name: "no phalanx at all", to: universe.Coordinate{Galaxy: 1, System: 1, Position: 3}, level: 0, want: ErrNoPhalanx},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := InRange(from, test.to, test.level, topology)
			if test.want == nil && err != nil {
				t.Fatalf("InRange() error = %v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("InRange() error = %v, want %v", err, test.want)
			}
		})
	}

	linear := topology
	linear.CircularSystems = false
	if err := InRange(from, universe.Coordinate{Galaxy: 1, System: 99, Position: 3}, 2, linear); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("a linear galaxy does not wrap: %v", err)
	}
}
