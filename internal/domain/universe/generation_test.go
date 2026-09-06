package universe

import (
	"testing"

	"universeatwar/internal/domain/random"
)

func TestGenerateIsDeterministicAndPositionDependent(t *testing.T) {
	at := Coordinate{Galaxy: 1, System: 2, Position: 4}
	first, err := Generate(at, 120, 260, 15, random.NewSeeded(42))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := Generate(at, 120, 260, 15, random.NewSeeded(42))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if first != second {
		t.Fatalf("the same seed generated two planets: %+v and %+v", first, second)
	}
	if first.TotalFields < 120 || first.TotalFields > 260 {
		t.Fatalf("fields = %d, outside the configured range", first.TotalFields)
	}
	if first.MaximumTemperature-first.MinimumTemperature != 40 {
		t.Fatalf("temperature spread = %d", first.MaximumTemperature-first.MinimumTemperature)
	}
}

func TestGenerateWarmsPositionsCloseToTheStar(t *testing.T) {
	tests := []struct {
		position int
		want     int
	}{
		{position: 1, want: 110},
		{position: 8, want: 40},
		{position: 15, want: -30},
	}
	for _, test := range tests {
		got, err := Generate(Coordinate{Galaxy: 1, System: 1, Position: test.position}, 100, 100, 15, random.NewSeeded(1))
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if got.MaximumTemperature != test.want {
			t.Fatalf("position %d: maximum temperature = %d, want %d", test.position, got.MaximumTemperature, test.want)
		}
	}
}

func TestGenerateRefusesImpossibleBounds(t *testing.T) {
	source := random.NewSeeded(1)
	if _, err := Generate(Coordinate{Galaxy: 1, System: 1, Position: 1}, 0, 100, 15, source); err == nil {
		t.Fatal("Generate() accepted a planet without fields")
	}
	if _, err := Generate(Coordinate{Galaxy: 1, System: 1, Position: 1}, 200, 100, 15, source); err == nil {
		t.Fatal("Generate() accepted an inverted range")
	}
	if _, err := Generate(Coordinate{Galaxy: 1, System: 1, Position: 1}, 100, 200, 15, nil); err == nil {
		t.Fatal("Generate() accepted a missing source")
	}
}
