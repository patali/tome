// Package store is Tome's SQLite persistence layer (users, invites,
// settings). Pure-Go driver (modernc.org/sqlite) so the container build stays
// CGO-free.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// Open creates/opens the database at <dataDir>/tome.db and migrates the
// schema. WAL + busy_timeout make concurrent access from `tome init-admin`
// (via container exec) safe while the server is running.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("data dir %s: %w", dataDir, err)
	}
	dsn := "file:" + filepath.Join(dataDir, "tome.db") +
		"?_txlock=immediate" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite handles one writer at a time; avoid db-locked churn.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  email          TEXT NOT NULL UNIQUE COLLATE NOCASE,
  kindle_email   TEXT NOT NULL,
  api_key_hash   TEXT NOT NULL UNIQUE,
  api_key_prefix TEXT NOT NULL,
  is_admin       INTEGER NOT NULL DEFAULT 0,
  disabled       INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE IF NOT EXISTS invites (
  code       TEXT PRIMARY KEY,
  email_hint TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  expires_at TEXT NOT NULL,
  used_by    INTEGER REFERENCES users(id),
  used_at    TEXT
);
`

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}
