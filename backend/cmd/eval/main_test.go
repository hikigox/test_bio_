package main

import (
	"os"
	"path/filepath"
	"testing"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

func TestParseExpectedCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "expected_results.csv")
	content := "meter_id,expected_type,expected_severity\n" +
		"M-109,REAL_ANOMALY,HIGH\n" +
		"M-106,FALSE_POSITIVE,LOW\n"
	os.WriteFile(path, []byte(content), 0o644)

	rows, err := parseExpectedCSV(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 || rows[0].MeterID != "M-109" || rows[0].ExpectedType != "REAL_ANOMALY" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestScoreRubricAllCorrect(t *testing.T) {
	expected := []expectedRow{
		{MeterID: "M-109", ExpectedType: "REAL_ANOMALY", ExpectedSeverity: "HIGH"},
	}
	actual := map[string]actualRow{
		"M-109": {Type: "REAL_ANOMALY", Severity: "HIGH", PriorityRank: 1},
	}
	score := scoreRubric(expected, actual)
	if score.DetectionPoints != 30 {
		t.Fatalf("expected full detection points, got %v", score)
	}
	if score.PriorityPoints != 25 {
		t.Fatalf("expected full priority points, got %v", score)
	}
}

func TestScoreRubricFalsePositive(t *testing.T) {
	expected := []expectedRow{
		{MeterID: "M-106", ExpectedType: "FALSE_POSITIVE", ExpectedSeverity: "LOW"},
	}
	actual := map[string]actualRow{
		"M-106": {Type: "FALSE_POSITIVE", Severity: "LOW", PriorityRank: 2},
	}
	score := scoreRubric(expected, actual)
	if score.FalsePosPoints != 15 {
		t.Fatalf("expected full false positive points, got %v", score)
	}
}

func TestScoreRubricDataQuality(t *testing.T) {
	expected := []expectedRow{
		{MeterID: "M-112", ExpectedType: "DATA_QUALITY", ExpectedSeverity: "LOW"},
	}
	actual := map[string]actualRow{
		"M-112": {Type: "DATA_QUALITY", Severity: "LOW", PriorityRank: 3},
	}
	score := scoreRubric(expected, actual)
	if score.DataQualityPoints != 10 {
		t.Fatalf("expected full data quality points, got %v", score)
	}
}

func TestScoreRubricMissingActual(t *testing.T) {
	// Expected meter not present in actual results at all (e.g. no readings
	// loaded for it) must not panic and must score zero for that meter.
	expected := []expectedRow{
		{MeterID: "M-999", ExpectedType: "REAL_ANOMALY", ExpectedSeverity: "HIGH"},
	}
	actual := map[string]actualRow{}
	score := scoreRubric(expected, actual)
	if score.Total() != 0 {
		t.Fatalf("expected zero score for missing meter, got %v", score)
	}
}

// TestRunEngineOverStoreRealDataset is an end-to-end smoke test: it loads the
// real dataset shipped in the repo (data/readings.csv, data/events.csv) into
// an in-memory store, runs the engine per meter via runEngineOverStore, and
// scores against a synthetic expected_results.csv written to a temp dir. The
// synthetic file is NOT the real expected_results.csv (which must never be
// committed to the repo per spec 00 regla 1) — it only exercises the wiring.
func TestRunEngineOverStoreRealDataset(t *testing.T) {
	dataDir := findRepoDataDir(t)
	if dataDir == "" {
		t.Skip("real dataset (data/readings.csv, data/events.csv) not found relative to repo root")
	}

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := store.LoadReadingsCSV(db, filepath.Join(dataDir, "readings.csv")); err != nil {
		t.Fatalf("load readings: %v", err)
	}
	if _, err := store.LoadEventsCSV(db, filepath.Join(dataDir, "events.csv")); err != nil {
		t.Fatalf("load events: %v", err)
	}

	results := runEngineOverStore(db)
	if len(results) == 0 {
		t.Fatalf("expected at least one anomaly result from the real dataset, got none")
	}

	// Regression guard: running engine.Run directly against M-109's readings
	// (loaded via the same loadMeterReadings helper) must agree with what
	// runEngineOverStore reports for M-109. This specifically catches a bug
	// where issuing per-meter queries on the same *sql.DB while the outer
	// "SELECT DISTINCT meter_id" rows were still open forced database/sql to
	// open a second pooled connection — which, for a ":memory:" SQLite
	// database, is a completely separate empty database, silently making
	// every meter's readings/events come back empty.
	direct := engine.Run("M-109", loadMeterReadings(db, "M-109"), loadMeterEvents(db, "M-109"), 0.15, engine.DefaultConfig())
	if !direct.HasAnomaly {
		t.Fatalf("expected M-109 to have an anomaly when run directly, got HasAnomaly=false")
	}
	viaStore, ok := results["M-109"]
	if !ok {
		t.Fatalf("expected M-109 in runEngineOverStore results, got %+v", results)
	}
	if viaStore.Type != string(direct.Type) {
		t.Fatalf("runEngineOverStore M-109 type %q does not match direct engine.Run type %q", viaStore.Type, direct.Type)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "expected_results.csv")
	content := "meter_id,expected_type,expected_severity\n" +
		"M-999-NONEXISTENT,REAL_ANOMALY,HIGH\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write synthetic expected csv: %v", err)
	}

	expected, err := parseExpectedCSV(path)
	if err != nil {
		t.Fatalf("parse synthetic expected csv: %v", err)
	}
	score := scoreRubric(expected, results)
	if score.Total() != 0 {
		t.Fatalf("synthetic expected csv references a nonexistent meter, expected zero score, got %+v", score)
	}
}

// findRepoDataDir walks up from the current working directory looking for a
// data/readings.csv file (the real dataset lives at the repo root, two
// levels above backend/cmd/eval). Returns "" if not found within a few
// levels, so the test can skip cleanly in environments without the dataset.
func findRepoDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "data", "readings.csv")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(dir, "data")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
