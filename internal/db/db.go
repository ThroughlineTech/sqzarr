package db

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection with query helpers.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs migrations.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Single writer — serialise writes through one connection.
	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		_ = conn.Close() // Preserve the migration error while releasing the connection.
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying *sql.DB for use by query layers.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}
	return db.alterations()
}

// alterations applies idempotent ALTER TABLE statements for columns added after
// the initial schema. SQLite does not support IF NOT EXISTS on ALTER TABLE, so
// we ignore "duplicate column" errors.
func (db *DB) alterations() error {
	alters := []string{
		`ALTER TABLE jobs ADD COLUMN fail_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE directories ADD COLUMN bitrate_skip_margin REAL NOT NULL DEFAULT 0.10`,
	}
	for _, stmt := range alters {
		if _, err := db.conn.Exec(stmt); err != nil {
			// "duplicate column name" is expected on databases that already have it.
			if !isDuplicateColumn(err) {
				return fmt.Errorf("alter: %w", err)
			}
		}
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS directories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    path                 TEXT    NOT NULL UNIQUE,
    enabled              BOOLEAN NOT NULL DEFAULT 1,
    min_age_days         INTEGER NOT NULL DEFAULT 7,
    max_bitrate          INTEGER NOT NULL DEFAULT 2222000,
    min_size_mb          INTEGER NOT NULL DEFAULT 500,
    bitrate_skip_margin  REAL    NOT NULL DEFAULT 0.10,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS jobs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    directory_id    INTEGER REFERENCES directories(id),
    source_path     TEXT    NOT NULL,
    source_size     INTEGER NOT NULL,
    source_codec    TEXT    NOT NULL,
    source_duration REAL    NOT NULL,
    source_bitrate  INTEGER NOT NULL,
    output_path     TEXT,
    output_size     INTEGER,
    encoder_used    TEXT,
    status          TEXT    NOT NULL DEFAULT 'pending',
    priority        INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT,
    progress        REAL    NOT NULL DEFAULT 0,
    bytes_saved     INTEGER,
    fail_count      INTEGER NOT NULL DEFAULT 0,
    started_at      DATETIME,
    finished_at     DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_jobs_status      ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_source_path ON jobs(source_path);

-- originals: holds source files moved aside while the transcoded copy is reviewed.
-- The original sits at held_path (in the processed dir) until the user deletes it
-- or the retention period expires.
CREATE TABLE IF NOT EXISTS originals (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id          INTEGER NOT NULL REFERENCES jobs(id),
    original_path   TEXT    NOT NULL,  -- where the file was originally
    held_path       TEXT    NOT NULL,  -- where the original now lives (processed dir)
    output_path     TEXT    NOT NULL,  -- where the transcoded file was placed
    original_size   INTEGER NOT NULL DEFAULT 0,
    output_size     INTEGER NOT NULL DEFAULT 0,
    expires_at      DATETIME NOT NULL,
    deleted_at      DATETIME,          -- set when user deletes the original
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scan_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    directory_id    INTEGER REFERENCES directories(id),
    files_scanned   INTEGER NOT NULL DEFAULT 0,
    files_queued    INTEGER NOT NULL DEFAULT 0,
    files_skipped   INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER,
    error           TEXT,
    started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at     DATETIME
);

CREATE TABLE IF NOT EXISTS stats (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    total_bytes_saved INTEGER NOT NULL DEFAULT 0,
    total_jobs_done   INTEGER NOT NULL DEFAULT 0,
    total_jobs_failed INTEGER NOT NULL DEFAULT 0,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO stats (id) VALUES (1);

CREATE TABLE IF NOT EXISTS processed_files (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source_path  TEXT    NOT NULL UNIQUE,
    status       TEXT    NOT NULL,  -- done, excluded, skipped
    reason       TEXT,
    source_size  INTEGER NOT NULL DEFAULT 0,
    source_mtime DATETIME,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_processed_files_source_path ON processed_files(source_path);
`
