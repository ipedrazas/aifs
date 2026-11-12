// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Environment names.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Config is the complete runtime configuration. It is populated only from
// environment variables.
type Config struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// DataDir holds the SQLite database and the blob store.
	DataDir string
	// Env is either "development" or "production".
	Env string
	// LogLevel is the minimum slog level.
	LogLevel slog.Level
}

// Dev reports whether the server runs in development mode.
func (c Config) Dev() bool { return c.Env == EnvDevelopment }

// Load reads configuration from the environment using getenv (os.Getenv in
// production, a stub in tests).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:    valueOr(getenv("AIFS_ADDR"), ":8080"),
		DataDir: valueOr(getenv("AIFS_DATA_DIR"), "./data"),
		Env:     strings.ToLower(valueOr(getenv("AIFS_ENV"), EnvProduction)),
	}

	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		return Config{}, fmt.Errorf("AIFS_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.Env)
	}

	level := valueOr(getenv("AIFS_LOG_LEVEL"), "info")
	if err := c.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("AIFS_LOG_LEVEL: %w", err)
	}

	return c, nil
}

// FromEnv loads configuration from the process environment.
func FromEnv() (Config, error) { return Load(os.Getenv) }

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
