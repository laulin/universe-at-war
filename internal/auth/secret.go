package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// SecretGenerator creates copyable secrets with a CSPRNG supplied by the
// executable.
type SecretGenerator struct {
	random io.Reader
	bytes  int
}

// NewSecretGenerator creates a URL-safe secret generator.
func NewSecretGenerator(random io.Reader, bytes int) *SecretGenerator {
	return &SecretGenerator{random: random, bytes: bytes}
}

// Generate returns an unpadded base64url secret containing at least 128 bits.
func (g *SecretGenerator) Generate() (string, error) {
	if g.bytes < 16 {
		return "", errors.New("auth: secret requires at least 16 random bytes")
	}
	if g.random == nil {
		return "", errors.New("auth: random source is nil")
	}
	buffer := make([]byte, g.bytes)
	if _, err := io.ReadFull(g.random, buffer); err != nil {
		return "", fmt.Errorf("auth: generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
