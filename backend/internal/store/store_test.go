package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMigrateCreatesTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name='readings'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected readings table to exist")
	}
}

func TestMigrateCreatesAllTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	wantTables := []string{
		"meters", "readings", "events", "users", "analyses",
		"meter_baselines", "anomalies", "anomaly_signals",
		"anomaly_variables", "anomaly_events",
	}

	for _, table := range wantTables {
		rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if !rows.Next() {
			rows.Close()
			t.Errorf("expected table %s to exist", table)
			continue
		}
		rows.Close()
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("second migrate should not error: %v", err)
	}
}

// TestOpenInMemoryPinsSingleConnection is the I1 regression test. For an
// in-memory SQLite DSN every extra pooled connection is a SEPARATE, empty
// database (there is no shared cache), so a nested query issued while an outer
// cursor was still open used to be served by a second connection and silently
// read from an empty database — zero rows, no error.
//
// Pinning the pool to a single connection makes that impossible: the nested
// query can no longer reach a different database. The visible trade-off is
// that it now WAITS for the outer cursor instead, which is why callers must
// still drain a cursor before querying again (see cmd/eval). This test pins
// both halves of that contract: MaxOpenConnections == 1, and a nested query
// that blocks (here surfaced as a context deadline) rather than quietly
// returning nothing.
func TestOpenInMemoryPinsSingleConnection(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("in-memory pool must be pinned to 1 connection, got %d", got)
	}

	if _, err := db.Exec(`INSERT INTO readings (meter_id, timestamp, consumption_kwh)
		VALUES ('M-101','2026-09-01T00:00:00Z',10),('M-102','2026-09-01T00:00:00Z',20)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	outer, err := db.Query(`SELECT meter_id FROM readings ORDER BY meter_id`)
	if err != nil {
		t.Fatalf("outer query: %v", err)
	}
	if !outer.Next() {
		outer.Close()
		t.Fatal("expected at least one row from the outer query")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	var n int
	nestedErr := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM readings`).Scan(&n)
	cancel()
	if nestedErr == nil && n == 0 {
		t.Fatal("nested query returned zero rows with no error: it reached a second, empty in-memory database")
	}
	if nestedErr != nil && !errors.Is(nestedErr, context.DeadlineExceeded) {
		t.Fatalf("unexpected nested query error: %v", nestedErr)
	}
	outer.Close()

	// Once the outer cursor is released the same connection serves the query
	// and the data is there, which is the drain-then-query discipline.
	if err := db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&n); err != nil {
		t.Fatalf("query after drain: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 seeded rows after draining the cursor, got %d", n)
	}
}

// TestOpenEnablesForeignKeys covers the deferred Task 1 review item: SQLite
// disables FK enforcement by default and the PRAGMA is per-connection, so Open
// must turn it on (reliable here because the pool is pinned to one connection).
func TestOpenEnablesForeignKeys(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if on != 1 {
		t.Fatalf("expected foreign_keys = ON, got %d", on)
	}
}
