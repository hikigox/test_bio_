package api

import (
	"testing"
	"time"

	"energy-management/internal/store"
)

func seedTwoWeeksOfReadings(t *testing.T, db *store.DB, meterID string, kwh func(h int) float64) {
	t.Helper()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 14*24; h++ {
		ts := start.Add(time.Duration(h) * time.Hour).Format(time.RFC3339)
		_, err := db.Exec(`INSERT INTO readings (meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
			VALUES (?, ?, ?, 220, 10, 0.95, 'OK')`, meterID, ts, kwh(h))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartAnalysisRejectsBaselineWindowUnder3Days(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	seedTwoWeeksOfReadings(t, db, "M-101", func(h int) float64 { return 10 })

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	baselineFrom := from
	baselineTo := from.Add(2 * 24 * time.Hour) // < 3 días

	_, err := StartAnalysis(db, AnalysisScope{From: from, To: to, BaselineFrom: &baselineFrom, BaselineTo: &baselineTo}, 1)
	if err == nil {
		t.Fatal("expected error for baseline window under 3 days")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestStartAnalysisRejectsConcurrentRun(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	seedTwoWeeksOfReadings(t, db, "M-101", func(h int) float64 { return 10 })
	db.Exec(`INSERT INTO analyses (status, data_from, data_to) VALUES ('RUNNING', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z')`)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	_, err := StartAnalysis(db, AnalysisScope{From: from, To: to}, 1)
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
}

func TestStartAnalysisCreatesPendingRowAndReturnsID(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	seedTwoWeeksOfReadings(t, db, "M-101", func(h int) float64 { return 10 })

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	id, err := StartAnalysis(db, AnalysisScope{From: from, To: to}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero analysis id")
	}

	// esperar a que la goroutine termine (dataset pequeño en test)
	waitForAnalysisDone(t, db, id, 2*time.Second)

	var status string
	db.QueryRow(`SELECT status FROM analyses WHERE id = ?`, id).Scan(&status)
	if status != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %v", status)
	}
}

func waitForAnalysisDone(t *testing.T, db *store.DB, id int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var status string
		db.QueryRow(`SELECT status FROM analyses WHERE id = ?`, id).Scan(&status)
		if status == "COMPLETED" || status == "FAILED" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for analysis to finish")
}
