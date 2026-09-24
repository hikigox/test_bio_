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
	sqlDB, err := sql.Open("sqlite", dbPath)
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
