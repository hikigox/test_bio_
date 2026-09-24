package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetDashboardSummaryReturnsDataRangeAndByMeter(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	seedTwoWeeksOfReadings(t, srv.DB, "M-109", func(h int) float64 { return 10 })
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		DataRange struct{ From, To string } `json:"data_range"`
		Period    struct{ From, To string } `json:"period"`
		ByMeter   []struct {
			MeterID        string  `json:"meter_id"`
			ConsumptionKWh float64 `json:"consumption_kwh"`
			SharePct       float64 `json:"share_pct"`
			Status         string  `json:"status"`
			Severity       string  `json:"severity"`
		} `json:"by_meter"`
		Anomalies     int     `json:"anomalies"`
		HighPriority  int     `json:"high_priority"`
		AvgConfidence float64 `json:"avg_confidence"`
		LastAnalysis  *struct {
			At     string `json:"at"`
			Status string `json:"status"`
		} `json:"last_analysis"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DataRange.From == "" || resp.DataRange.To == "" {
		t.Fatal("expected non-empty data_range")
	}
	if len(resp.ByMeter) == 0 {
		t.Fatal("expected at least one meter in by_meter")
	}
	if resp.Anomalies != 1 || resp.HighPriority != 1 {
		t.Fatalf("expected 1 anomaly / 1 high priority (M-109 REAL_ANOMALY HIGH), got anomalies=%d high_priority=%d", resp.Anomalies, resp.HighPriority)
	}
	m109 := resp.ByMeter[0]
	if m109.MeterID != "M-109" {
		t.Fatalf("expected M-109 first in by_meter, got %+v", resp.ByMeter)
	}
	if m109.Severity != "HIGH" {
		t.Fatalf("expected M-109 severity HIGH, got %q", m109.Severity)
	}
	if m109.SharePct != 100 {
		t.Fatalf("expected M-109 share_pct 100 (only meter with consumption), got %v", m109.SharePct)
	}
	if resp.LastAnalysis == nil || resp.LastAnalysis.Status != "COMPLETED" {
		t.Fatalf("expected last_analysis with status COMPLETED, got %+v", resp.LastAnalysis)
	}
}

func TestGetDashboardSummaryPeriodReflectsFromToQueryParams(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	seedTwoWeeksOfReadings(t, srv.DB, "M-109", func(h int) float64 { return 10 })
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/dashboard/summary?from=2026-09-01T00:00:00Z&to=2026-09-02T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp struct {
		Period struct{ From, To string } `json:"period"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Period.From != "2026-09-01T00:00:00Z" || resp.Period.To != "2026-09-02T00:00:00Z" {
		t.Fatalf("expected period to echo query from/to, got %+v", resp.Period)
	}
}

func TestGetDashboardSummaryWithNoAnalysesReturnsZeroAnomaliesAndNilLastAnalysis(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-201', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Anomalies    int         `json:"anomalies"`
		LastAnalysis interface{} `json:"last_analysis"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Anomalies != 0 {
		t.Fatalf("expected 0 anomalies with no analyses, got %d", resp.Anomalies)
	}
	if resp.LastAnalysis != nil {
		t.Fatalf("expected nil last_analysis with no analyses, got %v", resp.LastAnalysis)
	}
}
