package store

import "testing"

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
