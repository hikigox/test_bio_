package store

import (
	"database/sql"

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
	return &DB{sqlDB}, nil
}

// Migrate creates all tables defined in the schema if they do not already
// exist. It is safe to call multiple times.
func (db *DB) Migrate() error {
	_, err := db.Exec(schemaSQL)
	return err
}
