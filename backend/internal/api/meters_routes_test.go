package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetMetersListsAnalyzedAndUnknownMeters(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-999', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			MeterID      string          `json:"meter_id"`
			VariationPct *float64        `json:"variation_pct"`
			Status       string          `json:"status"`
			Anomaly      *anomalySummary `json:"anomaly"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, rec.Body.String())
	}

	var m109, m999 *struct {
		MeterID      string          `json:"meter_id"`
		VariationPct *float64        `json:"variation_pct"`
		Status       string          `json:"status"`
		Anomaly      *anomalySummary `json:"anomaly"`
	}
	for i := range resp.Items {
		if resp.Items[i].MeterID == "M-109" {
			m109 = &resp.Items[i]
		}
		if resp.Items[i].MeterID == "M-999" {
			m999 = &resp.Items[i]
		}
	}
	if m109 == nil || m109.VariationPct == nil || *m109.VariationPct != 103.7 {
		t.Fatalf("expected M-109 with variation_pct 103.7, got %+v", m109)
	}
	if m109.Anomaly == nil {
		t.Fatalf("expected M-109 to have a non-nil anomaly summary, got %+v", m109)
	}
	if m999 == nil || m999.VariationPct != nil || m999.Status != "UNKNOWN" {
		t.Fatalf("expected M-999 with null variation_pct and UNKNOWN status, got %+v", m999)
	}
	if m999.Anomaly != nil {
		t.Fatalf("expected M-999 (never analyzed) to have a nil anomaly, got %+v", m999.Anomaly)
	}
}

func TestGetMetersInRangeReflectsDateFilterOverlap(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	// The anomaly's active window is [2026-09-08, 2026-09-14] (change_point_at .. period_to).
	// A range entirely before it should not overlap => in_range: false.
	req := authedRequest(t, srv, http.MethodGet, "/api/meters?from=2026-08-01T00:00:00Z&to=2026-08-31T23:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			MeterID string          `json:"meter_id"`
			Anomaly *anomalySummary `json:"anomaly"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var m109 *anomalySummary
	for _, it := range resp.Items {
		if it.MeterID == "M-109" {
			m109 = it.Anomaly
		}
	}
	if m109 == nil {
		t.Fatalf("expected M-109 anomaly present regardless of range")
	}
	if m109.InRange {
		t.Fatalf("expected in_range=false for a non-overlapping range, got true")
	}

	// A range overlapping the active window should give in_range: true.
	req2 := authedRequest(t, srv, http.MethodGet, "/api/meters?from=2026-09-01T00:00:00Z&to=2026-09-30T23:00:00Z", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var resp2 struct {
		Items []struct {
			MeterID string          `json:"meter_id"`
			Anomaly *anomalySummary `json:"anomaly"`
		} `json:"items"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &resp2)
	var m109b *anomalySummary
	for _, it := range resp2.Items {
		if it.MeterID == "M-109" {
			m109b = it.Anomaly
		}
	}
	if m109b == nil || !m109b.InRange {
		t.Fatalf("expected in_range=true for an overlapping range, got %+v", m109b)
	}
}

func TestGetMeterByIDReturnsDetail(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var item meterListItem
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if item.MeterID != "M-109" || item.VariationPct == nil || *item.VariationPct != 103.7 {
		t.Fatalf("expected M-109 detail with variation_pct 103.7, got %+v", item)
	}
}

func TestGetMeterByIDUnknownMeterReturns404(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-DOES-NOT-EXIST", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetMeterByIDNeverAnalyzedHasNullAnomalyAndVariation(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-999', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var item meterListItem
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if item.VariationPct != nil || item.Anomaly != nil || item.ConsumptionKWh != nil || item.BaselineKWh != nil {
		t.Fatalf("expected all-nil analysis fields for never-analyzed meter, got %+v", item)
	}
}

func TestGetMeterReadingsReturnsPointsInRange(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	srv.DB.Exec(`INSERT INTO readings (meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
		VALUES ('M-109', '2026-09-01T05:00:00Z', 20, 220, 10, 0.95, 'OK')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/readings?from=2026-09-01T00:00:00Z&to=2026-09-02T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Timestamp      string  `json:"timestamp"`
			ConsumptionKWh float64 `json:"consumption_kwh"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].ConsumptionKWh != 20 {
		t.Fatalf("expected 1 reading with consumption_kwh 20, got %+v", resp.Items)
	}
}

func TestGetMeterEventsReturnsEvents(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	srv.DB.Exec(`INSERT INTO events (meter_id, timestamp, type, description) VALUES ('M-109', '2026-09-08T00:00:00Z', 'MAINTENANCE', 'scheduled check')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Type string `json:"type"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].Type != "MAINTENANCE" {
		t.Fatalf("expected 1 MAINTENANCE event, got %+v", resp.Items)
	}
}

func TestGetMeterAnomaliesDefaultsToLatest(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/anomalies", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Type string `json:"type"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].Type != "REAL_ANOMALY" {
		t.Fatalf("expected 1 REAL_ANOMALY anomaly, got %+v", resp.Items)
	}
}

// --- Fix-round tests (spec 03 gaps found in code review) ---

func TestGetMeterByIDIncludesBaselineCurrentVIPAndLatestAnomaly(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	// A reading inside the anomaly's period (2026-09-01..2026-09-14) so
	// current_voltage/current/power_factor have a real, known average.
	srv.DB.Exec(`INSERT INTO readings (meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
		VALUES ('M-109', '2026-09-10T00:00:00Z', 20, 230, 12, 0.90, 'OK')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var detail meterDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if detail.Baseline == nil {
		t.Fatal("expected non-nil baseline block")
	}
	if len(detail.Baseline.HourlyProfile) != 24 {
		t.Fatalf("expected 24-hour baseline profile, got %d entries", len(detail.Baseline.HourlyProfile))
	}
	if detail.Baseline.Voltage != 220 || detail.Baseline.Current != 10 || detail.Baseline.PowerFactor != 0.95 {
		t.Fatalf("expected baseline V/I/PF 220/10/0.95, got %+v", detail.Baseline)
	}
	if detail.Baseline.ChangePointAt == nil || *detail.Baseline.ChangePointAt != "2026-09-08T00:00:00Z" {
		t.Fatalf("expected baseline change_point_at 2026-09-08T00:00:00Z, got %+v", detail.Baseline.ChangePointAt)
	}

	if detail.CurrentVoltage == nil || *detail.CurrentVoltage != 230 {
		t.Fatalf("expected current_voltage 230, got %+v", detail.CurrentVoltage)
	}
	if detail.CurrentCurrent == nil || *detail.CurrentCurrent != 12 {
		t.Fatalf("expected current_current 12, got %+v", detail.CurrentCurrent)
	}
	if detail.CurrentPowerFactor == nil || *detail.CurrentPowerFactor != 0.90 {
		t.Fatalf("expected current_power_factor 0.90, got %+v", detail.CurrentPowerFactor)
	}

	if detail.LatestAnomaly == nil || detail.LatestAnomaly.Type != "REAL_ANOMALY" {
		t.Fatalf("expected latest_anomaly REAL_ANOMALY, got %+v", detail.LatestAnomaly)
	}
	if detail.Anomaly == nil || detail.Anomaly.Type != "REAL_ANOMALY" {
		t.Fatalf("expected list-item anomaly to still be present on detail, got %+v", detail.Anomaly)
	}
}

func TestGetMeterByIDRespectsFromToRange(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	// A range entirely before the anomaly's active window (change_point_at
	// 2026-09-08 .. period_to 2026-09-14) must not overlap.
	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109?from=2026-08-01T00:00:00Z&to=2026-08-31T23:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var detail meterDetail
	json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.Anomaly == nil {
		t.Fatalf("expected anomaly still present regardless of range")
	}
	if detail.Anomaly.InRange {
		t.Fatalf("expected in_range=false for a non-overlapping ?from=&to=, got true")
	}

	req2 := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109?from=2026-09-01T00:00:00Z&to=2026-09-30T23:00:00Z", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var detail2 meterDetail
	json.Unmarshal(rec2.Body.Bytes(), &detail2)
	if detail2.Anomaly == nil || !detail2.Anomaly.InRange {
		t.Fatalf("expected in_range=true for an overlapping ?from=&to=, got %+v", detail2.Anomaly)
	}
}

func TestGetMeterEventsRespectsFromToRange(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	srv.DB.Exec(`INSERT INTO events (meter_id, timestamp, type, description) VALUES ('M-109', '2026-09-08T00:00:00Z', 'MAINTENANCE', 'in range')`)
	srv.DB.Exec(`INSERT INTO events (meter_id, timestamp, type, description) VALUES ('M-109', '2026-06-01T00:00:00Z', 'MAINTENANCE', 'out of range')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/events?from=2026-09-01T00:00:00Z&to=2026-09-14T23:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Description string `json:"description"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].Description != "in range" {
		t.Fatalf("expected only the in-range event, got %+v", resp.Items)
	}
}

func TestGetMeterAnomaliesRespectsFromToViaOverlap(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	// The anomaly's active window is [2026-09-08, 2026-09-14]. A
	// non-overlapping range should filter it out even with scope=history.
	req := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/anomalies?scope=history&from=2026-01-01T00:00:00Z&to=2026-01-31T23:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Type string `json:"type"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 anomalies for a non-overlapping range, got %+v", resp.Items)
	}

	req2 := authedRequest(t, srv, http.MethodGet, "/api/meters/M-109/anomalies?scope=history&from=2026-09-01T00:00:00Z&to=2026-09-30T23:00:00Z", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var resp2 struct {
		Items []struct {
			Type string `json:"type"`
		} `json:"items"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if len(resp2.Items) != 1 || resp2.Items[0].Type != "REAL_ANOMALY" {
		t.Fatalf("expected 1 anomaly for an overlapping range, got %+v", resp2.Items)
	}
}

func TestGetMetersEnvelopeHasPeriodBaselineRefAndAnalysisPeriod(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'CRITICAL', ?)`, time.Now().Format(time.RFC3339))
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-999', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	setupAnalysisWithAnomaly(t, srv.DB)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/meters?from=2026-09-01T00:00:00Z&to=2026-09-30T23:00:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Period periodRange     `json:"period"`
		Items  []meterListItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Period.From != "2026-09-01T00:00:00Z" || resp.Period.To != "2026-09-30T23:00:00Z" {
		t.Fatalf("expected top-level period to echo the query range, got %+v", resp.Period)
	}

	var m109, m999 *meterListItem
	for i := range resp.Items {
		if resp.Items[i].MeterID == "M-109" {
			m109 = &resp.Items[i]
		}
		if resp.Items[i].MeterID == "M-999" {
			m999 = &resp.Items[i]
		}
	}
	if m109 == nil {
		t.Fatal("M-109 missing from items")
	}
	if m109.BaselineRef == nil || m109.BaselineRef.Method != "HOURLY_MEDIAN" ||
		m109.BaselineRef.WindowFrom != "2026-08-18T00:00:00Z" || m109.BaselineRef.WindowTo != "2026-08-31T23:00:00Z" {
		t.Fatalf("expected M-109 baseline_ref populated from meter_baselines, got %+v", m109.BaselineRef)
	}
	if m109.AnalysisPeriod == nil || m109.AnalysisPeriod.From != "2026-09-01T00:00:00Z" || m109.AnalysisPeriod.To != "2026-09-14T23:00:00Z" {
		t.Fatalf("expected M-109 analysis_period to reflect the anomaly's own period, got %+v", m109.AnalysisPeriod)
	}

	if m999 == nil {
		t.Fatal("M-999 missing from items")
	}
	if m999.BaselineRef != nil || m999.AnalysisPeriod != nil {
		t.Fatalf("expected never-analyzed M-999 to have nil baseline_ref/analysis_period, got %+v", m999)
	}
}
