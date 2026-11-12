package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.DataDir != "./data" || c.Env != EnvProduction || c.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.Dev() {
		t.Fatal("default must not be development mode")
	}
	if c.MaxVariations != 4 || c.Workers != 4 || c.FakeProvider || c.StepTimeout != 10*time.Minute {
		t.Fatalf("unexpected generation defaults: %+v", c)
	}
	if c.SessionTTL != 720*time.Hour || !c.CookieSecure || c.Password != "" || c.MaxUploadBytes != 30<<20 {
		t.Fatalf("unexpected auth defaults: %+v", c)
	}
}

func TestValidatePassword(t *testing.T) {
	prod, _ := Load(env(nil))
	if err := prod.Validate(); err == nil {
		t.Fatal("production without a password must not validate")
	}
	short, _ := Load(env(map[string]string{"AIFS_PASSWORD": "short"}))
	if err := short.Validate(); err == nil {
		t.Fatal("short password must not validate")
	}
	ok, _ := Load(env(map[string]string{"AIFS_PASSWORD": "correct horse battery"}))
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	dev, _ := Load(env(map[string]string{"AIFS_ENV": "development"}))
	if err := dev.Validate(); err != nil || dev.Password != DevPassword {
		t.Fatalf("development should default the password: %v %q", err, dev.Password)
	}
}

func TestPasswordFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(f, []byte("from a secret file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(env(map[string]string{"AIFS_PASSWORD_FILE": f}))
	if err != nil || c.Password != "from a secret file" {
		t.Fatalf("got %q, %v", c.Password, err)
	}
	if _, err := Load(env(map[string]string{"AIFS_PASSWORD_FILE": f, "AIFS_PASSWORD": "x"})); err == nil {
		t.Fatal("both set must be an error")
	}
}

func TestLoadOverrides(t *testing.T) {
	c, err := Load(env(map[string]string{
		"AIFS_ADDR":      "127.0.0.1:9000",
		"AIFS_DATA_DIR":  "/data",
		"AIFS_ENV":       "Development",
		"AIFS_LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:9000" || c.DataDir != "/data" || !c.Dev() || c.LogLevel != slog.LevelDebug || !c.FakeProvider {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"env":    {"AIFS_ENV": "staging"},
		"level":  {"AIFS_LOG_LEVEL": "loud"},
		"ttl":    {"AIFS_SESSION_TTL": "5s"},
		"secure": {"AIFS_COOKIE_SECURE": "maybe"},
		"upload": {"AIFS_MAX_UPLOAD_MB": "0"},
		"vars":   {"AIFS_MAX_VARIATIONS": "50"},
		"fake":   {"AIFS_FAKE_PROVIDER": "maybe"},
		"delay":  {"AIFS_FAKE_DELAY": "soon"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(m)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
