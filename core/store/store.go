// Package store is the core's SQLite storage (WAL mode, pure Go driver):
// one file, concurrent readers plus one writer. Migrations only move forward.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Store is an open Kombinat database.
type Store struct {
	db *sql.DB
}

// migrations[i] moves the schema from version i to i+1; never edit a released one.
var migrations = []string{
	// 1: the append-only event log.
	`CREATE TABLE events (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		ts      TEXT NOT NULL,
		actor   TEXT NOT NULL,
		action  TEXT NOT NULL,
		subject TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		payload TEXT
	);
	CREATE INDEX events_action ON events(action);
	CREATE TRIGGER events_no_update BEFORE UPDATE ON events
		BEGIN SELECT RAISE(ABORT, 'events are append-only'); END;
	CREATE TRIGGER events_no_delete BEFORE DELETE ON events
		BEGIN SELECT RAISE(ABORT, 'events are append-only'); END;`,
}

// Open opens (creating when missing) the database at path and migrates it.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create dir: %w", err)
	}
	// Escape the URI-special characters a file path may carry.
	uriPath := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	dsn := "file:" + uriPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Version returns the schema version.
func (s *Store) Version(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a committed tx makes this a no-op
	var v int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if v > len(migrations) {
		return fmt.Errorf("store: schema version %d is newer than this core (%d); refusing to touch it", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return fmt.Errorf("store: set schema version: %w", err)
	}
	return tx.Commit()
}
