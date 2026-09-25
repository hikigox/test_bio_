package store

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB connected to the application's SQLite database.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at dbPath. Use ":memory:" for
// an in-memory database, primarily useful in tests.
func Open(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", withBusyTimeout(dbPath))
	if err != nil {
		return nil, err
	}

	// For an in-memory SQLite DSN every connection is a SEPARATE, empty
	// database (there is no shared cache), so the moment database/sql opens a
	// second pooled connection — e.g. a nested query while another cursor is
	// still open, or two concurrent HTTP handlers — the caller silently reads
	// from an empty database with no error at all. Pinning the pool to a
	// single connection makes that impossible.
	if isMemoryDSN(dbPath) {
		sqlDB.SetMaxOpenConns(1)
	}

	// SQLite disables foreign key enforcement by default and the PRAGMA is
	// per-connection. With MaxOpenConns(1) above, an in-memory database keeps
	// it for its whole lifetime; for a file database database/sql may open
	// further connections, so this is best-effort there and the schema's
	// constraints remain the authority.
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return &DB{sqlDB}, nil
}

// withBusyTimeout appends a `_pragma=busy_timeout(5000)` DSN param so the
// driver applies it to EVERY connection it opens (unlike `db.Exec("PRAGMA
// busy_timeout...")`, which only touches whichever single pooled connection
// happens to run it — the same "best-effort" gap noted below for
// foreign_keys). Without this, a file-backed deployment (Docker, where
// database/sql freely hands out multiple real connections to the same
// SQLite file — see the MaxOpenConns comment above) hits SQLite's default
// behavior of failing a query IMMEDIATELY with "database is locked" the
// moment another connection is mid-write, instead of waiting briefly for the
// lock to clear. That single spurious error was being reported to the
// frontend as a permanent 404 "análisis no encontrado" (GET /ai/analysis/:id
// conflated ANY db error with "row doesn't exist" — see the sql.ErrNoRows
// check in analysis.go) even though the row existed the whole time.
func withBusyTimeout(dbPath string) string {
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	return dbPath + sep + "_pragma=busy_timeout(5000)"
}

// isMemoryDSN reports whether the DSN refers to an in-memory database, either
// as the bare ":memory:" path or via the "mode=memory" URI parameter.
func isMemoryDSN(dbPath string) bool {
	return dbPath == ":memory:" ||
		strings.Contains(dbPath, ":memory:") ||
		strings.Contains(dbPath, "mode=memory")
}

// Migrate creates all tables defined in the schema if they do not already
// exist. It is safe to call multiple times.
func (db *DB) Migrate() error {
	_, err := db.Exec(schemaSQL)
	return err
}
