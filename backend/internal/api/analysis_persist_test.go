package api

import (
	"encoding/json"
	"testing"
	"time"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

func TestPersistResultsWritesBaselineForEveryMeterAndAnomalyOnlyWhenPresent(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	stable := engine.MeterResult{
		MeterID: "M-101", HasAnomaly: false,
		Baseline:    engine.HourlyProfile{MedianByHour: [24]float64{}},
		BaselineKWh: 100, ActualKWh: 102, VariationPct: 2,
	}
	withAnomaly := engine.MeterResult{
		MeterID: "M-109", HasAnomaly: true, Type: engine.RealAnomaly, Severity: engine.High,
		Confidence: 0.96, PriorityScore: 5.8, BaselineKWh: 1070, ActualKWh: 2180, VariationPct: 103.7,
		Reason: "Consumo 103.7% por encima del baseline...", RecommendedAction: "Investigar medidor e instalación",
		Baseline: engine.HourlyProfile{MedianByHour: [24]float64{}},
		Evidence: engine.Evidence{DecisionPath: []string{"no_data_quality_issue", "significant_change", "no_event"}},
	}

	analyzedFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	analyzedTo := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	err := persistResults(db, 1, []meterRun{
		{Result: stable, PeriodFrom: analyzedFrom, PeriodTo: analyzedTo, ReadingsCount: 336},
		{Result: withAnomaly, PeriodFrom: analyzedFrom, PeriodTo: analyzedTo, ReadingsCount: 336},
	})
	if err != nil {
		t.Fatalf("persist: %v", err)
	}

	var baselineCount int
	db.QueryRow(`SELECT COUNT(*) FROM meter_baselines WHERE analysis_id = 1`).Scan(&baselineCount)
	if baselineCount != 2 {
		t.Fatalf("expected 2 baseline rows (one per meter), got %d", baselineCount)
	}

	var anomalyCount int
	db.QueryRow(`SELECT COUNT(*) FROM anomalies WHERE analysis_id = 1`).Scan(&anomalyCount)
	if anomalyCount != 1 {
		t.Fatalf("expected 1 anomaly row (only M-109), got %d", anomalyCount)
	}

	var evidenceJSON string
	db.QueryRow(`SELECT evidence_json FROM anomalies WHERE meter_id = 'M-109'`).Scan(&evidenceJSON)
	var evidence map[string]interface{}
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		t.Fatalf("evidence_json not valid JSON: %v", err)
	}
	if evidence["decision_path"] == nil {
		t.Fatal("expected decision_path in evidence_json")
	}
}

func TestPersistResultsWritesMeterStatusFromAnomaly(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	result := engine.MeterResult{
		MeterID: "M-109", HasAnomaly: true, Type: engine.RealAnomaly, Severity: engine.High,
		Baseline: engine.HourlyProfile{},
	}
	persistResults(db, 1, []meterRun{{Result: result}})

	var status string
	db.QueryRow(`SELECT status FROM meters WHERE meter_id = 'M-109'`).Scan(&status)
	if status != "CRITICAL" { // REAL_ANOMALY·HIGH -> CRITICAL, spec 03
		t.Fatalf("expected CRITICAL status, got %v", status)
	}
}

// TestPersistResultsRollsBackOnPartialFailure proves the transaction is
// all-or-nothing: if inserting one meter's baseline fails partway through a
// multi-meter batch (here, a duplicate analysis_id+meter_id pair violating
// the UNIQUE constraint on meter_baselines), no row from that persistResults
// call should be visible afterward -- not even the first meter's row that
// was inserted successfully before the failure.
func TestPersistResultsRollsBackOnPartialFailure(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)
	// Pre-existing row that will collide with the second result's insert,
	// forcing persistResults to fail after the first insert has succeeded.
	db.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id) VALUES (1, 'M-202')`)

	first := engine.MeterResult{MeterID: "M-101", Baseline: engine.HourlyProfile{}}
	colliding := engine.MeterResult{MeterID: "M-202", Baseline: engine.HourlyProfile{}}

	err := persistResults(db, 1, []meterRun{{Result: first}, {Result: colliding}})
	if err == nil {
		t.Fatal("expected error from UNIQUE constraint violation on second insert")
	}

	var countM101 int
	db.QueryRow(`SELECT COUNT(*) FROM meter_baselines WHERE analysis_id = 1 AND meter_id = 'M-101'`).Scan(&countM101)
	if countM101 != 0 {
		t.Fatalf("expected rollback to remove M-101's row too, got %d rows", countM101)
	}
}
