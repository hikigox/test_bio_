package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"energy-management/internal/auth"
)

func authedRequest(t *testing.T, srv *Server, method, path string, body []byte) *http.Request {
	t.Helper()
	token, _ := auth.GenerateToken(1, srv.JWTSecret)
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestPostAIAnalyzeWithoutBodyReturns202AndPolls(t *testing.T) {
	srv := newTestServer(t)
	seedTwoWeeksOfReadings(t, srv.DB, "M-101", func(h int) float64 { return 10 })
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-101', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodPost, "/api/ai/analyze", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID == 0 {
		t.Fatal("expected non-zero analysis id")
	}

	waitForAnalysisDone(t, srv.DB, resp.ID, 2*time.Second)

	req2 := authedRequest(t, srv, http.MethodGet, "/api/ai/analysis/"+strconv.FormatInt(resp.ID, 10), nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on polling, got %d", rec2.Code)
	}
	var status struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &status)
	if status.Status != "COMPLETED" {
		t.Fatalf("expected COMPLETED status in poll response, got %q", status.Status)
	}
}

func TestPostAIAnalyzeConcurrentReturns409(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO analyses (status, data_from, data_to) VALUES ('RUNNING', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodPost, "/api/ai/analyze", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestGetAIAnalysisListReturnsHistory(t *testing.T) {
	srv := newTestServer(t)
	srv.DB.Exec(`INSERT INTO analyses (status, data_from, data_to) VALUES ('COMPLETED', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z')`)
	srv.DB.Exec(`INSERT INTO analyses (status, data_from, data_to) VALUES ('COMPLETED', '2026-08-01T00:00:00Z', '2026-08-14T23:00:00Z')`)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/ai/analysis?limit=10", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var history []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
}

func TestGetAIAnalysisUnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/ai/analysis/9999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
