package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

// --- C1: every analyzed meter leaves UNKNOWN --------------------------------

// TestPersistResultsSetsOKForAnalyzedButNormalMeter covers C1 directly at the
// persistence layer: insertMeterBaseline runs for every analyzed meter, so it
// is the only place a meter with no anomaly can leave UNKNOWN. Spec 03's status
// table reserves UNKNOWN for "Nunca analizado" and requires OK for "sin
// anomalía".
func TestPersistResultsSetsOKForAnalyzedButNormalMeter(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-101', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	normal := engine.MeterResult{MeterID: "M-101", HasAnomaly: false, Baseline: engine.HourlyProfile{}}
	if err := persistResults(db, 1, []meterRun{{Result: normal}}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	var status string
	db.QueryRow(`SELECT status FROM meters WHERE meter_id = 'M-101'`).Scan(&status)
	if status != "OK" {
		t.Fatalf("analyzed meter with no anomaly must be OK (spec 03), got %q", status)
	}
}

// TestPersistResultsAnomalyStatusWinsOverBaselineOK proves the ordering inside
// persistResults: insertMeterBaseline writes OK first, insertAnomaly then
// overwrites it for the same meter in the same transaction. If the order ever
// flips, a CRITICAL meter would silently be reported OK.
func TestPersistResultsAnomalyStatusWinsOverBaselineOK(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-106', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	critical := engine.MeterResult{MeterID: "M-109", HasAnomaly: true, Type: engine.RealAnomaly, Severity: engine.High}
	falsePositive := engine.MeterResult{MeterID: "M-106", HasAnomaly: true, Type: engine.FalsePositive, Severity: engine.Low}
	if err := persistResults(db, 1, []meterRun{{Result: critical}, {Result: falsePositive}}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	for meterID, want := range map[string]string{"M-109": "CRITICAL", "M-106": "OK"} {
		var got string
		db.QueryRow(`SELECT status FROM meters WHERE meter_id = ?`, meterID).Scan(&got)
		if got != want {
			t.Errorf("%s: want status %s, got %s", meterID, want, got)
		}
	}
}

// --- C2: anomaly period must be the analyzed period -------------------------

// TestInsertAnomalyPersistsAnalyzedPeriodNotBaselineWindow covers C2: the
// anomaly's period_from/period_to must be the period actually analyzed, not
// the engine's baseline/reference window (which ends at the change point).
// Otherwise spec 03's derived active window (`active_from = change_point_at ??
// period_from`, `active_to = period_to`) collapses to zero length.
func TestInsertAnomalyPersistsAnalyzedPeriodNotBaselineWindow(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO analyses (id, status, finished_at) VALUES (1, 'COMPLETED', '2026-09-15T00:00:00Z')`)

	analyzedFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	analyzedTo := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	changePoint := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	// The engine's baseline window deliberately ENDS at the change point.
	result := engine.MeterResult{
		MeterID: "M-109", HasAnomaly: true, Type: engine.RealAnomaly, Severity: engine.High,
		Confidence: 0.96, PriorityScore: 5.8,
		Baseline:    engine.HourlyProfile{WindowFrom: analyzedFrom, WindowTo: changePoint},
		ChangePoint: engine.ChangePoint{Found: true, At: changePoint},
	}
	if err := persistResults(db, 1, []meterRun{{Result: result, PeriodFrom: analyzedFrom, PeriodTo: analyzedTo}}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	var periodFrom, periodTo, windowFrom, windowTo string
	db.QueryRow(`SELECT period_from, period_to FROM anomalies WHERE meter_id = 'M-109'`).Scan(&periodFrom, &periodTo)
	db.QueryRow(`SELECT window_from, window_to FROM meter_baselines WHERE meter_id = 'M-109'`).Scan(&windowFrom, &windowTo)

	if periodTo != analyzedTo.Format(time.RFC3339) {
		t.Errorf("anomalies.period_to must be the analyzed period's end (%s), got %s",
			analyzedTo.Format(time.RFC3339), periodTo)
	}
	if periodFrom != analyzedFrom.Format(time.RFC3339) {
		t.Errorf("anomalies.period_from must be the analyzed period's start (%s), got %s",
			analyzedFrom.Format(time.RFC3339), periodFrom)
	}
	// meter_baselines keeps sourcing the reference window — a different,
	// legitimately distinct concept that must NOT change with this fix.
	if windowTo != changePoint.Format(time.RFC3339) {
		t.Errorf("meter_baselines.window_to must stay the baseline window end (%s), got %s",
			changePoint.Format(time.RFC3339), windowTo)
	}

	anomaly, err := CurrentAnomaly(db, "M-109")
	if err != nil || anomaly == nil {
		t.Fatalf("CurrentAnomaly: %v / %v", anomaly, err)
	}
	activeFrom, activeTo := ActiveWindow(*anomaly)
	if !activeTo.After(activeFrom) {
		t.Fatalf("active window must have real length, got %s .. %s", activeFrom, activeTo)
	}
	if hours := activeTo.Sub(activeFrom).Hours(); hours <= 0 {
		t.Fatalf("duration_hours must be > 0, got %v", hours)
	}
	// And the overlap filter must still match a range at the very end.
	if !Overlaps(activeFrom, activeTo, analyzedTo.Add(-24*time.Hour), analyzedTo) {
		t.Fatal("an anomaly ongoing through the end of the period must overlap a last-day filter")
	}
}

// --- C3: analyzed-but-normal meters must not report null figures ------------

// setupAnalysisNormalMeter seeds a COMPLETED analysis that INCLUDED M-200
// (meter_baselines row) but produced no anomaly for it — the "analyzed and
// normal" state C3 is about.
func setupAnalysisNormalMeter(t *testing.T, db *store.DB) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO analyses (status, data_from, data_to, finished_at)
		VALUES ('COMPLETED', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z', '2026-09-15T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	analysisID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, method, window_from, window_to,
		hourly_profile_json, baseline_kwh, actual_kwh, variation_pct)
		VALUES (?, 'M-200', 'HOURLY_MEDIAN', '2026-09-01T00:00:00Z', '2026-09-07T23:00:00Z', ?, 1000, 1020, 2)`,
		analysisID, hourlyProfileJSONFixture()); err != nil {
		t.Fatal(err)
	}
	return analysisID
}

func TestGetMetersReportsFiguresForAnalyzedButNormalMeter(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-200', 'OK', ?)`, time.Now().Format(time.RFC3339))
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-999', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisNormalMeter(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []meterListItem `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	byID := map[string]meterListItem{}
	for _, it := range resp.Items {
		byID[it.MeterID] = it
	}

	normal := byID["M-200"]
	if normal.AnalysisID == nil {
		t.Fatal("M-200 was analyzed, expected a non-null analysis_id")
	}
	if normal.Anomaly != nil {
		t.Fatalf("M-200 is normal, expected anomaly null, got %+v", normal.Anomaly)
	}
	if normal.ConsumptionKWh == nil || *normal.ConsumptionKWh != 1020 {
		t.Errorf("M-200 consumption_kwh must come from meter_baselines (1020), got %v", normal.ConsumptionKWh)
	}
	if normal.BaselineKWh == nil || *normal.BaselineKWh != 1000 {
		t.Errorf("M-200 baseline_kwh must come from meter_baselines (1000), got %v", normal.BaselineKWh)
	}
	if normal.VariationPct == nil || *normal.VariationPct != 2 {
		t.Errorf("M-200 variation_pct must come from meter_baselines (2), got %v", normal.VariationPct)
	}
	if normal.AnalysisPeriod == nil || normal.AnalysisPeriod.From != "2026-09-01T00:00:00Z" ||
		normal.AnalysisPeriod.To != "2026-09-14T23:00:00Z" {
		t.Errorf("M-200 analysis_period must come from the analysis's data_from/data_to, got %+v", normal.AnalysisPeriod)
	}

	// The genuinely-never-analyzed case must STILL be all null (spec 03 line 82).
	never := byID["M-999"]
	if never.AnalysisID != nil || never.ConsumptionKWh != nil || never.BaselineKWh != nil ||
		never.VariationPct != nil || never.AnalysisPeriod != nil {
		t.Errorf("M-999 was never analyzed, every analysis-derived field must stay null, got %+v", never)
	}
}

// --- I1: status / q / sort / order on GET /meters ---------------------------

// seedFilterableFleet builds three meters with distinct statuses, consumption
// and severities so each of ?status=, ?q=, ?sort= can be asserted separately,
// plus a never-analyzed one to pin the null-handling of the sorts.
func seedFilterableFleet(t *testing.T, srv *Server) {
	t.Helper()
	now := time.Now().Format(time.RFC3339)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, now)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-104', 'ALERT', ?)`, now)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-200', 'OK', ?)`, now)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('X-999', 'UNKNOWN', ?)`, now)

	res, _ := srv.DB.Exec(`INSERT INTO analyses (status, data_from, data_to, finished_at)
		VALUES ('COMPLETED', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z', '2026-09-15T00:00:00Z')`)
	analysisID, _ := res.LastInsertId()
	seed := func(meterID string, baseline, actual, variation float64) {
		srv.DB.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, method, window_from, window_to,
			hourly_profile_json, baseline_kwh, actual_kwh, variation_pct)
			VALUES (?, ?, 'HOURLY_MEDIAN', '2026-09-01T00:00:00Z', '2026-09-07T23:00:00Z', ?, ?, ?, ?)`,
			analysisID, meterID, hourlyProfileJSONFixture(), baseline, actual, variation)
	}
	seed("M-109", 1070, 2180, 103.7)
	seed("M-104", 900, 1100, 22.2)
	seed("M-200", 1000, 1020, 2)

	// The anomaly's own stored figures are what /meters reports when no
	// explicit ?from=&to= is given, so they must mirror the baseline row's.
	anomaly := func(meterID, typ, severity string, score, baseline, actual, variation float64) {
		srv.DB.Exec(`INSERT INTO anomalies (analysis_id, meter_id, type, severity, confidence, priority_score,
			baseline_kwh, actual_kwh, variation_pct, period_from, period_to, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, 0.9, ?, ?, ?, ?, '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z', 'OPEN', ?, ?)`,
			analysisID, meterID, typ, severity, score, baseline, actual, variation,
			"2026-09-15T00:00:00Z", "2026-09-15T00:00:00Z")
	}
	anomaly("M-109", "REAL_ANOMALY", "HIGH", 5.8, 1070, 2180, 103.7)
	anomaly("M-104", "EXPLAINABLE_ANOMALY", "MEDIUM", 2.1, 900, 1100, 22.2)
}

func meterIDsFrom(t *testing.T, srv *Server, router http.Handler, path string) []string {
	t.Helper()
	req := authedRequest(t, srv, http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d: %s", path, rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []meterListItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	ids := make([]string, 0, len(resp.Items))
	for _, it := range resp.Items {
		ids = append(ids, it.MeterID)
	}
	return ids
}

func assertIDs(t *testing.T, path string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("GET %s: want %v, got %v", path, want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GET %s: want %v, got %v", path, want, got)
		}
	}
}

func TestGetMetersStatusFilter(t *testing.T) {
	srv := newTestServer(t)
	seedFilterableFleet(t, srv)
	router := NewRouter(srv)

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"status=critical", []string{"M-109"}},
		{"status=alert", []string{"M-104"}},
		{"status=normal", []string{"M-200"}},
		// Unfiltered order is the natural meters-table order, untouched.
		{"status=all", []string{"M-109", "M-104", "M-200", "X-999"}},
		{"", []string{"M-109", "M-104", "M-200", "X-999"}},
	} {
		path := "/api/meters?" + tc.query
		assertIDs(t, path, meterIDsFrom(t, srv, router, path), tc.want)
	}
}

func TestGetMetersSearchFilter(t *testing.T) {
	srv := newTestServer(t)
	seedFilterableFleet(t, srv)
	router := NewRouter(srv)

	assertIDs(t, "q=109", meterIDsFrom(t, srv, router, "/api/meters?q=109"), []string{"M-109"})
	// Case-insensitive, and a prefix matches every meter that shares it.
	assertIDs(t, "q=m-1", meterIDsFrom(t, srv, router, "/api/meters?q=m-1"), []string{"M-109", "M-104"})
	assertIDs(t, "q=zzz", meterIDsFrom(t, srv, router, "/api/meters?q=zzz"), []string{})
	// status and q compose.
	assertIDs(t, "combined", meterIDsFrom(t, srv, router, "/api/meters?q=m-1&status=critical"), []string{"M-109"})
}

func TestGetMetersSortOrder(t *testing.T) {
	srv := newTestServer(t)
	seedFilterableFleet(t, srv)
	router := NewRouter(srv)

	// consumption: 2180 > 1100 > 1020 > (never analyzed, null -> last).
	assertIDs(t, "sort=consumption", meterIDsFrom(t, srv, router, "/api/meters?sort=consumption"),
		[]string{"M-109", "M-104", "M-200", "X-999"})
	// asc reverses it, and the null sorts first.
	assertIDs(t, "sort=consumption&order=asc", meterIDsFrom(t, srv, router, "/api/meters?sort=consumption&order=asc"),
		[]string{"X-999", "M-200", "M-104", "M-109"})
	// variation: 103.7 > 22.2 > 2 > null.
	assertIDs(t, "sort=variation", meterIDsFrom(t, srv, router, "/api/meters?sort=variation"),
		[]string{"M-109", "M-104", "M-200", "X-999"})
	// severity: HIGH > MEDIUM > sin anomalía (spec 03 line 69).
	assertIDs(t, "sort=severity", meterIDsFrom(t, srv, router, "/api/meters?sort=severity"),
		[]string{"M-109", "M-104", "M-200", "X-999"})
}

// TestSortMeterRowsSeverityTieBreaksOnPriorityScore isolates spec 03 line 69's
// "desempate por priority_score" — two meters with the same severity must be
// ordered by the anomaly's priority_score, which the JSON response itself
// doesn't carry.
func TestSortMeterRowsSeverityTieBreaksOnPriorityScore(t *testing.T) {
	rows := []meterListRow{
		{item: meterListItem{MeterID: "M-A"}, anomaly: &AnomalyRow{Severity: "HIGH", PriorityScore: 2.0}},
		{item: meterListItem{MeterID: "M-B"}, anomaly: &AnomalyRow{Severity: "HIGH", PriorityScore: 7.0}},
		{item: meterListItem{MeterID: "M-C"}, anomaly: &AnomalyRow{Severity: "LOW", PriorityScore: 9.0}},
		{item: meterListItem{MeterID: "M-D"}},
	}
	sortMeterRows(rows, "severity", "")
	want := []string{"M-B", "M-A", "M-C", "M-D"}
	for i, id := range want {
		if rows[i].item.MeterID != id {
			t.Fatalf("want %v, got %v (index %d)", want, rows, i)
		}
	}
}

// --- I2: GET /ai/analysis/:id full contract ---------------------------------

func TestGetAnalysisStatusExposesFullSpecContract(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO analyses (id, status, stage, progress, scope_meter_ids_json,
		data_from, data_to, baseline_from, baseline_to, started_at, finished_at, duration_ms,
		engine_version, readings_analyzed, meters_analyzed, anomalies_count, high_priority_count,
		avg_confidence, summary_message)
		VALUES (7, 'COMPLETED', 'RECOMMENDATION', 1, '["M-109"]',
		'2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z', '2026-09-01T00:00:00Z', '2026-09-07T23:00:00Z',
		'2026-09-15T00:00:00Z', '2026-09-15T00:00:03Z', 3120, 'v1', 4032, 12, 4, 2, 0.9,
		'4 anomalías detectadas · 2 requieren atención prioritaria')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/ai/analysis/7", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Assert on the raw JSON so a renamed/missing field is caught, not just
	// the Go struct round-trip.
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"id", "status", "stage", "progress", "scope", "started_at",
		"finished_at", "duration_ms", "engine_version", "readings_analyzed", "meters_analyzed", "summary"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("spec 03 field %q missing from GET /ai/analysis/:id", key)
		}
	}

	var resp analysisStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Scope.MeterIDs) != 1 || resp.Scope.MeterIDs[0] != "M-109" {
		t.Errorf("scope.meter_ids: want [M-109], got %v", resp.Scope.MeterIDs)
	}
	if resp.Scope.From != "2026-09-01T00:00:00Z" || resp.Scope.To != "2026-09-14T23:00:00Z" {
		t.Errorf("scope.from/to: got %q / %q", resp.Scope.From, resp.Scope.To)
	}
	if resp.Scope.BaselineFrom == nil || *resp.Scope.BaselineFrom != "2026-09-01T00:00:00Z" {
		t.Errorf("scope.baseline_from: got %v", resp.Scope.BaselineFrom)
	}
	if resp.Scope.BaselineTo == nil || *resp.Scope.BaselineTo != "2026-09-07T23:00:00Z" {
		t.Errorf("scope.baseline_to: got %v", resp.Scope.BaselineTo)
	}
	if resp.StartedAt != "2026-09-15T00:00:00Z" {
		t.Errorf("started_at: got %q", resp.StartedAt)
	}
	if resp.FinishedAt == nil || *resp.FinishedAt != "2026-09-15T00:00:03Z" {
		t.Errorf("finished_at: got %v", resp.FinishedAt)
	}
	if resp.DurationMs == nil || *resp.DurationMs != 3120 {
		t.Errorf("duration_ms: got %v", resp.DurationMs)
	}
	if resp.EngineVersion != "v1" {
		t.Errorf("engine_version: got %q", resp.EngineVersion)
	}
	if resp.ReadingsAnalyzed != 4032 || resp.MetersAnalyzed != 12 {
		t.Errorf("readings/meters analyzed: got %d / %d", resp.ReadingsAnalyzed, resp.MetersAnalyzed)
	}

	// A fleet-wide (no meter_ids) RUNNING analysis: meter_ids null,
	// finished_at/duration_ms null.
	srv.DB.Exec(`INSERT INTO analyses (id, status, stage, progress, scope_meter_ids_json,
		data_from, data_to, started_at, engine_version)
		VALUES (8, 'RUNNING', 'DETECTION', 0.3, 'null',
		'2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z', '2026-09-15T00:00:00Z', 'v1')`)
	req2 := authedRequest(t, srv, http.MethodGet, "/api/ai/analysis/8", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var running analysisStatusResponse
	json.Unmarshal(rec2.Body.Bytes(), &running)
	if running.Scope.MeterIDs != nil {
		t.Errorf("fleet-wide scope must render meter_ids null, got %v", running.Scope.MeterIDs)
	}
	if running.FinishedAt != nil || running.DurationMs != nil {
		t.Errorf("a RUNNING analysis must have null finished_at/duration_ms, got %v / %v",
			running.FinishedAt, running.DurationMs)
	}
}

// TestFinishAnalysisWritesRealDurationAndReadingsCount covers the write side
// of I2: finishAnalysis used to hardcode duration_ms and readings_analyzed to 0.
func TestFinishAnalysisWritesRealDurationAndReadingsCount(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	runs := []meterRun{
		{Result: engine.MeterResult{MeterID: "M-101"}, ReadingsCount: 336},
		{Result: engine.MeterResult{MeterID: "M-109", HasAnomaly: true, Severity: engine.High, Confidence: 0.96}, ReadingsCount: 336},
	}
	finishAnalysis(db, 1, runs, time.Now().Add(-1500*time.Millisecond))

	var durationMs, readings, meters int64
	db.QueryRow(`SELECT duration_ms, readings_analyzed, meters_analyzed FROM analyses WHERE id = 1`).
		Scan(&durationMs, &readings, &meters)
	if durationMs < 1000 {
		t.Errorf("duration_ms must reflect real elapsed time (>= ~1500ms), got %d", durationMs)
	}
	if readings != 672 {
		t.Errorf("readings_analyzed must sum the per-meter counts (672), got %d", readings)
	}
	if meters != 2 {
		t.Errorf("meters_analyzed: want 2, got %d", meters)
	}
}

// --- I4: fourth vigencia site ------------------------------------------------

// TestMeterAnomaliesScopeCurrentUsesVigenteAnalysis covers I4: a meter that was
// anomalous in an old analysis and re-analyzed as normal must report NO current
// anomaly, instead of the stale one. scope=history still shows it.
func TestMeterAnomaliesScopeCurrentUsesVigenteAnalysis(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'OK', ?)`, time.Now().Format(time.RFC3339))

	// Analysis 1: found an anomaly.
	srv.DB.Exec(`INSERT INTO analyses (id, status, data_from, data_to, finished_at)
		VALUES (1, 'COMPLETED', '2026-09-01T00:00:00Z', '2026-09-07T23:00:00Z', '2026-09-08T00:00:00Z')`)
	srv.DB.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, hourly_profile_json) VALUES (1, 'M-109', ?)`,
		hourlyProfileJSONFixture())
	srv.DB.Exec(`INSERT INTO anomalies (analysis_id, meter_id, type, severity, confidence, priority_score,
		period_from, period_to, status, created_at, updated_at)
		VALUES (1, 'M-109', 'REAL_ANOMALY', 'HIGH', 0.96, 5.8,
		'2026-09-01T00:00:00Z', '2026-09-07T23:00:00Z', 'OPEN', '2026-09-08T00:00:00Z', '2026-09-08T00:00:00Z')`)

	// Analysis 2 (the vigente one): INCLUDED M-109 but found it normal, so it
	// has a meter_baselines row and NO anomalies row.
	srv.DB.Exec(`INSERT INTO analyses (id, status, data_from, data_to, finished_at)
		VALUES (2, 'COMPLETED', '2026-09-08T00:00:00Z', '2026-09-14T23:00:00Z', '2026-09-15T00:00:00Z')`)
	srv.DB.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, hourly_profile_json) VALUES (2, 'M-109', ?)`,
		hourlyProfileJSONFixture())

	router := NewRouter(srv)

	// Default scope (=current) must be empty: the vigente analysis found nothing.
	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/anomalies", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var current struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &current)
	if len(current.Items) != 0 {
		t.Errorf("I4: scope=current must use the latest COMPLETED analysis that INCLUDED the meter (which found nothing), got %d stale items: %s",
			len(current.Items), rec.Body.String())
	}

	// scope=history still returns the old anomaly.
	req2 := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/anomalies?scope=history", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var history struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &history)
	if len(history.Items) != 1 {
		t.Errorf("scope=history must still return past anomalies, got %d", len(history.Items))
	}

	// A never-analyzed meter returns an empty list, not an error.
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-999', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	req3 := authedRequest(t, srv, http.MethodGet, "/api/meters/M-999/anomalies", nil)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("never-analyzed meter: expected 200, got %d", rec3.Code)
	}
	var none struct {
		Items []struct{} `json:"items"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &none); err != nil {
		t.Fatalf("never-analyzed meter: invalid JSON %s", rec3.Body.String())
	}
	if len(none.Items) != 0 {
		t.Errorf("never-analyzed meter must return an empty list, got %d", len(none.Items))
	}
}

// --- I5: 405 must keep the JSON error contract ------------------------------

func TestMethodNotAllowedReturnsJSONError(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	// /api/auth/login exists but only accepts POST.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("405 must carry a JSON Content-Type, got %q", ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("405 body must be JSON, got %q: %v", rec.Body.String(), err)
	}
	if body.Error == "" {
		t.Errorf(`405 must follow spec 03's {"error":"mensaje"} contract, got %q`, rec.Body.String())
	}
}

// --- I3: dashboard consumption is a raw-readings fact -----------------------

func TestDashboardConsumptionIndependentOfAnalysis(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-101', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	// No analyses row at all — the "Sin análisis" first demo screen.
	seedTwoWeeksOfReadings(t, srv.DB, "M-101", func(h int) float64 { return 10 })
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		TotalConsumptionKWh float64                `json:"total_consumption_kwh"`
		ByMeter             []dashboardByMeterItem `json:"by_meter"`
		LastAnalysis        interface{}            `json:"last_analysis"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.LastAnalysis != nil {
		t.Fatalf("precondition: no analysis expected, got %v", resp.LastAnalysis)
	}
	// 14 days x 24 hourly readings x 10 kWh, upper bound inclusive.
	const want = 14 * 24 * 10
	if resp.TotalConsumptionKWh != want {
		t.Errorf("I3: total_consumption_kwh must sum raw readings (%v) even with no analysis, got %v",
			float64(want), resp.TotalConsumptionKWh)
	}
	if len(resp.ByMeter) != 1 || resp.ByMeter[0].ConsumptionKWh != want {
		t.Errorf("I3: by_meter consumption must sum raw readings (%v), got %+v", float64(want), resp.ByMeter)
	}
	if len(resp.ByMeter) == 1 && resp.ByMeter[0].SharePct != 100 {
		t.Errorf("share_pct with a single meter must be 100, got %v", resp.ByMeter[0].SharePct)
	}
}
