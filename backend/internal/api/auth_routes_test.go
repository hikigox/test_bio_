package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"energy-management/internal/auth"
)

func seedDemoUser(t *testing.T, srv *Server, email, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, email, hash); err != nil {
		t.Fatal(err)
	}
}

func TestLoginSuccessReturnsToken(t *testing.T) {
	srv := newTestServer(t)
	seedDemoUser(t, srv, "demo@energy.local", "demo1234")
	router := NewRouter(srv)

	body := bytes.NewBufferString(`{"email":"demo@energy.local","password":"demo1234"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginWrongPasswordReturns401(t *testing.T) {
	srv := newTestServer(t)
	seedDemoUser(t, srv, "demo@energy.local", "demo1234")
	router := NewRouter(srv)

	body := bytes.NewBufferString(`{"email":"demo@energy.local","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestProtectedRouteRequiresAuth(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// NOTE: no real route is registered under the protected group yet
	// (registerMeterRoutes/registerAnomalyRoutes/registerAnalysisRoutes/
	// registerDashboardRoutes are all empty stubs owned by later tasks), so
	// chi's router never matches this path and falls straight to the JSON
	// 404 handler without ever entering the requireAuth middleware chain
	// (chi's NotFoundHandler is not wrapped by group-scoped middleware).
	// This is expected to become 401 once a real protected route exists
	// (Task 7). requireAuth's own behavior is covered directly below.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (no protected route registered yet), got %d", rec.Code)
	}
}

func protectedTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		userID, _ := req.Context().Value(userIDKey).(int64)
		writeJSON(w, http.StatusOK, map[string]int64{"user_id": userID})
	})
}

func TestRequireAuthRejectsMissingHeader(t *testing.T) {
	srv := newTestServer(t)
	handler := requireAuth(srv)(protectedTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing header, got %d", rec.Code)
	}
}

func TestRequireAuthRejectsMalformedHeader(t *testing.T) {
	srv := newTestServer(t)
	handler := requireAuth(srv)(protectedTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Authorization", "Token abc.def.ghi")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for malformed header, got %d", rec.Code)
	}
}

func TestRequireAuthRejectsWrongSecretToken(t *testing.T) {
	srv := newTestServer(t)
	handler := requireAuth(srv)(protectedTestHandler())

	token, err := auth.GenerateToken(7, "a-different-secret")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for token signed with wrong secret, got %d", rec.Code)
	}
}

func TestRequireAuthAcceptsValidToken(t *testing.T) {
	srv := newTestServer(t)
	handler := requireAuth(srv)(protectedTestHandler())

	token, err := auth.GenerateToken(7, srv.JWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid token, got %d: %s", rec.Code, rec.Body.String())
	}
}
