// Package random provides the cryptographic seed source of the simulation.
// Persisting a seed is what makes a probabilistic resolution replayable.
package random

import (
	"encoding/binary"
	"errors"
	"io"
)

// SeedGenerator draws the seeds persisted with missions and reports.
type SeedGenerator struct {
	random io.Reader
}

// NewSeedGenerator builds a generator over a source safe for concurrent use,
// such as crypto/rand.Reader.
func NewSeedGenerator(random io.Reader) *SeedGenerator {
	return &SeedGenerator{random: random}
}

// Seed returns a non-negative seed. Non-negative keeps it readable in the
// database and in reports without changing its entropy in practice.
func (g *SeedGenerator) Seed() (int64, error) {
	if g == nil || g.random == nil {
		return 0, errors.New("random: no seed source")
	}
	var raw [8]byte
	if _, err := io.ReadFull(g.random, raw[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(raw[:]) >> 1), nil
}
