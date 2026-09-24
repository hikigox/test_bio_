package api

import (
	"testing"
	"time"

	"energy-management/internal/store"
)

func setupAnalysisWithAnomaly(t *testing.T, db *store.DB) (analysisID int64) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO analyses (status, data_from, data_to) VALUES ('COMPLETED', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	analysisID, _ = res.LastInsertId()
	_, err = db.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, method, hourly_profile_json, baseline_kwh, actual_kwh, variation_pct)
		VALUES (?, 'M-109', 'HOURLY_MEDIAN', ?, 1070, 2180, 103.7)`, analysisID, hourlyProfileJSONFixture())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO anomalies (analysis_id, meter_id, type, severity, confidence, priority_score,
		baseline_kwh, actual_kwh, variation_pct, period_from, period_to, change_point_at, status, created_at, updated_at)
		VALUES (?, 'M-109', 'REAL_ANOMALY', 'HIGH', 0.96, 5.8, 1070, 2180, 103.7,
		'2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z', '2026-09-08T00:00:00Z', 'OPEN', '2026-09-14T23:00:00Z', '2026-09-14T23:00:00Z')`, analysisID)
	if err != nil {
		t.Fatal(err)
	}
	return analysisID
}

func hourlyProfileJSONFixture() string {
	return `[10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10]`
}

func TestCurrentAnomalyReturnsLatestCompletedAnalysis(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	setupAnalysisWithAnomaly(t, db)

	anomaly, err := CurrentAnomaly(db, "M-109")
	if err != nil {
		t.Fatal(err)
	}
	if anomaly == nil || anomaly.Type != "REAL_ANOMALY" {
		t.Fatalf("expected REAL_ANOMALY for M-109, got %+v", anomaly)
	}
}

func TestCurrentAnomalyReturnsNilWhenNeverAnalyzed(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()

	anomaly, err := CurrentAnomaly(db, "M-999")
	if err != nil {
		t.Fatal(err)
	}
	if anomaly != nil {
		t.Fatalf("expected nil anomaly for never-analyzed meter, got %+v", anomaly)
	}
}

func TestCurrentAnomalyReturnsNilWhenMeterNormalizedInLatestAnalysis(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()

	// Análisis viejo (#1, COMPLETED): M-109 tuvo anomalía.
	setupAnalysisWithAnomaly(t, db)

	// Análisis nuevo (#2, COMPLETED, más reciente): M-109 fue incluido
	// (tiene fila en meter_baselines) pero ya no es anómalo (sin fila en
	// anomalies). El medidor se normalizó; vigencia debe reflejar eso.
	res, err := db.Exec(`INSERT INTO analyses (status, data_from, data_to, finished_at)
		VALUES ('COMPLETED', '2026-09-15T00:00:00Z', '2026-09-28T23:00:00Z', '2026-09-28T23:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	newAnalysisID, _ := res.LastInsertId()
	_, err = db.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, method, hourly_profile_json, baseline_kwh, actual_kwh, variation_pct)
		VALUES (?, 'M-109', 'HOURLY_MEDIAN', ?, 1070, 1100, 2.8)`, newAnalysisID, hourlyProfileJSONFixture())
	if err != nil {
		t.Fatal(err)
	}

	anomaly, err := CurrentAnomaly(db, "M-109")
	if err != nil {
		t.Fatal(err)
	}
	if anomaly != nil {
		t.Fatalf("expected nil (meter normalized in latest analysis, stale old anomaly must not leak), got %+v", anomaly)
	}
}

func TestActiveWindowUsesChangePointWhenPresent(t *testing.T) {
	a := AnomalyRow{
		PeriodFrom:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		PeriodTo:      time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC),
		ChangePointAt: ptrTime(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)),
	}
	from, to := ActiveWindow(a)
	if !from.Equal(*a.ChangePointAt) || !to.Equal(a.PeriodTo) {
		t.Fatalf("expected active window [change_point, period_to], got [%v, %v]", from, to)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestBaselineForRangeSumsProfileOverExistingTimestamps(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	setupAnalysisWithAnomaly(t, db)
	// una sola lectura a las 05:00, perfil horario = 10 en todas las horas
	db.Exec(`INSERT INTO readings (meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
		VALUES ('M-109', '2026-09-01T05:00:00Z', 20, 220, 10, 0.95, 'OK')`)

	baselineKWh, actualKWh, err := BaselineForRange(db, "M-109",
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if baselineKWh != 10 {
		t.Fatalf("expected baseline 10 (profile at hour 5), got %v", baselineKWh)
	}
	if actualKWh != 20 {
		t.Fatalf("expected actual 20, got %v", actualKWh)
	}
}
