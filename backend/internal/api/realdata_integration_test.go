package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"energy-management/internal/store"
)

// findRepoDataDir walks up from the test's working directory looking for the
// real dataset (data/readings.csv at the repo root). Returns "" if not found,
// so the test skips cleanly where the dataset isn't present. Mirrors
// cmd/eval/main_test.go's helper of the same name (different package).
func findRepoDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "data", "readings.csv")); err == nil {
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

// newRealDataServer builds a Server backed by an in-memory DB loaded with the
// REAL data/readings.csv + data/events.csv, plus the meters rows and demo user
// that cmd/api's seedIfEmpty creates at startup. This is the same state the
// live binary is in right after `make run`.
func newRealDataServer(t *testing.T) *Server {
	t.Helper()
	dataDir := findRepoDataDir(t)
	if dataDir == "" {
		t.Skip("real dataset (data/readings.csv, data/events.csv) not found relative to repo root")
	}
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := store.LoadReadingsCSV(db, filepath.Join(dataDir, "readings.csv")); err != nil {
		t.Fatalf("load readings: %v", err)
	}
	if _, err := store.LoadEventsCSV(db, filepath.Join(dataDir, "events.csv")); err != nil {
		t.Fatalf("load events: %v", err)
	}

	rows, err := db.Query(`SELECT DISTINCT meter_id FROM readings ORDER BY meter_id`)
	if err != nil {
		t.Fatalf("meter ids: %v", err)
	}
	var meterIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		meterIDs = append(meterIDs, id)
	}
	rows.Close()
	for _, id := range meterIDs {
		if _, err := db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES (?, 'UNKNOWN', ?)`,
			id, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("seed meter %s: %v", id, err)
		}
	}

	srv := &Server{DB: db, JWTSecret: "test-secret"}
	seedDemoUser(t, srv, "demo@energy.local", "demo1234")
	return srv
}

type realDataClient struct {
	t      *testing.T
	router http.Handler
	token  string
}

func (c *realDataClient) get(path string, out interface{}) {
	c.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("GET %s: expected 200, got %d: %s", path, rec.Code, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			c.t.Fatalf("GET %s: decoding response: %v (%s)", path, err, rec.Body.String())
		}
	}
}

// TestRealDataAnalysisEndToEnd runs the actual pipeline over the actual
// dataset through the actual HTTP router — login, POST /ai/analyze, poll to
// COMPLETED, read the results back — and asserts on VALUES THE PIPELINE ITSELF
// WROTE. Every other test in this package hand-inserts fixture rows, which
// structurally cannot catch a defect where the pipeline persists the wrong
// value, because the fixture supplies the right one. This test is the
// regression lock for exactly that class of defect:
//
//	C1  every analyzed meter leaves UNKNOWN; the 8 meters spec 05's acceptance
//	    table lists as "Resto · Sin señales" end up OK.
//	C2  every anomaly's active window has real length (active_to > active_from,
//	    duration_hours > 0), so the ?from=&to= overlap filter still returns an
//	    anomaly that is ongoing at the end of the data (M-109).
//	C3  a meter with a non-null analysis_id also has a non-null consumption_kwh.
//
// Expected values are transcribed from spec 05's acceptance table, never from
// expected_results.csv (spec 00 regla 1 — cmd/eval's --expected flag is the
// only place that file is ever read).
func TestRealDataAnalysisEndToEnd(t *testing.T) {
	srv := newRealDataServer(t)
	router := NewRouter(srv)

	// 1. login
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		bytes.NewReader([]byte(`{"email":"demo@energy.local","password":"demo1234"}`)))
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decoding login: %v", err)
	}
	c := &realDataClient{t: t, router: router, token: loginResp.Token}

	// 2. POST /ai/analyze with no body -> whole fleet, whole period (spec 03).
	analyzeReq := httptest.NewRequest(http.MethodPost, "/api/ai/analyze", nil)
	analyzeReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	analyzeRec := httptest.NewRecorder()
	router.ServeHTTP(analyzeRec, analyzeReq)
	if analyzeRec.Code != http.StatusAccepted {
		t.Fatalf("analyze: expected 202, got %d: %s", analyzeRec.Code, analyzeRec.Body.String())
	}
	var analyzeResp struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(analyzeRec.Body.Bytes(), &analyzeResp)

	// 3. poll to completion
	waitForAnalysisDone(t, srv.DB, analyzeResp.ID, 30*time.Second)

	var status analysisStatusResponse
	c.get("/api/ai/analysis/"+strconv.FormatInt(analyzeResp.ID, 10), &status)
	if status.Status != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %q", status.Status)
	}
	// I2: the pipeline must report real figures, not the hardcoded zeros
	// finishAnalysis used to write.
	if status.MetersAnalyzed != 12 {
		t.Errorf("meters_analyzed: want 12, got %d", status.MetersAnalyzed)
	}
	if status.ReadingsAnalyzed != 4032 {
		t.Errorf("readings_analyzed: want 4032 (12 meters x 336 hourly readings), got %d", status.ReadingsAnalyzed)
	}
	if status.DurationMs == nil || *status.DurationMs <= 0 {
		t.Errorf("duration_ms must be a real elapsed time, got %v", status.DurationMs)
	}
	if status.StartedAt == "" || status.FinishedAt == nil || *status.FinishedAt == "" {
		t.Errorf("started_at/finished_at must be populated, got %q / %v", status.StartedAt, status.FinishedAt)
	}
	if status.EngineVersion == "" {
		t.Error("engine_version must be populated")
	}
	if status.Scope.From == "" || status.Scope.To == "" {
		t.Errorf("scope.from/to must be populated, got %q / %q", status.Scope.From, status.Scope.To)
	}
	if status.Summary.Anomalies != 4 || status.Summary.Priority != 2 {
		t.Errorf("spec 05: want 4 anomalies / 2 priority, got %d / %d",
			status.Summary.Anomalies, status.Summary.Priority)
	}

	// 4. GET /meters — C1 and C3.
	var meters struct {
		Items []meterListItem `json:"items"`
	}
	c.get("/api/meters", &meters)
	if len(meters.Items) != 12 {
		t.Fatalf("expected 12 meters, got %d", len(meters.Items))
	}
	byMeter := map[string]meterListItem{}
	for _, it := range meters.Items {
		byMeter[it.MeterID] = it
		// C1: analysis ran and covered every meter, so none may stay UNKNOWN.
		if it.Status == "UNKNOWN" {
			t.Errorf("C1 %s: still UNKNOWN after a completed fleet analysis", it.MeterID)
		}
		// C3: analysis_id non-null implies the analysis-derived figures are
		// non-null too (spec 03 line 82 only allows null for never-analyzed).
		if it.AnalysisID != nil {
			if it.ConsumptionKWh == nil || it.BaselineKWh == nil || it.VariationPct == nil {
				t.Errorf("C3 %s: analysis_id=%d but consumption=%v baseline=%v variation=%v",
					it.MeterID, *it.AnalysisID, it.ConsumptionKWh, it.BaselineKWh, it.VariationPct)
			}
			if it.AnalysisPeriod == nil || it.AnalysisPeriod.From == "" {
				t.Errorf("C3 %s: analysis_id set but analysis_period is %v", it.MeterID, it.AnalysisPeriod)
			}
		} else {
			t.Errorf("%s: expected a non-null analysis_id after a fleet analysis", it.MeterID)
		}
	}

	// spec 03 line 67 / spec 05's acceptance table.
	wantStatus := map[string]string{
		"M-109": "CRITICAL", // REAL_ANOMALY · HIGH
		"M-112": "ALERT",    // DATA_QUALITY · HIGH
		"M-104": "ALERT",    // EXPLAINABLE_ANOMALY · MEDIUM
		"M-106": "OK",       // FALSE_POSITIVE · LOW -> no escalar
	}
	for id, want := range wantStatus {
		if got := byMeter[id].Status; got != want {
			t.Errorf("%s: want status %s, got %s", id, want, got)
		}
	}
	// C1's headline symptom: spec 05's "Resto · Sin señales" meters were all
	// left at UNKNOWN because only insertAnomaly wrote a status.
	for _, id := range []string{"M-101", "M-102", "M-103", "M-105", "M-107", "M-108", "M-110", "M-111"} {
		it, ok := byMeter[id]
		if !ok {
			t.Errorf("%s: missing from GET /meters", id)
			continue
		}
		if it.Status != "OK" {
			t.Errorf("C1 %s: analyzed with no anomaly must be OK (spec 03), got %s", id, it.Status)
		}
		if it.Anomaly != nil {
			t.Errorf("%s: expected no anomaly, got %+v", id, it.Anomaly)
		}
	}

	// 5. GET /anomalies — C2: every active window must have real length.
	var anomalies struct {
		Items []anomalyListItem `json:"items"`
	}
	c.get("/api/anomalies", &anomalies)
	if len(anomalies.Items) != 4 {
		t.Fatalf("spec 05: expected 4 vigente anomalies, got %d", len(anomalies.Items))
	}
	if anomalies.Items[0].MeterID != "M-109" || anomalies.Items[0].PriorityRank != 1 {
		t.Errorf("spec 05: M-109 must be #1 in priority, got %s rank %d",
			anomalies.Items[0].MeterID, anomalies.Items[0].PriorityRank)
	}
	for _, a := range anomalies.Items {
		af, err := time.Parse(time.RFC3339, a.ActiveFrom)
		if err != nil {
			t.Fatalf("%s: bad active_from %q: %v", a.MeterID, a.ActiveFrom, err)
		}
		at, err := time.Parse(time.RFC3339, a.ActiveTo)
		if err != nil {
			t.Fatalf("%s: bad active_to %q: %v", a.MeterID, a.ActiveTo, err)
		}
		if !at.After(af) {
			t.Errorf("C2 %s: active window has zero length (%s .. %s) — period_to is being sourced from the baseline window, not the analyzed period",
				a.MeterID, a.ActiveFrom, a.ActiveTo)
		}
		var detail anomalyDetail
		c.get("/api/anomalies/"+strconv.FormatInt(a.ID, 10), &detail)
		if detail.DurationHours <= 0 {
			t.Errorf("C2 %s: duration_hours must be > 0, got %v", a.MeterID, detail.DurationHours)
		}
	}

	// 6. C2's user-visible symptom: M-109's anomaly runs through the end of the
	// data, so filtering to the last two days must still return it.
	var dataRange struct {
		DataRange struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"data_range"`
		TotalConsumptionKWh float64 `json:"total_consumption_kwh"`
	}
	c.get("/api/dashboard/summary", &dataRange)
	end, err := time.Parse(time.RFC3339, dataRange.DataRange.To)
	if err != nil {
		t.Fatalf("bad data_range.to %q: %v", dataRange.DataRange.To, err)
	}
	lastTwoDays := end.Add(-48 * time.Hour)
	var filtered struct {
		Items []anomalyListItem `json:"items"`
	}
	c.get("/api/anomalies?from="+lastTwoDays.Format(time.RFC3339)+"&to="+end.Format(time.RFC3339), &filtered)
	found := false
	for _, a := range filtered.Items {
		if a.MeterID == "M-109" {
			found = true
		}
	}
	if !found {
		t.Errorf("C2: M-109's anomaly is ongoing through the end of the data, so a %s..%s filter must still return it; got %d items",
			lastTwoDays.Format(time.RFC3339), end.Format(time.RFC3339), len(filtered.Items))
	}
}

// TestRealDataDashboardConsumptionBeforeAnyAnalysis locks down I3: the very
// first demo screen (spec 05 step 1, "Dashboard (12 medidores, 'Sin análisis')")
// must show real consumption, because consumption is a raw-readings fact — not
// something an AI analysis produces.
func TestRealDataDashboardConsumptionBeforeAnyAnalysis(t *testing.T) {
	srv := newRealDataServer(t)
	router := NewRouter(srv)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		bytes.NewReader([]byte(`{"email":"demo@energy.local","password":"demo1234"}`)))
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	var loginResp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(loginRec.Body.Bytes(), &loginResp)
	c := &realDataClient{t: t, router: router, token: loginResp.Token}

	var summary struct {
		Meters              int     `json:"meters"`
		TotalConsumptionKWh float64 `json:"total_consumption_kwh"`
		LastAnalysis        *struct {
			Status string `json:"status"`
		} `json:"last_analysis"`
		ByMeter []dashboardByMeterItem `json:"by_meter"`
	}
	c.get("/api/dashboard/summary", &summary)

	if summary.LastAnalysis != nil {
		t.Fatalf("precondition: no analysis should exist yet, got %+v", summary.LastAnalysis)
	}
	if summary.Meters != 12 {
		t.Errorf("expected 12 meters, got %d", summary.Meters)
	}
	if summary.TotalConsumptionKWh <= 0 {
		t.Errorf("I3: total_consumption_kwh must be real before any analysis, got %v", summary.TotalConsumptionKWh)
	}
	for _, m := range summary.ByMeter {
		if m.ConsumptionKWh <= 0 {
			t.Errorf("I3 %s: consumption_kwh must be real before any analysis, got %v", m.MeterID, m.ConsumptionKWh)
		}
		if m.SharePct <= 0 {
			t.Errorf("I3 %s: share_pct must be real before any analysis, got %v", m.MeterID, m.SharePct)
		}
		if m.Status != "UNKNOWN" {
			t.Errorf("%s: never analyzed yet, status must be UNKNOWN, got %s", m.MeterID, m.Status)
		}
	}
}
