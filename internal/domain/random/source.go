package random

import (
	"math/rand/v2"
)

// stream is the second PCG word: a fixed value keeps a seed the only input of a
// sequence, which is what makes a resolution replayable from its stored seed.
const stream uint64 = 0xda3e39cb94b95bdb

// Source is the controlled randomness the domain is allowed to consume. No
// domain function ever reaches for a global generator.
type Source interface {
	Float64() float64
	IntN(n int) int
}

// NewSeeded builds the deterministic source of one resolution.
func NewSeeded(seed uint64) Source {
	return rand.New(rand.NewPCG(seed, stream))
}

// Chance reports whether an event of the given probability happens. A
// probability that leaves no doubt consumes no draw, so the sequence of a
// resolution never depends on an impossible or certain event.
func Chance(source Source, probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	return source.Float64() < probability
}

// Script is a scripted source for the tests that walk through a resolution by
// hand. It panics when a draw is missing, so a test cannot silently drift.
type Script struct {
	integers []int
	floats   []float64
}

// NewScript builds a source that returns exactly the given draws.
func NewScript(integers []int, floats []float64) *Script {
	return &Script{integers: integers, floats: floats}
}

func (s *Script) IntN(n int) int {
	if len(s.integers) == 0 {
		panic("random: scripted source ran out of integers")
	}
	value := s.integers[0]
	s.integers = s.integers[1:]
	if value < 0 || value >= n {
		panic("random: scripted integer is out of range")
	}
	return value
}

func (s *Script) Float64() float64 {
	if len(s.floats) == 0 {
		panic("random: scripted source ran out of floats")
	}
	value := s.floats[0]
	s.floats = s.floats[1:]
	return value
}

// Remaining reports how many draws the script still holds, which lets a test
// assert that a resolution consumed exactly what it was expected to.
func (s *Script) Remaining() (int, int) {
	return len(s.integers), len(s.floats)
}
