// Package config loads and validates process configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	defaultDatabasePath  = "universe-at-war.db"
	defaultListenAddress = "127.0.0.1:8080"
	defaultLogLevel      = "info"
)

// Config contains infrastructure settings. Gameplay rules are persisted in a
// versioned ruleset and deliberately do not belong here.
type Config struct {
	DatabasePath  string
	ListenAddress string
	LogLevel      string
}

// Load reads configuration through getenv, allowing deterministic tests.
func Load(getenv func(string) string) (Config, error) {
	configuration := Config{
		DatabasePath:  valueOrDefault(getenv("UAW_DATABASE"), defaultDatabasePath),
		ListenAddress: valueOrDefault(getenv("UAW_LISTEN"), defaultListenAddress),
		LogLevel:      strings.ToLower(valueOrDefault(getenv("UAW_LOG_LEVEL"), defaultLogLevel)),
	}

	if strings.TrimSpace(configuration.DatabasePath) == "" {
		return Config{}, errors.New("config: UAW_DATABASE cannot be blank")
	}
	if _, _, err := net.SplitHostPort(configuration.ListenAddress); err != nil {
		return Config{}, fmt.Errorf("config: invalid UAW_LISTEN: %w", err)
	}
	switch configuration.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("config: invalid UAW_LOG_LEVEL %q", configuration.LogLevel)
	}
	return configuration, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
