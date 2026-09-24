package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadingsCSVIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "readings.csv")
	content := "meter_id,timestamp,consumption_kwh,voltage_v,current_a,power_factor,status\n" +
		"M-101,2026-09-01 00:00:00,23.5,221.9,101.28,0.954,OK\n" +
		"M-101,2026-09-01 01:00:00,20.11,221.15,100.49,0.935,OK\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	db, _ := Open(":memory:")
	defer db.Close()
	db.Migrate()

	n1, err := LoadReadingsCSV(db, csvPath)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if n1 != 2 {
		t.Fatalf("expected 2 rows inserted, got %d", n1)
	}

	n2, err := LoadReadingsCSV(db, csvPath)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 new rows on reload, got %d", n2)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 total rows, got %d", count)
	}
}

func TestLoadEventsCSVRealColumns(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "events.csv")
	// Columnas reales del dataset: event_timestamp, event_type (no timestamp/type).
	content := "meter_id,event_timestamp,event_type,description\n" +
		"M-104,2026-09-11 00:00,OPERATIONAL_CHANGE,New production line activated\n"
	os.WriteFile(csvPath, []byte(content), 0o644)

	db, _ := Open(":memory:")
	defer db.Close()
	db.Migrate()

	n, err := LoadEventsCSV(db, csvPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
	var typ string
	db.QueryRow(`SELECT type FROM events WHERE meter_id='M-104'`).Scan(&typ)
	if typ != "OPERATIONAL_CHANGE" {
		t.Fatalf("expected OPERATIONAL_CHANGE, got %q", typ)
	}
}

func TestLoadReadingsCSVMalformedRowReturnsError(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "readings.csv")
	// Second row has too few fields (missing status), which trips
	// csv.Reader's field-count check and must propagate as an error,
	// not be silently swallowed as if it were EOF.
	content := "meter_id,timestamp,consumption_kwh,voltage_v,current_a,power_factor,status\n" +
		"M-101,2026-09-01 00:00:00,23.5,221.9,101.28,0.954,OK\n" +
		"M-101,2026-09-01 01:00:00,20.11,221.15,100.49\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	db, _ := Open(":memory:")
	defer db.Close()
	db.Migrate()

	_, err := LoadReadingsCSV(db, csvPath)
	if err == nil {
		t.Fatal("expected an error for the malformed row, got nil")
	}
}

func TestLoadEventsCSVMalformedRowReturnsError(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "events.csv")
	// Second row has too few fields (missing description).
	content := "meter_id,event_timestamp,event_type,description\n" +
		"M-104,2026-09-11 00:00,OPERATIONAL_CHANGE,New production line activated\n" +
		"M-106,2026-09-08 00:00,SCHEDULED_OUTAGE\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	db, _ := Open(":memory:")
	defer db.Close()
	db.Migrate()

	_, err := LoadEventsCSV(db, csvPath)
	if err == nil {
		t.Fatal("expected an error for the malformed row, got nil")
	}
}
