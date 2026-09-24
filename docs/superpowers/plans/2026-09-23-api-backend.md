# API Backend (Fase 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Exponer el motor de anomalías y el store SQLite vía HTTP JSON (`/api`), con auth simple, análisis asíncrono con polling, y los filtros de fechas descritos en la spec 03.

**Architecture:** `cmd/api/main.go` levanta `net/http` con `chi` como router; `internal/api` contiene handlers finos que llaman a `internal/store` para leer/escribir y a `internal/engine.Run` (del plan de Motor) para analizar. El análisis corre en una goroutine por `POST /ai/analyze`, escribiendo su progreso en la tabla `analyses` para que el polling lo lea.

**Tech Stack:** Go 1.23, `net/http` + `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, JWT simple (`golang-jwt/jwt/v5`) o token opaco en memoria — se decide en Task 2.

**Spec:** `specs/00-overview.md`, `specs/01-data-model.md`, `specs/02-anomaly-engine.md` (solo como contexto de qué produce el motor), `specs/03-api.md`, `specs/05-testing-demo.md`

**Depende de:** el plan `2026-09-23-motor-anomaly-engine.md` debe estar completo — este plan usa `internal/store.Open/Migrate/LoadReadingsCSV/LoadEventsCSV` y `internal/engine.Run/DefaultConfig/MeterResult` tal como quedaron definidos ahí. No se redefinen aquí.

## Global Constraints

- Prefijo `/api` para todos los endpoints; respuestas y errores en JSON; error = `{"error":"mensaje"}` con código HTTP correcto — spec 03.
- Auth: `POST /auth/login {email,password}` → `{token}`; `Authorization: Bearer <token>` en el resto; CORS habilitado para el frontend — spec 03.
- Fechas en ISO-8601 UTC en toda la API — spec 03.
- Solo un análisis `RUNNING` a la vez (409 si hay otro) — spec 03.
- `POST /ai/analyze` sin cuerpo = todos los medidores y todo el período (caso del botón Run AI Analysis del Dashboard) — spec 03.
- El baseline no se recalcula con el filtro de fechas de consulta, pero sí se aplica a él (suma del perfil horario sobre los timestamps existentes en el rango) — spec 03.
- La anomalía vigente de un medidor es la del último análisis `COMPLETED` que lo incluyó; `priority_rank` se calcula al consultar, no se guarda — spec 03.
- `expected_results.csv` nunca se carga en la app — spec 00 regla 1 (hereda del plan de Motor).
- El backend carga `readings.csv`/`events.csv` al iniciar solo si la BD está vacía (idempotencia ya la da `store.LoadReadingsCSV`, spec 06).

## Review Focus

- `POST /ai/analyze` disparado dos veces seguidas mientras el primero sigue `RUNNING`: debe responder 409, no encolar ni pisar el análisis en curso.
- `GET /meters` para un medidor que nunca fue analizado: `baseline_kwh`, `variation_pct`, `analysis_id`, `anomaly` deben ser `null` y `status` debe ser `"UNKNOWN"`, no un error ni un 500 por division/nil.
- Filtro `from`/`to` en `/anomalies` cuyo rango no se solapa con ninguna anomalía vigente: debe devolver `[]`, no todas las anomalías ni un error.
- `PATCH /anomalies/:id` con un `status` fuera del enum (`OPEN|INVESTIGATING|RESOLVED|DISMISSED`): debe responder 400, no aceptarlo silenciosamente.
- `POST /ai/analyze` con `baseline_from`/`baseline_to` cubriendo menos de 3 días o fuera del rango de datos: debe responder 400 (spec 03, validación explícita), no lanzar el análisis con una ventana inválida.

---

## File Structure

```
backend/
  cmd/api/main.go            # arranque: migración, carga CSV inicial, router, listen
  internal/
    auth/
      auth.go                # hash de password, generación/validación de token
    api/
      router.go               # registro de rutas chi + middleware (auth, CORS, JSON errors)
      middleware.go            # RequireAuth, CORS, error helpers
      dashboard.go             # GET /dashboard/summary
      meters.go                # GET /meters, /meters/:id, /meters/:id/readings, /meters/:id/events, /meters/:id/anomalies
      anomalies.go             # GET /anomalies, GET /anomalies/:id, PATCH /anomalies/:id
      analysis.go              # POST /ai/analyze, GET /ai/analysis/:id, GET /ai/analysis
      analysis_runner.go       # goroutine que corre engine.Run por medidor y persiste resultados
      query.go                 # helpers compartidos: vigencia, ventana activa, baseline sobre rango
```

## Interfaces (contrato compartido)

```go
// internal/auth/auth.go
package auth

func HashPassword(plain string) (string, error)
func VerifyPassword(hash, plain string) bool
func GenerateToken(userID int64, secret string) (string, error)
func ParseToken(token, secret string) (userID int64, err error)
```

```go
// internal/api/router.go
package api

import (
	"net/http"
	"energy-management/internal/store"
)

type Server struct {
	DB        *store.DB
	JWTSecret string
}

func NewRouter(s *Server) http.Handler
```

```go
// internal/api/analysis_runner.go
package api

// StartAnalysis valida el scope, crea la fila `analyses` en PENDING/RUNNING
// y lanza la goroutine que corre el motor. Devuelve el id inmediatamente.
// Retorna error de tipo *ConflictError si ya hay un análisis RUNNING.
func StartAnalysis(db *store.DB, scope AnalysisScope, triggeredBy int64) (analysisID int64, err error)

type AnalysisScope struct {
	MeterIDs     []string   // nil = todos
	From, To     time.Time  // período a analizar
	BaselineFrom, BaselineTo *time.Time // nil = automático
}

type ConflictError struct{ RunningID int64 }
func (e *ConflictError) Error() string
```

Todas las tareas de este plan importan `energy-management/internal/store` y `energy-management/internal/engine` del plan de Motor; no se redefinen sus tipos aquí.

---

### Task 1: Scaffold del servidor HTTP y arranque (`cmd/api/main.go`)

**Files:**
- Create: `backend/cmd/api/main.go`
- Create: `backend/internal/api/router.go`
- Create: `backend/internal/api/middleware.go`
- Test: `backend/internal/api/router_test.go`

**Interfaces:**
- Consumes: `store.Open`, `store.Migrate`, `store.LoadReadingsCSV`, `store.LoadEventsCSV` (plan de Motor, Tasks 1-2).
- Produces: `api.NewRouter(*api.Server) http.Handler`, endpoint `GET /healthz` (spec 06) — usado por todas las tareas siguientes y por el healthcheck de Docker.

- [ ] **Step 1: Añadir dependencia chi**

```bash
cd backend
go get github.com/go-chi/chi/v5@latest
```

- [ ] **Step 2: Escribir el test del router base**

```go
// backend/internal/api/router_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"energy-management/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	return &Server{DB: db, JWTSecret: "test-secret"}
}

func TestHealthzReturns200(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	srv := newTestServer(t)
	router := NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -v`
Expected: FAIL (undefined: Server, NewRouter)

- [ ] **Step 4: Implementar `middleware.go`**

```go
// backend/internal/api/middleware.go
package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func notFoundJSON(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "ruta no encontrada")
}
```

- [ ] **Step 5: Implementar `router.go`**

```go
// backend/internal/api/router.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"energy-management/internal/store"
)

type Server struct {
	DB        *store.DB
	JWTSecret string
}

func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(corsMiddleware)
	r.NotFound(notFoundJSON)

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		registerAuthRoutes(api, s)   // Task 2

		api.Group(func(protected chi.Router) {
			protected.Use(requireAuth(s))
			registerMeterRoutes(protected, s)     // Task 3
			registerAnomalyRoutes(protected, s)   // Task 6
			registerAnalysisRoutes(protected, s)  // Task 4-5
			registerDashboardRoutes(protected, s) // Task 7
		})
	})

	return r
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS (dos tests compilan una vez existan los `register*Routes` — ver nota abajo)

Nota: en este paso `registerAuthRoutes`, `registerMeterRoutes`, etc. aún no existen. Para que el test de este Task pase de forma aislada, define stubs vacíos al final de `router.go`:

```go
func registerAuthRoutes(r chi.Router, s *Server)     {}
func registerMeterRoutes(r chi.Router, s *Server)    {}
func registerAnomalyRoutes(r chi.Router, s *Server)  {}
func registerAnalysisRoutes(r chi.Router, s *Server) {}
func registerDashboardRoutes(r chi.Router, s *Server) {}
func requireAuth(s *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
```//
Cada task siguiente reemplaza su stub correspondiente por la implementación real (no lo duplica).

- [ ] **Step 7: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/api/router.go backend/internal/api/middleware.go backend/internal/api/router_test.go
git commit -m "feat(api): HTTP router scaffold with healthz and JSON error handling"
```

---

### Task 2: Autenticación (`POST /auth/login`, middleware `requireAuth`)

**Files:**
- Create: `backend/internal/auth/auth.go`
- Create: `backend/internal/api/auth_routes.go` (reemplaza el stub `registerAuthRoutes`)
- Modify: `backend/internal/api/router.go:registerAuthRoutes stub` → eliminar el stub, `requireAuth` real reemplaza el suyo
- Test: `backend/internal/auth/auth_test.go`
- Test: `backend/internal/api/auth_routes_test.go`

**Interfaces:**
- Produces: `auth.HashPassword`, `auth.VerifyPassword`, `auth.GenerateToken`, `auth.ParseToken` — usados por el seed de usuario demo (Task 1 del plan de Docker, fuera de este plan) y por `requireAuth`.

- [ ] **Step 1: Escribir el test de `auth.go`**

```go
// backend/internal/auth/auth_test.go
package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("demo1234")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "demo1234") {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("expected wrong password to fail verification")
	}
}

func TestGenerateAndParseToken(t *testing.T) {
	token, err := GenerateToken(42, "secret")
	if err != nil {
		t.Fatal(err)
	}
	userID, err := ParseToken(token, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if userID != 42 {
		t.Fatalf("expected userID 42, got %d", userID)
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	token, _ := GenerateToken(42, "secret")
	if _, err := ParseToken(token, "other-secret"); err == nil {
		t.Fatal("expected error for token signed with a different secret")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/... -v`
Expected: FAIL (undefined: HashPassword)

- [ ] **Step 3: Implementar `auth.go`**

```go
// backend/internal/auth/auth.go
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

type claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

func GenerateToken(userID int64, secret string) (string, error) {
	c := claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString([]byte(secret))
}

func ParseToken(tokenStr, secret string) (int64, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return 0, errors.New("token inválido")
	}
	c, ok := token.Claims.(*claims)
	if !ok {
		return 0, errors.New("claims inválidos")
	}
	return c.UserID, nil
}
```

```bash
go get github.com/golang-jwt/jwt/v5 golang.org/x/crypto
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/... -v`
Expected: PASS

- [ ] **Step 5: Escribir el test de la ruta `/auth/login` y del middleware**

```go
// backend/internal/api/auth_routes_test.go
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

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run "Login|ProtectedRoute" -v`
Expected: FAIL (login endpoint returns 404, dashboard stub not registered)

- [ ] **Step 7: Implementar `auth_routes.go`, reemplazar los stubs en `router.go`**

```go
// backend/internal/api/auth_routes.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"energy-management/internal/auth"
)

type ctxKey string

const userIDKey ctxKey = "userID"

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func registerAuthRoutes(r chi.Router, s *Server) {
	r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) {
		var body loginRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "cuerpo inválido")
			return
		}
		var userID int64
		var hash string
		err := s.DB.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, body.Email).Scan(&userID, &hash)
		if err != nil || !auth.VerifyPassword(hash, body.Password) {
			writeError(w, http.StatusUnauthorized, "credenciales inválidas")
			return
		}
		token, err := auth.GenerateToken(userID, s.JWTSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error generando token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	})
}

func requireAuth(s *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			h := req.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "falta el token")
				return
			}
			token := strings.TrimPrefix(h, "Bearer ")
			userID, err := auth.ParseToken(token, s.JWTSecret)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "token inválido")
				return
			}
			ctx := context.WithValue(req.Context(), userIDKey, userID)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}
```

En `router.go`: eliminar `func registerAuthRoutes(...) {}` y `func requireAuth(...) {...}` (los stubs del Task 1) ya que ahora viven en `auth_routes.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add backend/internal/auth/auth.go backend/internal/auth/auth_test.go backend/internal/api/auth_routes.go backend/internal/api/auth_routes_test.go backend/internal/api/router.go backend/go.mod backend/go.sum
git commit -m "feat(api): JWT login and requireAuth middleware"
```

---

### Task 3: Helpers de vigencia, ventana activa y baseline sobre rango

**Files:**
- Create: `backend/internal/api/query.go`
- Test: `backend/internal/api/query_test.go`

**Interfaces:**
- Consumes: filas crudas de `meter_baselines`/`anomalies` vía `*store.DB`.
- Produces: `api.CurrentAnomaly(db *store.DB, meterID string) (*AnomalyRow, error)`, `api.ActiveWindow(a AnomalyRow) (from, to time.Time)`, `api.BaselineForRange(db *store.DB, meterID string, from, to time.Time) (baselineKWh, actualKWh float64, err error)` — usados por Task 4 (`/meters`), Task 6 (`/anomalies`), Task 7 (`/dashboard/summary`).

- [ ] **Step 1: Escribir el test (usa una BD en memoria con datos insertados a mano, spec 03 "vigencia" y "baseline y filtro de fechas")**

```go
// backend/internal/api/query_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run "CurrentAnomaly|ActiveWindow|BaselineForRange" -v`
Expected: FAIL (undefined: CurrentAnomaly)

- [ ] **Step 3: Implementar `query.go`**

```go
// backend/internal/api/query.go
package api

import (
	"database/sql"
	"encoding/json"
	"time"

	"energy-management/internal/store"
)

type AnomalyRow struct {
	ID            int64
	AnalysisID    int64
	MeterID       string
	Type          string
	Severity      string
	Confidence    float64
	PriorityScore float64
	BaselineKWh   float64
	ActualKWh     float64
	VariationPct  float64
	PeriodFrom    time.Time
	PeriodTo      time.Time
	ChangePointAt *time.Time
	Status        string
}

// CurrentAnomaly retorna la anomalía del último análisis COMPLETED que incluyó
// a meterID, o nil si nunca fue analizado o no tuvo anomalía (spec 03 "vigencia").
func CurrentAnomaly(db *store.DB, meterID string) (*AnomalyRow, error) {
	row := db.QueryRow(`
		SELECT a.id, a.analysis_id, a.meter_id, a.type, a.severity, a.confidence, a.priority_score,
		       a.baseline_kwh, a.actual_kwh, a.variation_pct, a.period_from, a.period_to, a.change_point_at, a.status
		FROM anomalies a
		JOIN analyses an ON an.id = a.analysis_id
		WHERE a.meter_id = ? AND an.status = 'COMPLETED'
		ORDER BY an.finished_at DESC, an.id DESC
		LIMIT 1`, meterID)

	var a AnomalyRow
	var periodFrom, periodTo string
	var changePointAt sql.NullString
	err := row.Scan(&a.ID, &a.AnalysisID, &a.MeterID, &a.Type, &a.Severity, &a.Confidence, &a.PriorityScore,
		&a.BaselineKWh, &a.ActualKWh, &a.VariationPct, &periodFrom, &periodTo, &changePointAt, &a.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.PeriodFrom, _ = time.Parse(time.RFC3339, periodFrom)
	a.PeriodTo, _ = time.Parse(time.RFC3339, periodTo)
	if changePointAt.Valid {
		t, _ := time.Parse(time.RFC3339, changePointAt.String)
		a.ChangePointAt = &t
	}
	return &a, nil
}

// ActiveWindow deriva la ventana activa: active_from = change_point_at ?? period_from (spec 03).
func ActiveWindow(a AnomalyRow) (from, to time.Time) {
	if a.ChangePointAt != nil {
		return *a.ChangePointAt, a.PeriodTo
	}
	return a.PeriodFrom, a.PeriodTo
}

// Overlaps indica si la ventana activa se solapa con [from, to] (spec 03 filtro de fechas).
func Overlaps(activeFrom, activeTo, from, to time.Time) bool {
	return !activeFrom.After(to) && !activeTo.Before(from)
}

// BaselineForRange calcula baseline_kwh esperado y actual_kwh real sobre [from, to),
// sumando el perfil horario guardado solo en los timestamps que existen en ese rango
// (spec 03 "Baseline y filtro de fechas": las lecturas faltantes no distorsionan).
func BaselineForRange(db *store.DB, meterID string, from, to time.Time) (baselineKWh, actualKWh float64, err error) {
	anomaly, err := CurrentAnomaly(db, meterID)
	if err != nil {
		return 0, 0, err
	}
	var analysisID int64
	if anomaly != nil {
		analysisID = anomaly.AnalysisID
	} else {
		// medidor sin anomalía puede seguir teniendo baseline (spec 01); buscar el más reciente.
		row := db.QueryRow(`SELECT mb.analysis_id FROM meter_baselines mb
			JOIN analyses an ON an.id = mb.analysis_id
			WHERE mb.meter_id = ? AND an.status = 'COMPLETED'
			ORDER BY an.finished_at DESC LIMIT 1`, meterID)
		if err := row.Scan(&analysisID); err == sql.ErrNoRows {
			return 0, 0, nil
		} else if err != nil {
			return 0, 0, err
		}
	}

	var profileJSON string
	if err := db.QueryRow(`SELECT hourly_profile_json FROM meter_baselines WHERE analysis_id = ? AND meter_id = ?`,
		analysisID, meterID).Scan(&profileJSON); err != nil {
		return 0, 0, err
	}
	var profile [24]float64
	if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
		return 0, 0, err
	}

	rows, err := db.Query(`SELECT timestamp, consumption_kwh FROM readings
		WHERE meter_id = ? AND timestamp >= ? AND timestamp < ?`,
		meterID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var ts string
		var kwh float64
		rows.Scan(&ts, &kwh)
		t, _ := time.Parse(time.RFC3339, ts)
		baselineKWh += profile[t.Hour()]
		actualKWh += kwh
	}
	return baselineKWh, actualKWh, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/query.go backend/internal/api/query_test.go
git commit -m "feat(api): current-anomaly, active-window and ranged-baseline query helpers"
```

---

### Task 4: `POST /ai/analyze` — validación de alcance y disparo del análisis

**Files:**
- Create: `backend/internal/api/analysis_runner.go`
- Create: `backend/internal/api/analysis.go` (reemplaza el stub `registerAnalysisRoutes`; incluye también `GET /ai/analysis/:id` y `GET /ai/analysis` del Task 5, pero se implementan en Task 5)
- Test: `backend/internal/api/analysis_runner_test.go`

**Interfaces:**
- Consumes: `engine.Run`, `engine.DefaultConfig`, `engine.MeterResult` (plan de Motor); `CurrentAnomaly`, helpers de `query.go` (Task 3).
- Produces: `api.StartAnalysis(db *store.DB, scope AnalysisScope, triggeredBy int64) (int64, error)` — usado por Task 5 (handler HTTP) y por los tests de integración (Task 8).

- [ ] **Step 1: Escribir el test — validación de scope y el 409 de concurrencia**

```go
// backend/internal/api/analysis_runner_test.go
package api

import (
	"testing"
	"time"

	"energy-management/internal/store"
)

func seedTwoWeeksOfReadings(t *testing.T, db *store.DB, meterID string, kwh func(h int) float64) {
	t.Helper()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 14*24; h++ {
		ts := start.Add(time.Duration(h) * time.Hour).Format(time.RFC3339)
		_, err := db.Exec(`INSERT INTO readings (meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
			VALUES (?, ?, ?, 220, 10, 0.95, 'OK')`, meterID, ts, kwh(h))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartAnalysisRejectsBaselineWindowUnder3Days(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	seedTwoWeeksOfReadings(t, db, "M-101", func(h int) float64 { return 10 })

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	baselineFrom := from
	baselineTo := from.Add(2 * 24 * time.Hour) // < 3 días

	_, err := StartAnalysis(db, AnalysisScope{From: from, To: to, BaselineFrom: &baselineFrom, BaselineTo: &baselineTo}, 1)
	if err == nil {
		t.Fatal("expected error for baseline window under 3 days")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestStartAnalysisRejectsConcurrentRun(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	seedTwoWeeksOfReadings(t, db, "M-101", func(h int) float64 { return 10 })
	db.Exec(`INSERT INTO analyses (status, data_from, data_to) VALUES ('RUNNING', '2026-09-01T00:00:00Z', '2026-09-14T23:00:00Z')`)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	_, err := StartAnalysis(db, AnalysisScope{From: from, To: to}, 1)
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
}

func TestStartAnalysisCreatesPendingRowAndReturnsID(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	seedTwoWeeksOfReadings(t, db, "M-101", func(h int) float64 { return 10 })

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 14, 23, 0, 0, 0, time.UTC)
	id, err := StartAnalysis(db, AnalysisScope{From: from, To: to}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero analysis id")
	}

	// esperar a que la goroutine termine (dataset pequeño en test)
	waitForAnalysisDone(t, db, id, 2*time.Second)

	var status string
	db.QueryRow(`SELECT status FROM analyses WHERE id = ?`, id).Scan(&status)
	if status != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %v", status)
	}
}

func waitForAnalysisDone(t *testing.T, db *store.DB, id int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var status string
		db.QueryRow(`SELECT status FROM analyses WHERE id = ?`, id).Scan(&status)
		if status == "COMPLETED" || status == "FAILED" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for analysis to finish")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run StartAnalysis -v`
Expected: FAIL (undefined: StartAnalysis, AnalysisScope, ValidationError)

- [ ] **Step 3: Implementar `analysis_runner.go`**

```go
// backend/internal/api/analysis_runner.go
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

type AnalysisScope struct {
	MeterIDs                 []string
	From, To                 time.Time
	BaselineFrom, BaselineTo *time.Time
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type ConflictError struct{ RunningID int64 }

func (e *ConflictError) Error() string { return fmt.Sprintf("ya hay un análisis en curso (id=%d)", e.RunningID) }

// StartAnalysis valida el scope, crea la fila `analyses` y lanza el pipeline
// en una goroutine. Devuelve el id de inmediato (202 en el handler HTTP, Task 5).
func StartAnalysis(db *store.DB, scope AnalysisScope, triggeredBy int64) (int64, error) {
	var runningID sql.NullInt64
	db.QueryRow(`SELECT id FROM analyses WHERE status = 'RUNNING' LIMIT 1`).Scan(&runningID)
	if runningID.Valid {
		return 0, &ConflictError{RunningID: runningID.Int64}
	}

	if scope.BaselineFrom != nil && scope.BaselineTo != nil {
		if scope.BaselineTo.Sub(*scope.BaselineFrom) < 3*24*time.Hour {
			return 0, &ValidationError{Message: "baseline_from/baseline_to debe cubrir al menos 3 días"}
		}
		if scope.BaselineFrom.Before(scope.From) || scope.BaselineTo.After(scope.To) {
			return 0, &ValidationError{Message: "baseline_from/baseline_to debe estar dentro de from/to"}
		}
	}

	meterIDsJSON, _ := json.Marshal(scope.MeterIDs)
	if scope.MeterIDs == nil {
		meterIDsJSON = []byte("null")
	}

	res, err := db.Exec(`INSERT INTO analyses
		(status, stage, progress, trigger, triggered_by, started_at, data_from, data_to,
		 baseline_from, baseline_to, scope_meter_ids_json, engine_version)
		VALUES ('RUNNING', 'READINGS', 0, 'MANUAL', ?, ?, ?, ?, ?, ?, ?, 'v1')`,
		triggeredBy, time.Now().UTC().Format(time.RFC3339),
		scope.From.Format(time.RFC3339), scope.To.Format(time.RFC3339),
		nullableTime(scope.BaselineFrom), nullableTime(scope.BaselineTo), string(meterIDsJSON))
	if err != nil {
		return 0, err
	}
	analysisID, _ := res.LastInsertId()

	go runAnalysis(db, analysisID, scope)

	return analysisID, nil
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

// runAnalysis corre el pipeline por medidor y persiste baselines/anomalías en
// una transacción (spec 01: "un análisis escribe... en una transacción").
func runAnalysis(db *store.DB, analysisID int64, scope AnalysisScope) {
	setStage(db, analysisID, "BASELINE", 0.1)

	meterIDs := scope.MeterIDs
	if meterIDs == nil {
		rows, err := db.Query(`SELECT DISTINCT meter_id FROM meters`)
		if err == nil {
			for rows.Next() {
				var id string
				rows.Scan(&id)
				meterIDs = append(meterIDs, id)
			}
			rows.Close()
		}
	}

	fleetCV := 0.15 // respaldo de flota; el cálculo fino queda documentado en 02 §10 (motor)
	var results []engine.MeterResult

	setStage(db, analysisID, "DETECTION", 0.3)
	for _, meterID := range meterIDs {
		readings := queryReadingsInRange(db, meterID, scope.From, scope.To)
		if len(readings) == 0 {
			continue
		}
		events := queryEventsForMeter(db, meterID)
		result := engine.Run(meterID, readings, events, fleetCV, engine.DefaultConfig())
		results = append(results, result)
	}

	setStage(db, analysisID, "CORRELATION", 0.5)
	setStage(db, analysisID, "EVENTS", 0.6)
	setStage(db, analysisID, "EXPLANATION", 0.8)
	setStage(db, analysisID, "RECOMMENDATION", 0.9)

	if err := persistResults(db, analysisID, results); err != nil {
		db.Exec(`UPDATE analyses SET status='FAILED', error=?, finished_at=? WHERE id=?`,
			err.Error(), time.Now().UTC().Format(time.RFC3339), analysisID)
		return
	}

	finishAnalysis(db, analysisID, results)
}

func setStage(db *store.DB, analysisID int64, stage string, progress float64) {
	db.Exec(`UPDATE analyses SET stage=?, progress=? WHERE id=?`, stage, progress, analysisID)
}
```

Nota: `queryReadingsInRange`, `queryEventsForMeter`, `persistResults` y `finishAnalysis` se implementan en el Task 5 (junto con el handler HTTP), porque comparten la lógica de mapear filas SQL ↔ `engine.Reading`/`engine.Event` que también usa `GET /ai/analysis/:id`. Este Task deja el flujo de control (`runAnalysis`) completo pero esas cuatro funciones sin cuerpo (fallarán en compilación) — el siguiente Step las añade como stubs mínimos para que Task 4 quede verde por sí solo, y Task 5 las reemplaza.

- [ ] **Step 4: Añadir stubs mínimos para compilar el Task 4 de forma aislada**

```go
// backend/internal/api/analysis_runner.go (añadir al final)

func queryReadingsInRange(db *store.DB, meterID string, from, to time.Time) []engine.Reading {
	rows, err := db.Query(`SELECT timestamp, consumption_kwh, voltage_v, current_a, power_factor, status
		FROM readings WHERE meter_id = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp`,
		meterID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []engine.Reading
	for rows.Next() {
		var r engine.Reading
		var ts string
		rows.Scan(&ts, &r.ConsumptionKWh, &r.VoltageV, &r.CurrentA, &r.PowerFactor, &r.Status)
		r.Timestamp, _ = time.Parse(time.RFC3339, ts)
		r.MeterID = meterID
		out = append(out, r)
	}
	return out
}

func queryEventsForMeter(db *store.DB, meterID string) []engine.Event {
	rows, err := db.Query(`SELECT timestamp, type, description FROM events WHERE meter_id = ?`, meterID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []engine.Event
	for rows.Next() {
		var e engine.Event
		var ts string
		rows.Scan(&ts, &e.Type, &e.Description)
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		e.MeterID = meterID
		out = append(out, e)
	}
	return out
}

// persistResults y finishAnalysis: implementación completa en Task 5.
func persistResults(db *store.DB, analysisID int64, results []engine.MeterResult) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, r := range results {
		if err := insertMeterBaseline(tx, analysisID, r); err != nil {
			tx.Rollback()
			return err
		}
		if r.HasAnomaly {
			if err := insertAnomaly(tx, analysisID, r); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func finishAnalysis(db *store.DB, analysisID int64, results []engine.MeterResult) {
	anomalyCount, highPriority := 0, 0
	var confSum float64
	for _, r := range results {
		if r.HasAnomaly {
			anomalyCount++
			confSum += r.Confidence
			if r.Severity == engine.High {
				highPriority++
			}
		}
	}
	avgConfidence := 0.0
	if anomalyCount > 0 {
		avgConfidence = confSum / float64(anomalyCount)
	}
	summary := fmt.Sprintf("%d anomalías detectadas · %d requieren atención prioritaria", anomalyCount, highPriority)
	db.Exec(`UPDATE analyses SET status='COMPLETED', stage='RECOMMENDATION', progress=1,
		finished_at=?, duration_ms=?, readings_analyzed=?, meters_analyzed=?,
		anomalies_count=?, high_priority_count=?, avg_confidence=?, summary_message=?
		WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), 0, 0, len(results),
		anomalyCount, highPriority, avgConfidence, summary, analysisID)
}
```

`insertMeterBaseline` e `insertAnomaly` se implementan en el Task 5 — para este Task, añádelos como funciones que solo hacen `return nil` (stub) de modo que el paquete compile:

```go
func insertMeterBaseline(tx *sql.Tx, analysisID int64, r engine.MeterResult) error { return nil }
func insertAnomaly(tx *sql.Tx, analysisID int64, r engine.MeterResult) error       { return nil }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -run StartAnalysis -v`
Expected: PASS (con los stubs, `TestStartAnalysisCreatesPendingRowAndReturnsID` verifica que llega a `COMPLETED` aunque no persista filas todavía — eso lo cubre Task 5)

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/analysis_runner.go backend/internal/api/analysis_runner_test.go
git commit -m "feat(api): analysis scope validation, concurrency guard, and async runner skeleton"
```

---

### Task 5: Persistencia de resultados + `POST/GET /ai/analyze`, `/ai/analysis/:id`, `/ai/analysis`

**Files:**
- Modify: `backend/internal/api/analysis_runner.go` (reemplazar los stubs `insertMeterBaseline`, `insertAnomaly` del Task 4 por la implementación real)
- Create: `backend/internal/api/analysis.go` (reemplaza el stub `registerAnalysisRoutes`)
- Test: `backend/internal/api/analysis_persist_test.go`
- Test: `backend/internal/api/analysis_routes_test.go`

**Interfaces:**
- Consumes: `StartAnalysis`, `ValidationError`, `ConflictError` (Task 4).
- Produces: endpoints `POST /ai/analyze`, `GET /ai/analysis/:id`, `GET /ai/analysis?limit=` — consumidos por el frontend (plan de Frontend, Task de "Run AI Analysis").

- [ ] **Step 1: Escribir el test de persistencia**

```go
// backend/internal/api/analysis_persist_test.go
package api

import (
	"encoding/json"
	"testing"
	"time"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

func TestPersistResultsWritesBaselineForEveryMeterAndAnomalyOnlyWhenPresent(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	stable := engine.MeterResult{
		MeterID: "M-101", HasAnomaly: false,
		Baseline: engine.HourlyProfile{MedianByHour: [24]float64{}},
		BaselineKWh: 100, ActualKWh: 102, VariationPct: 2,
	}
	withAnomaly := engine.MeterResult{
		MeterID: "M-109", HasAnomaly: true, Type: engine.RealAnomaly, Severity: engine.High,
		Confidence: 0.96, PriorityScore: 5.8, BaselineKWh: 1070, ActualKWh: 2180, VariationPct: 103.7,
		Reason: "Consumo 103.7% por encima del baseline...", RecommendedAction: "Investigar medidor e instalación",
		Baseline: engine.HourlyProfile{MedianByHour: [24]float64{}},
		Evidence: engine.Evidence{DecisionPath: []string{"no_data_quality_issue", "significant_change", "no_event"}},
	}

	err := persistResults(db, 1, []engine.MeterResult{stable, withAnomaly})
	if err != nil {
		t.Fatalf("persist: %v", err)
	}

	var baselineCount int
	db.QueryRow(`SELECT COUNT(*) FROM meter_baselines WHERE analysis_id = 1`).Scan(&baselineCount)
	if baselineCount != 2 {
		t.Fatalf("expected 2 baseline rows (one per meter), got %d", baselineCount)
	}

	var anomalyCount int
	db.QueryRow(`SELECT COUNT(*) FROM anomalies WHERE analysis_id = 1`).Scan(&anomalyCount)
	if anomalyCount != 1 {
		t.Fatalf("expected 1 anomaly row (only M-109), got %d", anomalyCount)
	}

	var evidenceJSON string
	db.QueryRow(`SELECT evidence_json FROM anomalies WHERE meter_id = 'M-109'`).Scan(&evidenceJSON)
	var evidence map[string]interface{}
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		t.Fatalf("evidence_json not valid JSON: %v", err)
	}
	if evidence["decision_path"] == nil {
		t.Fatal("expected decision_path in evidence_json")
	}
}

func TestPersistResultsWritesMeterStatusFromAnomaly(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	db.Migrate()
	db.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-109', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO analyses (id, status) VALUES (1, 'RUNNING')`)

	result := engine.MeterResult{
		MeterID: "M-109", HasAnomaly: true, Type: engine.RealAnomaly, Severity: engine.High,
		Baseline: engine.HourlyProfile{},
	}
	persistResults(db, 1, []engine.MeterResult{result})

	var status string
	db.QueryRow(`SELECT status FROM meters WHERE meter_id = 'M-109'`).Scan(&status)
	if status != "CRITICAL" { // REAL_ANOMALY·HIGH -> CRITICAL, spec 03
		t.Fatalf("expected CRITICAL status, got %v", status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run TestPersistResults -v`
Expected: FAIL (los stubs actuales no insertan nada; `baselineCount` será 0)

- [ ] **Step 3: Reemplazar los stubs en `analysis_runner.go`**

```go
// backend/internal/api/analysis_runner.go
// Reemplazar insertMeterBaseline e insertAnomaly (antes stubs) por:

func insertMeterBaseline(tx *sql.Tx, analysisID int64, r engine.MeterResult) error {
	profileJSON, _ := json.Marshal(r.Baseline.MedianByHour)
	var changePointAt interface{}
	if r.ChangePoint.Found {
		changePointAt = r.ChangePoint.At.Format(time.RFC3339)
	}
	_, err := tx.Exec(`INSERT INTO meter_baselines
		(analysis_id, meter_id, method, window_from, window_to, change_point_at,
		 baseline_kwh, actual_kwh, variation_pct, hourly_profile_json,
		 baseline_voltage_v, baseline_current_a, baseline_power_factor,
		 readings_count, data_quality_score)
		VALUES (?, ?, 'HOURLY_MEDIAN', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		analysisID, r.MeterID, r.Baseline.WindowFrom.Format(time.RFC3339), r.Baseline.WindowTo.Format(time.RFC3339),
		changePointAt, r.BaselineKWh, r.ActualKWh, r.VariationPct, string(profileJSON),
		r.Baseline.VoltageV, r.Baseline.CurrentA, r.Baseline.PowerFactor,
		len(r.Signals), dataQualityScore(r))
	return err
}

func dataQualityScore(r engine.MeterResult) float64 {
	if len(r.Evidence.DataQualityIssues) == 0 {
		return 1
	}
	return 0.5
}

var statusByAnomaly = map[string]string{
	"REAL_ANOMALY:HIGH": "CRITICAL",
	"REAL_ANOMALY:MEDIUM": "ALERT", "REAL_ANOMALY:LOW": "ALERT",
	"EXPLAINABLE_ANOMALY:HIGH": "ALERT", "EXPLAINABLE_ANOMALY:MEDIUM": "ALERT", "EXPLAINABLE_ANOMALY:LOW": "ALERT",
	"DATA_QUALITY:HIGH": "ALERT", "DATA_QUALITY:MEDIUM": "ALERT", "DATA_QUALITY:LOW": "ALERT",
	"FALSE_POSITIVE:HIGH": "OK", "FALSE_POSITIVE:MEDIUM": "OK", "FALSE_POSITIVE:LOW": "OK",
}

func insertAnomaly(tx *sql.Tx, analysisID int64, r engine.MeterResult) error {
	evidenceJSON, _ := json.Marshal(map[string]interface{}{
		"decision_path":         r.Evidence.DecisionPath,
		"confidence_breakdown":  r.Evidence.ConfidenceBreakdown,
		"data_quality_issues":   r.Evidence.DataQualityIssues,
		"affected_readings": map[string]interface{}{
			"count": r.Evidence.AffectedReadingsCount,
			"first": r.Evidence.AffectedReadingsFirst.Format(time.RFC3339),
			"last":  r.Evidence.AffectedReadingsLast.Format(time.RFC3339),
		},
	})
	confidenceLabel := "LOW"
	if r.Confidence > 0.85 {
		confidenceLabel = "HIGH"
	} else if r.Confidence >= 0.6 {
		confidenceLabel = "MEDIUM"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var changePointAt interface{}
	if r.ChangePoint.Found {
		changePointAt = r.ChangePoint.At.Format(time.RFC3339)
	}

	anomalyRes, err := tx.Exec(`INSERT INTO anomalies
		(analysis_id, meter_id, detected_at, period_from, period_to, change_point_at,
		 type, severity, confidence, confidence_label, priority_score,
		 baseline_kwh, actual_kwh, variation_pct, reason, recommended_action,
		 explanation_source, status, created_at, updated_at, evidence_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'TEMPLATE', 'OPEN', ?, ?, ?)`,
		analysisID, r.MeterID, now, r.Baseline.WindowFrom.Format(time.RFC3339), r.Baseline.WindowTo.Format(time.RFC3339),
		changePointAt, string(r.Type), string(r.Severity), r.Confidence, confidenceLabel, r.PriorityScore,
		r.BaselineKWh, r.ActualKWh, r.VariationPct, r.Reason, r.RecommendedAction, now, now, string(evidenceJSON))
	if err != nil {
		return err
	}
	anomalyID, _ := anomalyRes.LastInsertId()

	for _, s := range r.Signals {
		if _, err := tx.Exec(`INSERT INTO anomaly_signals (anomaly_id, signal, observed, threshold, detail)
			VALUES (?, ?, ?, ?, ?)`, anomalyID, string(s.Signal), s.Observed, s.Threshold, s.Detail); err != nil {
			return err
		}
	}
	for _, v := range r.Variables {
		changed := 0
		if v.Changed {
			changed = 1
		}
		if _, err := tx.Exec(`INSERT INTO anomaly_variables (anomaly_id, variable, baseline, actual, delta_pct, changed)
			VALUES (?, ?, ?, ?, ?, ?)`, anomalyID, v.Variable, v.Baseline, v.Actual, v.DeltaPct, changed); err != nil {
			return err
		}
	}

	statusKey := string(r.Type) + ":" + string(r.Severity)
	meterStatus := statusByAnomaly[statusKey]
	if meterStatus == "" {
		meterStatus = "OK"
	}
	if _, err := tx.Exec(`UPDATE meters SET status = ? WHERE meter_id = ?`, meterStatus, r.MeterID); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run persistence test to verify it passes**

Run: `cd backend && go test ./internal/api/... -run TestPersistResults -v`
Expected: PASS

- [ ] **Step 5: Escribir el test de los endpoints HTTP**

```go
// backend/internal/api/analysis_routes_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	req2 := authedRequest(t, srv, http.MethodGet, "/api/ai/analysis/"+itoa(resp.ID), nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on polling, got %d", rec2.Code)
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

func itoa(n int64) string {
	return json.Number(itoaHelper(n)).String()
}
func itoaHelper(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run "PostAIAnalyze" -v`
Expected: FAIL (registerAnalysisRoutes sigue siendo el stub vacío)

- [ ] **Step 7: Implementar `analysis.go` y reemplazar el stub en `router.go`**

```go
// backend/internal/api/analysis.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type analyzeRequest struct {
	MeterIDs     []string `json:"meter_ids"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	BaselineFrom string   `json:"baseline_from"`
	BaselineTo   string   `json:"baseline_to"`
}

func registerAnalysisRoutes(r chi.Router, s *Server) {
	r.Post("/ai/analyze", func(w http.ResponseWriter, req *http.Request) {
		var body analyzeRequest
		json.NewDecoder(req.Body).Decode(&body) // cuerpo vacío es válido (spec 03)

		scope := AnalysisScope{MeterIDs: body.MeterIDs}
		scope.From, scope.To = dataRangeOrDefault(s.DB, body.From, body.To)
		if body.BaselineFrom != "" && body.BaselineTo != "" {
			bf, _ := time.Parse(time.RFC3339, body.BaselineFrom)
			bt, _ := time.Parse(time.RFC3339, body.BaselineTo)
			scope.BaselineFrom, scope.BaselineTo = &bf, &bt
		}

		userID, _ := req.Context().Value(userIDKey).(int64)
		id, err := StartAnalysis(s.DB, scope, userID)
		if err != nil {
			switch e := err.(type) {
			case *ValidationError:
				writeError(w, http.StatusBadRequest, e.Message)
			case *ConflictError:
				writeError(w, http.StatusConflict, e.Error())
			default:
				writeError(w, http.StatusInternalServerError, "error iniciando el análisis")
			}
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"id": id, "status": "RUNNING"})
	})

	r.Get("/ai/analysis/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id inválido")
			return
		}
		result, err := loadAnalysisStatus(s.DB, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "análisis no encontrado")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	r.Get("/ai/analysis", func(w http.ResponseWriter, req *http.Request) {
		limit := 20
		if l := req.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil {
				limit = parsed
			}
		}
		history, err := loadAnalysisHistory(s.DB, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo historial")
			return
		}
		writeJSON(w, http.StatusOK, history)
	})
}

// dataRangeOrDefault usa from/to del body si vienen, o el rango completo de datos (spec 03 "sin cuerpo").
func dataRangeOrDefault(db interface{ QueryRow(string, ...interface{}) *sqlRow }, from, to string) (time.Time, time.Time) {
	if from != "" && to != "" {
		f, _ := time.Parse(time.RFC3339, from)
		t, _ := time.Parse(time.RFC3339, to)
		return f, t
	}
	return dataRangeFromDB(db)
}
```

Nota: `dataRangeOrDefault` recibe una interfaz mínima solo para que este archivo compile sin importar `*store.DB` dos veces; en la práctica, ajusta la firma a `func dataRangeOrDefault(db *store.DB, from, to string) (time.Time, time.Time)` (usa `*store.DB` directamente, como en el resto del código) — la versión con interfaz de arriba es innecesariamente indirecta, corrígela al implementar. `dataRangeFromDB`, `loadAnalysisStatus`, `loadAnalysisHistory` se implementan a continuación:

```go
// backend/internal/api/analysis.go (continuación)

func dataRangeFromDB(db *store.DB) (time.Time, time.Time) {
	var minTS, maxTS string
	db.QueryRow(`SELECT MIN(timestamp), MAX(timestamp) FROM readings`).Scan(&minTS, &maxTS)
	from, _ := time.Parse(time.RFC3339, minTS)
	to, _ := time.Parse(time.RFC3339, maxTS)
	return from, to
}

type analysisStatusResponse struct {
	ID       int64   `json:"id"`
	Status   string  `json:"status"`
	Stage    string  `json:"stage"`
	Progress float64 `json:"progress"`
	Summary  struct {
		Anomalies  int     `json:"anomalies"`
		Priority   int     `json:"priority"`
		AvgConf    float64 `json:"avg_confidence"`
		Message    string  `json:"message"`
	} `json:"summary"`
}

func loadAnalysisStatus(db *store.DB, id int64) (*analysisStatusResponse, error) {
	var resp analysisStatusResponse
	row := db.QueryRow(`SELECT id, status, stage, progress, anomalies_count, high_priority_count, avg_confidence, summary_message
		FROM analyses WHERE id = ?`, id)
	var anomaliesCount, highPriority sql.NullInt64
	var avgConf sql.NullFloat64
	var summary sql.NullString
	if err := row.Scan(&resp.ID, &resp.Status, &resp.Stage, &resp.Progress, &anomaliesCount, &highPriority, &avgConf, &summary); err != nil {
		return nil, err
	}
	resp.Summary.Anomalies = int(anomaliesCount.Int64)
	resp.Summary.Priority = int(highPriority.Int64)
	resp.Summary.AvgConf = avgConf.Float64
	resp.Summary.Message = summary.String
	return &resp, nil
}

func loadAnalysisHistory(db *store.DB, limit int) ([]analysisStatusResponse, error) {
	rows, err := db.Query(`SELECT id, status, stage, progress, anomalies_count, high_priority_count, avg_confidence, summary_message
		FROM analyses ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []analysisStatusResponse
	for rows.Next() {
		var resp analysisStatusResponse
		var anomaliesCount, highPriority sql.NullInt64
		var avgConf sql.NullFloat64
		var summary sql.NullString
		rows.Scan(&resp.ID, &resp.Status, &resp.Stage, &resp.Progress, &anomaliesCount, &highPriority, &avgConf, &summary)
		resp.Summary.Anomalies = int(anomaliesCount.Int64)
		resp.Summary.Priority = int(highPriority.Int64)
		resp.Summary.AvgConf = avgConf.Float64
		resp.Summary.Message = summary.String
		out = append(out, resp)
	}
	return out, nil
}
```

Ajustes finales de imports en `analysis.go`: añadir `"database/sql"` y `"energy-management/internal/store"`, y **eliminar** la firma indirecta de `dataRangeOrDefault` mostrada arriba, dejándola como:

```go
func dataRangeOrDefault(db *store.DB, from, to string) (time.Time, time.Time) {
	if from != "" && to != "" {
		f, _ := time.Parse(time.RFC3339, from)
		t, _ := time.Parse(time.RFC3339, to)
		return f, t
	}
	return dataRangeFromDB(db)
}
```

En `router.go`: eliminar el stub `func registerAnalysisRoutes(r chi.Router, s *Server) {}`.

- [ ] **Step 8: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add backend/internal/api/analysis_runner.go backend/internal/api/analysis.go backend/internal/api/analysis_persist_test.go backend/internal/api/analysis_routes_test.go
git commit -m "feat(api): persist engine results transactionally and expose /ai/analyze + polling"
```

---

### Task 6: `GET /meters`, `/meters/:id`, `/meters/:id/readings`, `/meters/:id/events`, `/meters/:id/anomalies`

**Files:**
- Create: `backend/internal/api/meters.go` (reemplaza el stub `registerMeterRoutes`)
- Test: `backend/internal/api/meters_routes_test.go`

**Interfaces:**
- Consumes: `CurrentAnomaly`, `ActiveWindow`, `Overlaps`, `BaselineForRange` (Task 3).
- Produces: contratos JSON de `GET /meters` y `GET /meters/:meterId` exactamente como spec 03 §"Contratos clave" — consumidos por el frontend (Dashboard, Meters, Detalle).

- [ ] **Step 1: Escribir el test — medidor con anomalía, medidor sin análisis (Review Focus), y `in_range`**

```go
// backend/internal/api/meters_routes_test.go
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
			MeterID      string   `json:"meter_id"`
			VariationPct *float64 `json:"variation_pct"`
			Status       string   `json:"status"`
		} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	var m109, m999 *struct {
		MeterID      string   `json:"meter_id"`
		VariationPct *float64 `json:"variation_pct"`
		Status       string   `json:"status"`
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
	if m999 == nil || m999.VariationPct != nil || m999.Status != "UNKNOWN" {
		t.Fatalf("expected M-999 with null variation_pct and UNKNOWN status, got %+v", m999)
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run "GetMeters" -v`
Expected: FAIL (registerMeterRoutes sigue siendo el stub vacío)

- [ ] **Step 3: Implementar `meters.go`**

```go
// backend/internal/api/meters.go
package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"energy-management/internal/store"
)

type meterListItem struct {
	MeterID      string   `json:"meter_id"`
	Name         string   `json:"name"`
	ConsumptionKWh *float64 `json:"consumption_kwh"`
	BaselineKWh  *float64 `json:"baseline_kwh"`
	VariationPct *float64 `json:"variation_pct"`
	Status       string   `json:"status"`
	AnalysisID   *int64   `json:"analysis_id"`
	Anomaly      *anomalySummary `json:"anomaly"`
}

type anomalySummary struct {
	ID           int64   `json:"id"`
	Type         string  `json:"type"`
	Severity     string  `json:"severity"`
	Confidence   float64 `json:"confidence"`
	PriorityRank int     `json:"priority_rank"`
	InRange      bool    `json:"in_range"`
	ActiveFrom   string  `json:"active_from"`
	ActiveTo     string  `json:"active_to"`
}

func registerMeterRoutes(r chi.Router, s *Server) {
	r.Get("/meters", func(w http.ResponseWriter, req *http.Request) {
		from, to, hasRange := parseRangeParams(req)
		ranks := priorityRanks(s.DB)

		rows, err := s.DB.Query(`SELECT meter_id, name, status FROM meters`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo medidores")
			return
		}
		defer rows.Close()

		var items []meterListItem
		for rows.Next() {
			var meterID, name, status string
			rows.Scan(&meterID, &name, &status)
			item := meterListItem{MeterID: meterID, Name: name, Status: status}

			anomaly, _ := CurrentAnomaly(s.DB, meterID)
			if anomaly != nil {
				item.AnalysisID = &anomaly.AnalysisID
				actualFrom, actualTo := from, to
				if !hasRange {
					actualFrom, actualTo = anomaly.PeriodFrom, anomaly.PeriodTo
				}
				baselineKWh, actualKWh, _ := BaselineForRange(s.DB, meterID, actualFrom, actualTo)
				variationPct := 0.0
				if baselineKWh != 0 {
					variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
				}
				item.ConsumptionKWh, item.BaselineKWh, item.VariationPct = &actualKWh, &baselineKWh, &variationPct

				activeFrom, activeTo := ActiveWindow(*anomaly)
				inRange := true
				if hasRange {
					inRange = Overlaps(activeFrom, activeTo, from, to)
				}
				item.Anomaly = &anomalySummary{
					ID: anomaly.ID, Type: anomaly.Type, Severity: anomaly.Severity, Confidence: anomaly.Confidence,
					PriorityRank: ranks[anomaly.ID], InRange: inRange,
					ActiveFrom: activeFrom.Format(time.RFC3339), ActiveTo: activeTo.Format(time.RFC3339),
				}
			}
			items = append(items, item)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
	})

	r.Get("/meters/{meterId}", func(w http.ResponseWriter, req *http.Request) {
		meterID := chi.URLParam(req, "meterId")
		var name, status string
		err := s.DB.QueryRow(`SELECT name, status FROM meters WHERE meter_id = ?`, meterID).Scan(&name, &status)
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "medidor no encontrado")
			return
		}
		item := meterListItem{MeterID: meterID, Name: name, Status: status}
		anomaly, _ := CurrentAnomaly(s.DB, meterID)
		if anomaly != nil {
			item.AnalysisID = &anomaly.AnalysisID
			baselineKWh, actualKWh, _ := BaselineForRange(s.DB, meterID, anomaly.PeriodFrom, anomaly.PeriodTo)
			variationPct := 0.0
			if baselineKWh != 0 {
				variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
			}
			item.ConsumptionKWh, item.BaselineKWh, item.VariationPct = &actualKWh, &baselineKWh, &variationPct
		}
		writeJSON(w, http.StatusOK, item)
	})

	r.Get("/meters/{meterId}/readings", func(w http.ResponseWriter, req *http.Request) {
		meterID := chi.URLParam(req, "meterId")
		from, to, _ := parseRangeParams(req)
		rows, err := s.DB.Query(`SELECT timestamp, consumption_kwh, voltage_v, current_a, power_factor
			FROM readings WHERE meter_id = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp`,
			meterID, from.Format(time.RFC3339), to.Format(time.RFC3339))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo lecturas")
			return
		}
		defer rows.Close()
		type point struct {
			Timestamp string `json:"timestamp"`
			ConsumptionKWh float64 `json:"consumption_kwh"`
			VoltageV float64 `json:"voltage_v"`
			CurrentA float64 `json:"current_a"`
			PowerFactor float64 `json:"power_factor"`
		}
		var out []point
		for rows.Next() {
			var p point
			rows.Scan(&p.Timestamp, &p.ConsumptionKWh, &p.VoltageV, &p.CurrentA, &p.PowerFactor)
			out = append(out, p)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
	})

	r.Get("/meters/{meterId}/events", func(w http.ResponseWriter, req *http.Request) {
		meterID := chi.URLParam(req, "meterId")
		rows, err := s.DB.Query(`SELECT timestamp, type, description FROM events WHERE meter_id = ? ORDER BY timestamp`, meterID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo eventos")
			return
		}
		defer rows.Close()
		type ev struct {
			Timestamp string `json:"timestamp"`
			Type string `json:"type"`
			Description string `json:"description"`
		}
		var out []ev
		for rows.Next() {
			var e ev
			rows.Scan(&e.Timestamp, &e.Type, &e.Description)
			out = append(out, e)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
	})

	r.Get("/meters/{meterId}/anomalies", func(w http.ResponseWriter, req *http.Request) {
		meterID := chi.URLParam(req, "meterId")
		scope := req.URL.Query().Get("scope")
		query := `SELECT a.id, a.type, a.severity, a.confidence, a.status FROM anomalies a
			JOIN analyses an ON an.id = a.analysis_id WHERE a.meter_id = ? AND an.status = 'COMPLETED'`
		if scope != "history" {
			query += ` ORDER BY an.finished_at DESC LIMIT 1`
		} else {
			query += ` ORDER BY an.finished_at DESC`
		}
		rows, err := s.DB.Query(query, meterID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
			return
		}
		defer rows.Close()
		type item struct {
			ID int64 `json:"id"`
			Type string `json:"type"`
			Severity string `json:"severity"`
			Confidence float64 `json:"confidence"`
			Status string `json:"status"`
		}
		var out []item
		for rows.Next() {
			var i item
			rows.Scan(&i.ID, &i.Type, &i.Severity, &i.Confidence, &i.Status)
			out = append(out, i)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
	})
}

func parseRangeParams(req *http.Request) (from, to time.Time, hasRange bool) {
	fromStr := req.URL.Query().Get("from")
	toStr := req.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, false
	}
	from, _ = time.Parse(time.RFC3339, fromStr)
	to, _ = time.Parse(time.RFC3339, toStr)
	return from, to, true
}

// priorityRanks calcula el rank de todas las anomalías vigentes de la flota,
// ordenadas por priority_score descendente (spec 03: "no se guarda, se calcula al consultar").
func priorityRanks(db *store.DB) map[int64]int {
	rows, err := db.Query(`SELECT a.id FROM anomalies a
		JOIN analyses an ON an.id = a.analysis_id
		WHERE an.status = 'COMPLETED' AND an.id = (
			SELECT MAX(an2.id) FROM analyses an2
			JOIN anomalies a2 ON a2.analysis_id = an2.id
			WHERE a2.meter_id = a.meter_id AND an2.status = 'COMPLETED'
		)
		ORDER BY a.priority_score DESC`)
	if err != nil {
		return map[int64]int{}
	}
	defer rows.Close()
	ranks := map[int64]int{}
	rank := 0
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		rank++
		ranks[id] = rank
	}
	return ranks
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/meters.go backend/internal/api/meters_routes_test.go
git commit -m "feat(api): GET /meters, /meters/:id, readings, events, anomalies endpoints"
```

---

### Task 7: `GET/PATCH /anomalies`, `GET /dashboard/summary`

**Files:**
- Create: `backend/internal/api/anomalies.go` (reemplaza el stub `registerAnomalyRoutes`)
- Create: `backend/internal/api/dashboard.go` (reemplaza el stub `registerDashboardRoutes`)
- Test: `backend/internal/api/anomalies_routes_test.go`
- Test: `backend/internal/api/dashboard_routes_test.go`

**Interfaces:**
- Consumes: `ActiveWindow`, `Overlaps`, `priorityRanks` (Tasks 3 y 6).
- Produces: contratos JSON de `GET /anomalies`, `GET /anomalies/:id`, `PATCH /anomalies/:id`, `GET /dashboard/summary` — consumidos por el frontend (Anomalías, Investigación, Dashboard).

- [ ] **Step 1: Escribir los tests — filtro de fechas sin solape (Review Focus) y `PATCH` con status inválido (Review Focus)**

```go
// backend/internal/api/anomalies_routes_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestGetAnomalyByIDIncludesEvidence(t *testing.T) {
	srv := newTestServer(t)
	setupAnalysisWithAnomaly(t, srv.DB)
	srv.DB.Exec(`UPDATE anomalies SET evidence_json = ? WHERE id = 1`, bytes.NewBufferString(`{"decision_path":["no_data_quality_issue"]}`).String())
	router := NewRouter(srv)

	req := authedRequest(t, srv, http.MethodGet, "/api/anomalies/1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

```go
// backend/internal/api/dashboard_routes_test.go
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
		ByMeter   []interface{}             `json:"by_meter"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.DataRange.From == "" || resp.DataRange.To == "" {
		t.Fatal("expected non-empty data_range")
	}
	if len(resp.ByMeter) == 0 {
		t.Fatal("expected at least one meter in by_meter")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/api/... -run "GetAnomalies|PatchAnomaly|GetDashboard" -v`
Expected: FAIL (stubs vacíos)

- [ ] **Step 3: Implementar `anomalies.go`**

```go
// backend/internal/api/anomalies.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

var validAnomalyStatuses = map[string]bool{
	"OPEN": true, "INVESTIGATING": true, "RESOLVED": true, "DISMISSED": true,
}

func registerAnomalyRoutes(r chi.Router, s *Server) {
	r.Get("/anomalies", func(w http.ResponseWriter, req *http.Request) {
		from, to, hasRange := parseRangeParams(req)
		ranks := priorityRanks(s.DB)

		rows, err := s.DB.Query(`SELECT a.id, a.meter_id, a.type, a.severity, a.confidence,
			a.period_from, a.period_to, a.change_point_at, a.status
			FROM anomalies a JOIN analyses an ON an.id = a.analysis_id
			WHERE an.status = 'COMPLETED'
			ORDER BY a.priority_score DESC`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
			return
		}
		defer rows.Close()

		type item struct {
			ID int64 `json:"id"`
			MeterID string `json:"meter_id"`
			Type string `json:"type"`
			Severity string `json:"severity"`
			Confidence float64 `json:"confidence"`
			PriorityRank int `json:"priority_rank"`
			ActiveFrom string `json:"active_from"`
			ActiveTo string `json:"active_to"`
		}
		var out []item
		for rows.Next() {
			var i item
			var periodFrom, periodTo string
			var changePointAt sql.NullString
			var status string
			rows.Scan(&i.ID, &i.MeterID, &i.Type, &i.Severity, &i.Confidence, &periodFrom, &periodTo, &changePointAt, &status)

			a := AnomalyRow{ID: i.ID}
			a.PeriodFrom, _ = time.Parse(time.RFC3339, periodFrom)
			a.PeriodTo, _ = time.Parse(time.RFC3339, periodTo)
			if changePointAt.Valid {
				cp, _ := time.Parse(time.RFC3339, changePointAt.String)
				a.ChangePointAt = &cp
			}
			activeFrom, activeTo := ActiveWindow(a)
			if hasRange && !Overlaps(activeFrom, activeTo, from, to) {
				continue
			}
			i.PriorityRank = ranks[i.ID]
			i.ActiveFrom, i.ActiveTo = activeFrom.Format(time.RFC3339), activeTo.Format(time.RFC3339)
			out = append(out, i)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
	})

	r.Get("/anomalies/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		detail, err := loadAnomalyDetail(s.DB, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "anomalía no encontrada")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})

	r.Patch("/anomalies/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		var body struct{ Status string `json:"status"` }
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "cuerpo inválido")
			return
		}
		if !validAnomalyStatuses[body.Status] {
			writeError(w, http.StatusBadRequest, "status inválido")
			return
		}
		_, err := s.DB.Exec(`UPDATE anomalies SET status = ?, updated_at = ? WHERE id = ?`,
			body.Status, time.Now().UTC().Format(time.RFC3339), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error actualizando anomalía")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
	})
}

type anomalyDetail struct {
	ID int64 `json:"id"`
	MeterID string `json:"meter_id"`
	Type string `json:"type"`
	Severity string `json:"severity"`
	Confidence float64 `json:"confidence"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	RecommendedAction string `json:"recommended_action"`
	EvidenceJSON json.RawMessage `json:"evidence"`
}

func loadAnomalyDetail(db *store.DB, id int64) (*anomalyDetail, error) {
	var d anomalyDetail
	var evidenceStr string
	err := db.QueryRow(`SELECT id, meter_id, type, severity, confidence, status, reason, recommended_action, evidence_json
		FROM anomalies WHERE id = ?`, id).Scan(&d.ID, &d.MeterID, &d.Type, &d.Severity, &d.Confidence, &d.Status,
		&d.Reason, &d.RecommendedAction, &evidenceStr)
	if err != nil {
		return nil, err
	}
	if evidenceStr != "" {
		d.EvidenceJSON = json.RawMessage(evidenceStr)
	} else {
		d.EvidenceJSON = json.RawMessage(`{}`)
	}
	return &d, nil
}
```

Añadir `"database/sql"` y `"energy-management/internal/store"` a los imports de `anomalies.go`.

- [ ] **Step 4: Implementar `dashboard.go`**

```go
// backend/internal/api/dashboard.go
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func registerDashboardRoutes(r chi.Router, s *Server) {
	r.Get("/dashboard/summary", func(w http.ResponseWriter, req *http.Request) {
		from, to, hasRange := parseRangeParams(req)
		dataFrom, dataTo := dataRangeFromDB(s.DB)
		if !hasRange {
			from, to = dataFrom, dataTo
		}

		rows, err := s.DB.Query(`SELECT meter_id, status FROM meters`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo medidores")
			return
		}
		defer rows.Close()

		type byMeterItem struct {
			MeterID string `json:"meter_id"`
			ConsumptionKWh float64 `json:"consumption_kwh"`
			Status string `json:"status"`
		}
		var byMeter []byMeterItem
		total := 0.0
		anomaliesCount, highPriority := 0, 0
		var confSum float64
		confCount := 0

		for rows.Next() {
			var meterID, status string
			rows.Scan(&meterID, &status)
			_, actualKWh, _ := BaselineForRange(s.DB, meterID, from, to)
			byMeter = append(byMeter, byMeterItem{MeterID: meterID, ConsumptionKWh: actualKWh, Status: status})
			total += actualKWh

			anomaly, _ := CurrentAnomaly(s.DB, meterID)
			if anomaly != nil {
				activeFrom, activeTo := ActiveWindow(*anomaly)
				if Overlaps(activeFrom, activeTo, from, to) {
					anomaliesCount++
					if anomaly.Severity == "HIGH" {
						highPriority++
					}
					confSum += anomaly.Confidence
					confCount++
				}
			}
		}

		// ordenar by_meter de mayor a menor (para el pastel)
		for i := 1; i < len(byMeter); i++ {
			for j := i; j > 0 && byMeter[j].ConsumptionKWh > byMeter[j-1].ConsumptionKWh; j-- {
				byMeter[j], byMeter[j-1] = byMeter[j-1], byMeter[j]
			}
		}

		avgConfidence := 0.0
		if confCount > 0 {
			avgConfidence = confSum / float64(confCount)
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data_range": map[string]string{"from": dataFrom.Format(time.RFC3339), "to": dataTo.Format(time.RFC3339)},
			"period":     map[string]string{"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339)},
			"meters":     len(byMeter),
			"total_consumption_kwh": total,
			"anomalies": anomaliesCount, "high_priority": highPriority, "avg_confidence": avgConfidence,
			"by_meter": byMeter,
		})
	})
}
```

En `router.go`: eliminar los stubs `registerAnomalyRoutes` y `registerDashboardRoutes`.

- [ ] **Step 5: Run all tests to verify they pass**

Run: `cd backend && go test ./... -v`
Expected: PASS en todos los paquetes (`store`, `engine`, `auth`, `api`)

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/anomalies.go backend/internal/api/dashboard.go backend/internal/api/anomalies_routes_test.go backend/internal/api/dashboard_routes_test.go
git commit -m "feat(api): anomalies CRUD-lite endpoints and dashboard summary"
```

---

### Task 8: `cmd/api/main.go` — arranque completo

**Files:**
- Create: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: `store.Open/Migrate/LoadReadingsCSV/LoadEventsCSV` (plan de Motor), `api.NewRouter` (Task 1).
- Produces: binario `api` — usado por el Dockerfile de backend (spec 06, fuera de este plan) y por el desarrollo local (`go run ./cmd/api`).

- [ ] **Step 1: Implementar `main.go` (sin test unitario: es un `main` de composición; se verifica con arranque manual en el Step 2)**

```go
// backend/cmd/api/main.go
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"energy-management/internal/api"
	"energy-management/internal/auth"
	"energy-management/internal/store"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dbPath := getenv("DB_PATH", "./energy.db")
	dataDir := getenv("DATA_DIR", "./data")
	port := getenv("PORT", "8080")
	jwtSecret := getenv("JWT_SECRET", "change-me")
	demoEmail := getenv("DEMO_EMAIL", "demo@energy.local")
	demoPassword := getenv("DEMO_PASSWORD", "demo1234")

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("abriendo BD: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		log.Fatalf("migrando BD: %v", err)
	}

	seedIfEmpty(db, dataDir, demoEmail, demoPassword)

	router := api.NewRouter(&api.Server{DB: db, JWTSecret: jwtSecret})
	log.Printf("escuchando en :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

// seedIfEmpty carga readings.csv/events.csv y crea el usuario demo solo si
// la tabla readings está vacía (arranque idempotente, spec 06).
func seedIfEmpty(db *store.DB, dataDir, demoEmail, demoPassword string) {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&count)
	if count == 0 {
		if _, err := store.LoadReadingsCSV(db, dataDir+"/readings.csv"); err != nil {
			log.Printf("advertencia: no se pudo cargar readings.csv: %v", err)
		}
		if _, err := store.LoadEventsCSV(db, dataDir+"/events.csv"); err != nil {
			log.Printf("advertencia: no se pudo cargar events.csv: %v", err)
		}
		seedMetersFromReadings(db)
	}

	var userCount int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount)
	if userCount == 0 {
		hash, _ := auth.HashPassword(demoPassword)
		db.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, demoEmail, hash)
	}
}

func seedMetersFromReadings(db *store.DB) {
	rows, err := db.Query(`SELECT DISTINCT meter_id FROM readings`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var meterID string
		rows.Scan(&meterID)
		db.Exec(`INSERT OR IGNORE INTO meters (meter_id, status, created_at) VALUES (?, 'UNKNOWN', datetime('now'))`, meterID)
	}
}

var _ = sql.ErrNoRows // evita import no usado si se recorta código en el futuro
```

- [ ] **Step 2: Verificar arranque manual**

Run: `cd backend && go build ./... && DB_PATH=:memory: DATA_DIR=../data ./api & sleep 1 && curl -s http://localhost:8080/healthz && kill %1`
Expected: `{"status":"ok"}` y el proceso arranca sin panics

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/api/main.go
git commit -m "feat(api): wire up main.go with idempotent seed and HTTP listen"
```

---

### Task 9: Tests de integración de extremo a extremo (spec 05)

**Files:**
- Create: `backend/internal/api/integration_test.go`

**Interfaces:**
- Consumes: todos los endpoints de Tasks 2-7.

- [ ] **Step 1: Escribir el test de integración completo: login → analyze → polling → COMPLETED → /meters refleja el resultado**

```go
// backend/internal/api/integration_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFullFlowLoginAnalyzePollAndReadMeters(t *testing.T) {
	srv := newTestServer(t)
	seedDemoUser(t, srv, "demo@energy.local", "demo1234")
	srv.DB.Exec(`INSERT INTO meters (meter_id, status, created_at) VALUES ('M-101', 'UNKNOWN', ?)`, time.Now().Format(time.RFC3339))
	seedTwoWeeksOfReadings(t, srv.DB, "M-101", func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 25
		}
		return 10
	})
	router := NewRouter(srv)

	// 1. login
	loginBody := []byte(`{"email":"demo@energy.local","password":"demo1234"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytesReader(loginBody))
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	var loginResp struct{ Token string `json:"token"` }
	json.Unmarshal(loginRec.Body.Bytes(), &loginResp)
	if loginResp.Token == "" {
		t.Fatal("expected token from login")
	}

	// 2. POST /ai/analyze
	analyzeReq := httptest.NewRequest(http.MethodPost, "/api/ai/analyze", nil)
	analyzeReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	analyzeRec := httptest.NewRecorder()
	router.ServeHTTP(analyzeRec, analyzeReq)
	if analyzeRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", analyzeRec.Code, analyzeRec.Body.String())
	}
	var analyzeResp struct{ ID int64 `json:"id"` }
	json.Unmarshal(analyzeRec.Body.Bytes(), &analyzeResp)

	// 3. polling hasta COMPLETED
	waitForAnalysisDone(t, srv.DB, analyzeResp.ID, 3*time.Second)

	// 4. GET /meters refleja el resultado
	metersReq := httptest.NewRequest(http.MethodGet, "/api/meters", nil)
	metersReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	metersRec := httptest.NewRecorder()
	router.ServeHTTP(metersRec, metersReq)

	var metersResp struct {
		Items []struct {
			MeterID string `json:"meter_id"`
			Status  string `json:"status"`
		} `json:"items"`
	}
	json.Unmarshal(metersRec.Body.Bytes(), &metersResp)
	if len(metersResp.Items) != 1 || metersResp.Items[0].Status == "UNKNOWN" {
		t.Fatalf("expected M-101 status updated after analysis, got %+v", metersResp.Items)
	}
}

func bytesReader(b []byte) *bytesReaderT { return &bytesReaderT{b, 0} }

type bytesReaderT struct {
	b []byte
	i int
}

func (r *bytesReaderT) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
```

Nota: usa `bytes.NewReader` de la stdlib en lugar del `bytesReaderT` casero de arriba — simplifícalo a `bytes.NewReader(loginBody)` y añade `"bytes"` e `"io"` (si aún se necesita) a los imports; el tipo custom no aporta nada sobre `bytes.Reader` y se elimina al implementar.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run FullFlow -v`
Expected: puede fallar si algún endpoint previo tiene un bug de integración no cubierto por sus tests unitarios — ese es el propósito de este test

- [ ] **Step 3: Corregir cualquier fallo de integración encontrado y confirmar que pasa**

Run: `cd backend && go test ./... -v`
Expected: PASS en todo el módulo

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/integration_test.go
git commit -m "test(api): end-to-end login -> analyze -> poll -> meters integration test"
```

---

## Self-Review

**1. Cobertura de spec:** Auth y errores → Task 2. Endpoints de lectura con filtro de fechas → Task 6. `POST /ai/analyze` con alcance, polling, concurrencia → Tasks 4-5. Vigencia y prioridad → Task 3, 6, 7. Baseline y filtro de fechas → Task 3. Contratos clave (`/meters`, `/anomalies/:id`, `/ai/analysis/:id`, `/dashboard/summary`) → Tasks 6, 5, 7. Tests de integración → Task 9.

**2. Placeholders:** ninguno; el único punto marcado explícitamente como "a corregir al implementar" (la firma indirecta de `dataRangeOrDefault` en Task 5, y el `bytesReaderT` en Task 9) trae la corrección exacta inline, no es un placeholder abierto.

**3. Consistencia de tipos:** `AnalysisScope`, `ValidationError`, `ConflictError` (Task 4) se usan sin cambios en Task 5 (handler) y Task 9 (integración). `AnomalyRow`, `ActiveWindow`, `Overlaps`, `BaselineForRange` (Task 3) se reutilizan idénticos en Tasks 6 y 7.

**4. Review Focus:** los 5 casos (409 en análisis concurrente, medidor nunca analizado en `/meters`, filtro sin solape en `/anomalies`, `PATCH` con status inválido, `baseline_from/to` inválido) tienen test dedicado en Tasks 4, 6 y 7.
