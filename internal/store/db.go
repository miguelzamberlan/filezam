// Package store is the SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on unique constraint violations.
var ErrConflict = errors.New("conflict")

// DB wraps a read pool and a single-connection write pool.
type DB struct {
	r   *sql.DB
	w   *sql.DB
	Now func() time.Time
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_txlock=immediate"
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxLifetime(0)
	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	db := &DB{r: r, w: w, Now: time.Now}
	if err := db.w.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Close closes both pools.
func (db *DB) Close() error {
	return errors.Join(db.r.Close(), db.w.Close())
}

// Ping checks database liveness.
func (db *DB) Ping(ctx context.Context) error { return db.r.PingContext(ctx) }

func (db *DB) now() int64 { return db.Now().Unix() }

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.w.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		verStr, _, ok := strings.Cut(name, "_")
		if !ok {
			return fmt.Errorf("bad migration name %q", name)
		}
		ver, err := strconv.Atoi(verStr)
		if err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		var n int
		if err := db.w.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=?`, ver).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.w.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?,?)`, ver, db.now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case isUnique(err):
		return ErrConflict
	}
	return err
}

func nullInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
