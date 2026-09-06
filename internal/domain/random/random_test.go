package random

import (
	"bytes"
	"testing"
)

func TestSeededSourceIsReproducible(t *testing.T) {
	draws := func(seed uint64) []float64 {
		source := NewSeeded(seed)
		sequence := make([]float64, 0, 2000)
		for range 1000 {
			sequence = append(sequence, source.Float64(), float64(source.IntN(1000)))
		}
		return sequence
	}
	first, second, other := draws(42), draws(42), draws(43)
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("the same seed diverged at draw %d", index)
		}
	}
	same := true
	for index := range first {
		if first[index] != other[index] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("two different seeds produced the same sequence")
	}
}

func TestChanceSkipsCertainAndImpossibleDraws(t *testing.T) {
	empty := NewScript(nil, nil)
	if Chance(empty, 0) {
		t.Fatal("an impossible event happened")
	}
	if !Chance(empty, 1) {
		t.Fatal("a certain event did not happen")
	}
	scripted := NewScript(nil, []float64{.4, .6})
	if !Chance(scripted, .5) {
		t.Fatal("a draw of 0.4 must succeed at probability 0.5")
	}
	if Chance(scripted, .5) {
		t.Fatal("a draw of 0.6 must fail at probability 0.5")
	}
}

func TestScriptPanicsWhenExhausted(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an exhausted script must panic")
		}
	}()
	NewScript(nil, nil).Float64()
}

func TestSeedGeneratorProducesNonNegativeSeeds(t *testing.T) {
	generator := NewSeedGenerator(bytes.NewReader(bytes.Repeat([]byte{0xff}, 64)))
	seed, err := generator.Seed()
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	if seed < 0 {
		t.Fatalf("Seed() = %d, want a non-negative seed", seed)
	}
	empty := NewSeedGenerator(bytes.NewReader(nil))
	if _, err := empty.Seed(); err == nil {
		t.Fatal("Seed() accepted an exhausted source")
	}
}
