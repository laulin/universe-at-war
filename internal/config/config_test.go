package config

import "testing"

func TestLoadUsesSafeDefaults(t *testing.T) {
	configuration, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.DatabasePath != "universe-at-war.db" {
		t.Errorf("DatabasePath = %q, want universe-at-war.db", configuration.DatabasePath)
	}
	if configuration.ListenAddress != "127.0.0.1:8080" {
		t.Errorf("ListenAddress = %q, want loopback default", configuration.ListenAddress)
	}
	if configuration.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", configuration.LogLevel)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	values := map[string]string{
		"UAW_DATABASE":  "data/campaign.db",
		"UAW_LISTEN":    "[::1]:9090",
		"UAW_LOG_LEVEL": "debug",
	}
	configuration, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.DatabasePath != values["UAW_DATABASE"] {
		t.Errorf("DatabasePath = %q", configuration.DatabasePath)
	}
	if configuration.ListenAddress != values["UAW_LISTEN"] {
		t.Errorf("ListenAddress = %q", configuration.ListenAddress)
	}
	if configuration.LogLevel != values["UAW_LOG_LEVEL"] {
		t.Errorf("LogLevel = %q", configuration.LogLevel)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "empty database", values: map[string]string{"UAW_DATABASE": "   "}},
		{name: "invalid listen address", values: map[string]string{"UAW_LISTEN": "not-an-address"}},
		{name: "invalid log level", values: map[string]string{"UAW_LOG_LEVEL": "verbose"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(func(key string) string {
				value, exists := tt.values[key]
				if exists {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
		})
	}
}
