package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const argon2Version = 19

// Parameters defines the Argon2id cost and output policy encoded with every
// credential.
type Parameters struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParameters returns the production password hashing policy.
func DefaultParameters() Parameters {
	return Parameters{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// PasswordHasher hashes and verifies PHC-encoded Argon2id credentials.
type PasswordHasher struct {
	parameters Parameters
	random     io.Reader
}

// NewPasswordHasher creates a hasher with an explicit policy and random source.
// NewPasswordHasher builds a hasher. The random source draws one salt per hash
// and must therefore be safe for concurrent use, like crypto/rand.Reader.
func NewPasswordHasher(parameters Parameters, random io.Reader) *PasswordHasher {
	return &PasswordHasher{parameters: parameters, random: random}
}

// Hash creates a new salt and returns a PHC-encoded Argon2id credential.
func (h *PasswordHasher) Hash(password string) (string, error) {
	if err := h.parameters.validate(); err != nil {
		return "", err
	}
	if h.random == nil {
		return "", errors.New("auth: random source is nil")
	}

	salt := make([]byte, h.parameters.SaltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("auth: generate password salt: %w", err)
	}
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		h.parameters.Iterations,
		h.parameters.MemoryKiB,
		h.parameters.Parallelism,
		h.parameters.KeyLength,
	)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version,
		h.parameters.MemoryKiB,
		h.parameters.Iterations,
		h.parameters.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// Verify checks a password in constant time and reports whether the credential
// should be upgraded to the current policy after a successful login.
func (h *PasswordHasher) Verify(encoded, password string) (match, needsRehash bool, err error) {
	parsed, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, false, err
	}
	actual := argon2.IDKey(
		[]byte(password),
		salt,
		parsed.Iterations,
		parsed.MemoryKiB,
		parsed.Parallelism,
		parsed.KeyLength,
	)
	match = subtle.ConstantTimeCompare(actual, expected) == 1
	return match, parsed != h.parameters, nil
}

func parsePasswordHash(encoded string) (Parameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Parameters{}, nil, nil, errors.New("auth: invalid Argon2id encoding")
	}

	var parameters Parameters
	values := strings.Split(parts[3], ",")
	if len(values) != 3 {
		return Parameters{}, nil, nil, errors.New("auth: invalid Argon2id parameters")
	}
	memory, err := parseUintParameter(values[0], "m=", 32)
	if err != nil {
		return Parameters{}, nil, nil, err
	}
	iterations, err := parseUintParameter(values[1], "t=", 32)
	if err != nil {
		return Parameters{}, nil, nil, err
	}
	parallelism, err := parseUintParameter(values[2], "p=", 8)
	if err != nil {
		return Parameters{}, nil, nil, err
	}
	parameters.MemoryKiB = uint32(memory)
	parameters.Iterations = uint32(iterations)
	parameters.Parallelism = uint8(parallelism)

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Parameters{}, nil, nil, errors.New("auth: invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Parameters{}, nil, nil, errors.New("auth: invalid Argon2id hash")
	}
	parameters.SaltLength = uint32(len(salt))
	parameters.KeyLength = uint32(len(expected))
	if err := parameters.validate(); err != nil {
		return Parameters{}, nil, nil, err
	}
	return parameters, salt, expected, nil
}

func parseUintParameter(value, prefix string, bits int) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, errors.New("auth: invalid Argon2id parameter")
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, bits)
	if err != nil {
		return 0, errors.New("auth: invalid Argon2id parameter")
	}
	return parsed, nil
}

func (p Parameters) validate() error {
	if p.MemoryKiB < 8*1024 || p.MemoryKiB > 1024*1024 {
		return errors.New("auth: Argon2id memory must be between 8192 and 1048576 KiB")
	}
	if p.Iterations == 0 || p.Iterations > 20 {
		return errors.New("auth: Argon2id iterations must be between 1 and 20")
	}
	if p.Parallelism == 0 {
		return errors.New("auth: Argon2id parallelism must be positive")
	}
	if p.SaltLength < 16 || p.SaltLength > 64 {
		return errors.New("auth: Argon2id salt length must be between 16 and 64 bytes")
	}
	if p.KeyLength < 16 || p.KeyLength > 64 {
		return errors.New("auth: Argon2id key length must be between 16 and 64 bytes")
	}
	return nil
}
