// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
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

	// Password is the shared password protecting the app. Required outside
	// development mode (see Validate).
	Password string
	// SessionTTL is how long a login lasts.
	SessionTTL time.Duration
	// CookieSecure sets the Secure flag on the session cookie. Disable only
	// when serving over plain HTTP on a non-localhost address.
	CookieSecure bool
	// MaxUploadBytes bounds a single uploaded photograph.
	MaxUploadBytes int64

	// MaxVariations caps the variations in one generation.
	MaxVariations int
	// Workers is how many generations run at once.
	Workers int
	// StepTimeout bounds one provider call.
	StepTimeout time.Duration
	// FakeProvider enables the offline test provider.
	FakeProvider bool
	// FakeDelay is how long each fake provider job takes.
	FakeDelay time.Duration

	// FASHN is the virtual try-on provider; enabled when the key is set.
	FASHN FASHNConfig
}

// FASHNConfig configures the FASHN provider.
type FASHNConfig struct {
	APIKey string
	// Model, Resolution and Mode are validated by the adapter.
	Model      string
	Resolution string
	Mode       string
	// CreditPriceMicros is the price of one credit in micro-USD.
	CreditPriceMicros int64
	BaseURL           string
}

// String never prints the key.
func (f FASHNConfig) String() string {
	return fmt.Sprintf("{model:%s resolution:%s mode:%s key-set:%v}", f.Model, f.Resolution, f.Mode, f.APIKey != "")
}

// DevPassword is used in development mode when no password is configured.
const DevPassword = "dev"

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

	c.Password = getenv("AIFS_PASSWORD")
	if file := getenv("AIFS_PASSWORD_FILE"); file != "" {
		if c.Password != "" {
			return Config{}, errors.New("set AIFS_PASSWORD or AIFS_PASSWORD_FILE, not both")
		}
		b, err := os.ReadFile(file) //nolint:gosec // path comes from the operator
		if err != nil {
			return Config{}, fmt.Errorf("AIFS_PASSWORD_FILE: %w", err)
		}
		c.Password = strings.TrimRight(string(b), "\r\n")
	}
	if c.Password == "" && c.Dev() {
		c.Password = DevPassword
	}

	ttl, err := time.ParseDuration(valueOr(getenv("AIFS_SESSION_TTL"), "720h"))
	if err != nil || ttl < time.Minute {
		return Config{}, fmt.Errorf("AIFS_SESSION_TTL must be a duration of at least 1m, got %q", getenv("AIFS_SESSION_TTL"))
	}
	c.SessionTTL = ttl

	c.CookieSecure, err = strconv.ParseBool(valueOr(getenv("AIFS_COOKIE_SECURE"), "true"))
	if err != nil {
		return Config{}, fmt.Errorf("AIFS_COOKIE_SECURE: %w", err)
	}

	mb, err := strconv.Atoi(valueOr(getenv("AIFS_MAX_UPLOAD_MB"), "30"))
	if err != nil || mb < 1 || mb > 200 {
		return Config{}, fmt.Errorf("AIFS_MAX_UPLOAD_MB must be between 1 and 200, got %q", getenv("AIFS_MAX_UPLOAD_MB"))
	}
	c.MaxUploadBytes = int64(mb) << 20

	if c.MaxVariations, err = intEnv(getenv, "AIFS_MAX_VARIATIONS", 4, 1, 20); err != nil {
		return Config{}, err
	}
	if c.Workers, err = intEnv(getenv, "AIFS_WORKERS", 4, 1, 64); err != nil {
		return Config{}, err
	}
	if c.StepTimeout, err = durationEnv(getenv, "AIFS_STEP_TIMEOUT", 10*time.Minute, time.Second); err != nil {
		return Config{}, err
	}
	fakeDefault := "false"
	if c.Dev() {
		fakeDefault = "true"
	}
	if c.FakeProvider, err = strconv.ParseBool(valueOr(getenv("AIFS_FAKE_PROVIDER"), fakeDefault)); err != nil {
		return Config{}, fmt.Errorf("AIFS_FAKE_PROVIDER: %w", err)
	}
	if c.FakeDelay, err = durationEnv(getenv, "AIFS_FAKE_DELAY", 2*time.Second, 0); err != nil {
		return Config{}, err
	}

	if c.FASHN.APIKey, err = secretEnv(getenv, "AIFS_FASHN_API_KEY"); err != nil {
		return Config{}, err
	}
	c.FASHN.Model = getenv("AIFS_FASHN_MODEL")
	c.FASHN.Resolution = getenv("AIFS_FASHN_RESOLUTION")
	c.FASHN.Mode = getenv("AIFS_FASHN_MODE")
	c.FASHN.BaseURL = getenv("AIFS_FASHN_BASE_URL")
	price, err := strconv.ParseFloat(valueOr(getenv("AIFS_FASHN_CREDIT_PRICE_USD"), "0.075"), 64)
	if err != nil || price < 0 {
		return Config{}, fmt.Errorf("AIFS_FASHN_CREDIT_PRICE_USD must be a non-negative number, got %q", getenv("AIFS_FASHN_CREDIT_PRICE_USD"))
	}
	c.FASHN.CreditPriceMicros = int64(price*1_000_000 + 0.5)

	return c, nil
}

// secretEnv reads KEY, or the file named by KEY_FILE (Docker secrets).
func secretEnv(getenv func(string) string, key string) (string, error) {
	v := getenv(key)
	file := getenv(key + "_FILE")
	if file == "" {
		return v, nil
	}
	if v != "" {
		return "", fmt.Errorf("set %s or %s_FILE, not both", key, key)
	}
	b, err := os.ReadFile(file) //nolint:gosec // path comes from the operator
	if err != nil {
		return "", fmt.Errorf("%s_FILE: %w", key, err)
	}
	return strings.TrimSpace(string(b)), nil
}

func intEnv(getenv func(string) string, key string, def, lo, hi int) (int, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be a whole number between %d and %d, got %q", key, lo, hi, v)
	}
	return n, nil
}

func durationEnv(getenv func(string) string, key string, def, minimum time.Duration) (time.Duration, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < minimum {
		return 0, fmt.Errorf("%s must be a duration of at least %s, got %q", key, minimum, v)
	}
	return d, nil
}

// MinPasswordLength is enforced in production.
const MinPasswordLength = 12

// Validate checks settings that only the server (not helper commands such as
// healthcheck) needs.
func (c Config) Validate() error {
	if c.Dev() {
		return nil
	}
	if c.Password == "" {
		return errors.New("AIFS_PASSWORD (or AIFS_PASSWORD_FILE) is required outside development mode")
	}
	if len(c.Password) < MinPasswordLength {
		return fmt.Errorf("AIFS_PASSWORD must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// FromEnv loads configuration from the process environment.
func FromEnv() (Config, error) { return Load(os.Getenv) }

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
