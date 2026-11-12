// Package db opens the SQLite database and applies schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)
)

// FileName is the database file inside the data directory.
const FileName = "aifs.db"

// DB holds two pools on the same file: a single-connection writer, so writes
// are serialised in the process, and a reader pool for concurrent reads (WAL).
type DB struct {
	Write *sql.DB
	Read  *sql.DB
}

// Open creates the data directory if needed, opens the database, and applies
// all pending migrations.
func Open(ctx context.Context, dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, FileName)

	write, err := openPool(path, 1)
	if err != nil {
		return nil, err
	}
	read, err := openPool(path, max(4, runtime.NumCPU()))
	if err != nil {
		_ = write.Close()
		return nil, err
	}
	d := &DB{Write: write, Read: read}

	if err := migrate(ctx, d.Write); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func openPool(path string, conns int) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")

	pool, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	pool.SetMaxOpenConns(conns)
	pool.SetMaxIdleConns(conns)
	return pool, nil
}

// Ping checks that the database answers.
func (d *DB) Ping(ctx context.Context) error {
	return d.Read.PingContext(ctx)
}

// Close closes both pools.
func (d *DB) Close() error {
	rerr := d.Read.Close()
	werr := d.Write.Close()
	if werr != nil {
		return werr
	}
	return rerr
}
