package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGetAnomaliesFiltersByNonOverlappingRangeReturnsEmpty(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB) // anomalía activa 2026-09-08 -> 2026-09-14
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies?from=2026-08-01T00:00:00Z&to=2026-08-31T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp struct {
		Items []interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 0 {
		t.Fatalf("expected empty items for non-overlapping range, got %d", len(resp.Items))
	}
}

func TestGetAnomaliesDefaultsToVigenteOnly(t *testing.T) {
	// Spec 03: "GET /anomalies ... Vigentes por defecto". A meter that had an
	// anomaly in an old analysis but normalized in the latest COMPLETED
	// analysis must not appear by default.
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB) // analysis #1, M-109 anomalous
	res, err := srv.DB.Exec(`INSERT INTO analyses (status, data_from, data_to, finished_at)
		VALUES ('COMPLETED', '2026-09-15T00:00:00Z', '2026-09-28T23:00:00Z', '2026-09-28T23:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	newAnalysisID, _ := res.LastInsertId()
	if _, err := srv.DB.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, method, hourly_profile_json, baseline_kwh, actual_kwh, variation_pct)
		VALUES (?, 'M-109', 'HOURLY_MEDIAN', ?, 1070, 1100, 2.8)`, newAnalysisID, hourlyProfileJSONFixture()); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp struct {
		Items []interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 0 {
		t.Fatalf("expected empty items (M-109 normalized in latest analysis), got %d: %s", len(resp.Items), rec.Body.String())
	}
}

func TestGetAnomaliesFiltersByAnalysisIDBypassesVigenteDefault(t *testing.T) {
	// analysis_id= lets the caller inspect a specific (possibly stale)
	// analysis's anomalies explicitly, per spec 03's query param table.
	srv := newTestServer(t)
	analysisID := setupAnalysisWithAnomaly(t, srv.DB)
	res, _ := srv.DB.Exec(`INSERT INTO analyses (status, data_from, data_to, finished_at)
		VALUES ('COMPLETED', '2026-09-15T00:00:00Z', '2026-09-28T23:00:00Z', '2026-09-28T23:00:00Z')`)
	newAnalysisID, _ := res.LastInsertId()
	srv.DB.Exec(`INSERT INTO meter_baselines (analysis_id, meter_id, method, hourly_profile_json, baseline_kwh, actual_kwh, variation_pct)
		VALUES (?, 'M-109', 'HOURLY_MEDIAN', ?, 1070, 1100, 2.8)`, newAnalysisID, hourlyProfileJSONFixture())
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies?analysis_id="+strconv.FormatInt(analysisID, 10), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp struct {
		Items []interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item for explicit analysis_id filter, got %d: %s", len(resp.Items), rec.Body.String())
	}
}

func TestGetAnomaliesFiltersByMeterID(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies?meter_id=M-999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp struct {
		Items []interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 items for unrelated meter_id filter, got %d", len(resp.Items))
	}
}

func TestPatchAnomalyRejectsInvalidStatus(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	body, _ := json.Marshal(map[string]string{"status": "NOT_A_REAL_STATUS"})
	req := authedRequest(t, srv, http.MethodPatch, "/api/anomalies/1", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchAnomalyAcceptsValidStatus(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	body, _ := json.Marshal(map[string]string{"status": "INVESTIGATING"})
	req := authedRequest(t, srv, http.MethodPatch, "/api/anomalies/1", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var status string
	srv.DB.QueryRow(`SELECT status FROM anomalies WHERE id = 1`).Scan(&status)
	if status != "INVESTIGATING" {
		t.Fatalf("expected status updated to INVESTIGATING, got %v", status)
	}
}

func TestPatchAnomalyReturns404ForUnknownID(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	body, _ := json.Marshal(map[string]string{"status": "INVESTIGATING"})
	req := authedRequest(t, srv, http.MethodPatch, "/api/anomalies/999", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown anomaly id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetAnomalyByIDIncludesEvidenceAndSignals(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB)
	srv.DB.Exec(`UPDATE anomalies SET evidence_json = ? WHERE id = 1`, bytes.NewBufferString(`{"decision_path":["no_data_quality_issue"]}`).String())
	srv.DB.Exec(`INSERT INTO anomaly_signals (anomaly_id, signal, observed, threshold, detail) VALUES (1, 'PERSISTENT_SHIFT', 103.7, 25, 'sostenido')`)
	srv.DB.Exec(`INSERT INTO anomaly_variables (anomaly_id, variable, baseline, actual, delta_pct, changed) VALUES (1, 'current_a', 10, 20, 100, 1)`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies/1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		ID            int64                    `json:"id"`
		AnalysisID    int64                    `json:"analysis_id"`
		MeterID       string                   `json:"meter_id"`
		PriorityRank  int                      `json:"priority_rank"`
		DurationHours float64                  `json:"duration_hours"`
		ActiveFrom    string                   `json:"active_from"`
		ActiveTo      string                   `json:"active_to"`
		Signals       []map[string]interface{} `json:"signals"`
		Variables     []map[string]interface{} `json:"variables"`
		Evidence      struct {
			DecisionPath []string `json:"decision_path"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AnalysisID == 0 {
		t.Fatal("expected non-zero analysis_id")
	}
	if resp.MeterID != "M-109" {
		t.Fatalf("expected meter_id M-109, got %q", resp.MeterID)
	}
	if len(resp.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(resp.Signals))
	}
	if len(resp.Variables) != 1 {
		t.Fatalf("expected 1 variable, got %d", len(resp.Variables))
	}
	if len(resp.Evidence.DecisionPath) != 1 || resp.Evidence.DecisionPath[0] != "no_data_quality_issue" {
		t.Fatalf("expected evidence.decision_path from evidence_json, got %+v", resp.Evidence)
	}
	if resp.ActiveFrom == "" || resp.ActiveTo == "" {
		t.Fatal("expected non-empty active_from/active_to")
	}
	if resp.DurationHours <= 0 {
		t.Fatalf("expected positive duration_hours, got %v", resp.DurationHours)
	}
}

func TestGetAnomalyByIDReturns404ForUnknownID(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies/999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
