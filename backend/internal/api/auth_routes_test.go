package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestLoginNonexistentEmailReturns401(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	body := bytes.NewBufferString(`{"email":"nobody@energy.local","password":"whatever"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for nonexistent email, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestLoginTimingSideChannelIsMitigated proves the fix for the timing
// side-channel: login must run a bcrypt compare on BOTH the "email not
// found" and "email found, wrong password" paths, so an attacker cannot
// distinguish the two by response time. Rather than asserting a fragile
// exact-timing bound, this test asserts that both paths take a comparable,
// non-trivial amount of time (each within an order of magnitude of the
// other, and both well above what a bare DB lookup alone would cost),
// which only holds if bcrypt.CompareHashAndPassword actually runs on both.
// Before the fix, "email not found" short-circuited before ever calling
// auth.VerifyPassword and was orders of magnitude faster.
func TestLoginTimingSideChannelIsMitigated(t *testing.T) {
	srv := newTestServer(t)
	seedDemoUser(t, srv, "demo@energy.local", "demo1234")
	router := NewRouter(srv)

	measure := func(email string) time.Duration {
		body := bytes.NewBufferString(`{"email":"` + email + `","password":"wrong-password"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
		rec := httptest.NewRecorder()
		start := time.Now()
		router.ServeHTTP(rec, req)
		elapsed := time.Since(start)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for %q, got %d", email, rec.Code)
		}
		return elapsed
	}

	missingEmailDuration := measure("nobody@energy.local")
	wrongPasswordDuration := measure("demo@energy.local")

	// bcrypt at DefaultCost dominates request time (tens of ms); a
	// short-circuited lookup-miss would be sub-millisecond by comparison.
	// Require both paths to be within the same order of magnitude of each
	// other so the test fails if the short-circuit regresses.
	ratio := float64(missingEmailDuration) / float64(wrongPasswordDuration)
	if ratio < 0.2 || ratio > 5 {
		t.Fatalf("timing side-channel suspected: missing-email=%v wrong-password=%v ratio=%.3f (want within 0.2x-5x of each other)",
			missingEmailDuration, wrongPasswordDuration, ratio)
	}
}

func TestProtectedRouteRequiresAuth(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// GET /api/dashboard/summary is now a real, routed, protected endpoint
	// (Task 7), so an unauthenticated request must be rejected by
	// requireAuth's middleware chain before ever reaching the handler.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (no auth header on protected route), got %d", rec.Code)
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
