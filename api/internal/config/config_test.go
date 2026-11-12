package config

import (
	"log/slog"
	"testing"
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
	if c.Addr != "127.0.0.1:9000" || c.DataDir != "/data" || !c.Dev() || c.LogLevel != slog.LevelDebug {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"env":   {"AIFS_ENV": "staging"},
		"level": {"AIFS_LOG_LEVEL": "loud"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(m)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
