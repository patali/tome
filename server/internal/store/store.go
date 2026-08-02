// Package store is Tome's SQLite persistence layer (users, invites,
// settings). Pure-Go driver (modernc.org/sqlite) so the container build stays
// CGO-free.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

-- One row per conversion, recording that it happened and never what was read.
-- No title, no URL, no domain: an operator needs to see load and failures, and
-- a server-side record of which articles someone reads is a different and far
-- more sensitive thing. Swept after 30 days.
CREATE TABLE IF NOT EXISTS conversions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  kind        TEXT NOT NULL,               -- convert | send
  format      TEXT NOT NULL,               -- pdf | epub
  ok          INTEGER NOT NULL,
  bytes       INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_conversions_created ON conversions(created_at);
CREATE INDEX IF NOT EXISTS idx_conversions_user    ON conversions(user_id, created_at);

-- Invite requests from the landing page form. Stored rather than only emailed,
-- so the published three-month retention is enforced by a sweep instead of by
-- the operator's inbox habits, and so "who is waiting" is answerable.
CREATE TABLE IF NOT EXISTS invite_requests (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  email      TEXT NOT NULL COLLATE NOCASE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  status     TEXT NOT NULL DEFAULT 'pending',  -- pending | invited | dismissed
  handled_at TEXT
);
-- At most one open request per address, so repeat submissions refresh the
-- existing row instead of filling the queue with duplicates.
CREATE UNIQUE INDEX IF NOT EXISTS idx_invite_requests_pending
  ON invite_requests(email) WHERE status = 'pending';
`

// userColumns is extended by migrations below rather than in `schema`, so an
// existing database picks the column up too — CREATE TABLE IF NOT EXISTS does
// nothing once the table is there.
var migrations = []string{
	`ALTER TABLE users ADD COLUMN last_seen_at TEXT`,
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	for _, m := range migrations {
		// Additive and idempotent by intent: a duplicate-column error means it
		// has already run. SQLite has no ADD COLUMN IF NOT EXISTS.
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migration %q: %w", m, err)
		}
	}
	return nil
}
