package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// TestFullFlowLoginAnalyzePollAndReadMeters exercises the real, end-to-end
// request flow across every endpoint wired up in Tasks 2-8: login issues a
// JWT, POST /ai/analyze starts a background analysis run, polling
// GET /ai/analysis/:id (via the store directly, mirroring what a client
// polling the same endpoint would observe) waits for it to reach a terminal
// state, and GET /meters reflects the resulting status/anomaly on the
// analyzed meter. All requests go through the real chi router (NewRouter),
// not individual handlers, so this is the first test to catch bugs that only
// show up from handlers interacting with each other across a single shared
// DB connection (see Tasks 6-8's self-deadlock fixes).
func TestFullFlowLoginAnalyzePollAndReadMeters(t *testing.T) {
	srv := newTestServer(t)
	seedDemoUser(t, srv, "demo@energy.local", "demo1234")
	if _, err := srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-101', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	seedTwoWeeksOfReadings(t, srv.DB, "M-101", func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 25
		}
		return 10
	})
	router := NewRouter(srv)

	// 1. login
	loginBody := []byte(`{"email":"demo@energy.local","password":"demo1234"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decoding login response: %v", err)
	}
	if loginResp.Token == "" {
		t.Fatal("expected token from login")
	}

	// 2. POST /ai/analyze (no body -> analyzes the full fleet over the full
	// available data range, per spec 03).
	analyzeReq := httptest.NewRequest(http.MethodPost, "/api/ai/analyze", nil)
	analyzeReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	analyzeRec := httptest.NewRecorder()
	router.ServeHTTP(analyzeRec, analyzeReq)
	if analyzeRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", analyzeRec.Code, analyzeRec.Body.String())
	}
	var analyzeResp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(analyzeRec.Body.Bytes(), &analyzeResp); err != nil {
		t.Fatalf("decoding analyze response: %v", err)
	}

	// 3. Poll until the analysis reaches a terminal state, bounded so a
	// regression that reintroduces the connection-pool self-deadlock fails
	// loudly instead of hanging the test suite forever.
	waitForAnalysisDone(t, srv.DB, analyzeResp.ID, 5*time.Second)

	// Confirm the polling endpoint itself (not just the DB row) reports
	// COMPLETED, since that's the contract a real client observes.
	statusReq := httptest.NewRequest(http.MethodGet, "/api/ai/analysis/"+strconv.FormatInt(analyzeResp.ID, 10), nil)
	statusReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /ai/analysis/:id, got %d: %s", statusRec.Code, statusRec.Body.String())
	}
	var statusResp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decoding analysis status response: %v", err)
	}
	if statusResp.Status != "COMPLETED" {
		t.Fatalf("expected analysis status COMPLETED, got %q: %s", statusResp.Status, statusRec.Body.String())
	}

	// 4. GET /meters reflects the resulting anomaly.
	metersReq := httptest.NewRequest(http.MethodGet, "/api/meters", nil)
	metersReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	metersRec := httptest.NewRecorder()
	router.ServeHTTP(metersRec, metersReq)
	if metersRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /meters, got %d: %s", metersRec.Code, metersRec.Body.String())
	}

	var metersResp struct {
		Items []struct {
			MeterID string `json:"meter_id"`
			Status  string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(metersRec.Body.Bytes(), &metersResp); err != nil {
		t.Fatalf("decoding meters response: %v", err)
	}
	if len(metersResp.Items) != 1 || metersResp.Items[0].Status == "UNKNOWN" {
		t.Fatalf("expected M-101 status updated after analysis, got %+v", metersResp.Items)
	}
}
