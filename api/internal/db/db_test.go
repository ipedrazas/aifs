package db

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	d, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := d.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 < 1 {
		t.Fatalf("expected at least one migration applied, got %d", v1)
	}
	var id string
	if err := d.Read.QueryRowContext(ctx, `SELECT value FROM app_meta WHERE key = 'installation_id'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening must not reapply anything: the installation id is stable.
	d, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var id2 string
	if err := d.Read.QueryRowContext(ctx, `SELECT value FROM app_meta WHERE key = 'installation_id'`).Scan(&id2); err != nil {
		t.Fatal(err)
	}
	if id != id2 {
		t.Fatalf("installation id changed across reopen: %q != %q", id, id2)
	}
}

func TestPragmas(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	var mode string
	if err := d.Read.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := d.Write.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatal("foreign keys are off")
	}
}

func TestMigrateFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	before, _ := d.SchemaVersion(ctx)

	bad := fstest.MapFS{
		"migrations/9001_ok.sql":     {Data: []byte(`CREATE TABLE t_ok (id INTEGER PRIMARY KEY);`)},
		"migrations/9002_broken.sql": {Data: []byte(`CREATE TABLE t_half (id INTEGER); THIS IS NOT SQL;`)},
	}
	err = migrateFS(ctx, d.Write, bad)
	if err == nil || !strings.Contains(err.Error(), "9002_broken.sql") {
		t.Fatalf("expected failure naming the broken migration, got %v", err)
	}

	after, _ := d.SchemaVersion(ctx)
	if after != 9001 {
		t.Fatalf("schema version = %d, want 9001 (before was %d)", after, before)
	}
	var n int
	_ = d.Read.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 't_half'`).Scan(&n)
	if n != 0 {
		t.Fatal("partial migration was not rolled back")
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Write.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES (99999, 'future.sql')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, d.Write); err == nil {
		t.Fatal("expected refusal on a schema newer than the binary")
	}
}

func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"no prefix": {"migrations/init.sql": {}},
		"duplicate": {"migrations/0001_a.sql": {}, "migrations/1_b.sql": {}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMigrations(fsys); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
