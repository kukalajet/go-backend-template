package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

const testDatabaseURL = "postgresql://db.test/app"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", testDatabaseURL)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL.Value() != testDatabaseURL {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL.Value())
	}
	if cfg.APIHost != "127.0.0.1" || cfg.APIPort != 8000 {
		t.Fatalf("address = %s:%d", cfg.APIHost, cfg.APIPort)
	}
	if cfg.WorkerPollInterval != 5*time.Second || cfg.WorkerPassTimeout != 30*time.Second {
		t.Fatalf("worker = %v, %v", cfg.WorkerPollInterval, cfg.WorkerPassTimeout)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestLoadParsesLogLevel(t *testing.T) {
	t.Setenv("DATABASE_URL", testDatabaseURL)
	t.Setenv("LOG_LEVEL", "DEBUG")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
	}{
		{"port zero", "API_PORT", "0"},
		{"port too high", "API_PORT", "65536"},
		{"port not a number", "API_PORT", "abc"},
		{"zero interval", "WORKER_POLL_INTERVAL", "0s"},
		{"negative timeout", "WORKER_PASS_TIMEOUT", "-1s"},
		{"bad level", "LOG_LEVEL", "LOUD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", testDatabaseURL)
			t.Setenv(tt.key, tt.val)

			if _, err := Load(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "") // restores the original value after the test
	if err := os.Unsetenv("DATABASE_URL"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSecretNeverPrintsItsValue(t *testing.T) {
	const value = "hunter2"
	secret := Secret(value)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("msg", "url", secret)

	for name, out := range map[string]string{
		"String": secret.String(),
		"fmt":    fmt.Sprintf("%v %s", secret, secret),
		"struct": fmt.Sprintf("%+v", Config{DatabaseURL: secret}),
		"slog":   buf.String(),
	} {
		if strings.Contains(out, "hunter2") {
			t.Errorf("%s leaked the secret: %s", name, out)
		}
	}
	if secret.Value() != value {
		t.Fatalf("Value() = %q", secret.Value())
	}
}
