// Package config loads the settings shared by the api and worker binaries.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"
)

const redacted = "[REDACTED]"

// Secret is a string that never prints its value.
type Secret string

// String returns a redacted placeholder.
func (s Secret) String() string { return redacted }

// LogValue returns a redacted placeholder.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// Value returns the secret.
func (s Secret) Value() string { return string(s) }

// Config holds the environment settings.
type Config struct {
	DatabaseURL        Secret        `env:"DATABASE_URL,required"`
	APIHost            string        `env:"API_HOST" envDefault:"127.0.0.1"`
	APIPort            int           `env:"API_PORT" envDefault:"8000"`
	WorkerPollInterval time.Duration `env:"WORKER_POLL_INTERVAL" envDefault:"5s"`
	WorkerPassTimeout  time.Duration `env:"WORKER_PASS_TIMEOUT" envDefault:"30s"`
	LogLevel           slog.Level    `env:"LOG_LEVEL" envDefault:"INFO"`
}

// Load parses the environment and validates the result.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate checks the ranges that parsing can't.
func (c Config) Validate() error {
	if c.APIPort < 1 || c.APIPort > 65535 {
		return errors.New("API_PORT must be between 1 and 65535")
	}
	if c.WorkerPollInterval <= 0 {
		return errors.New("WORKER_POLL_INTERVAL must be positive")
	}
	if c.WorkerPassTimeout <= 0 {
		return errors.New("WORKER_PASS_TIMEOUT must be positive")
	}

	return nil
}
