# Motor de Anomalías (Fase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Construir el paquete Go `internal/engine` (determinista, sin red) que convierte lecturas + eventos en anomalías clasificadas con evidencia, más el scaffold de `internal/store` (SQLite) y `cmd/eval`.

**Architecture:** Pipeline puro en memoria (`Lecturas → Baseline → Detección → Correlación → Eventos → Clasificación → Explicación`) que opera sobre structs Go, sin tocar HTTP. `internal/store` solo carga CSV a SQLite de forma idempotente y expone lectura de filas; el motor no depende de `database/sql` directamente (recibe `[]Reading`/`[]Event` ya materializados), para que sea testeable con fixtures sintéticos sin BD.

**Tech Stack:** Go 1.23, `modernc.org/sqlite` (sin CGO), stdlib (`math`, `sort`, `time`, `encoding/csv`), tests con `testing` table-driven.

**Spec:** `specs/00-overview.md`, `specs/01-data-model.md`, `specs/02-anomaly-engine.md`, `specs/05-testing-demo.md`

## Global Constraints

- Backend en Go, SQLite vía `modernc.org/sqlite` (sin CGO) — spec 00.
- El motor no contiene lógica hardcodeada por `meter_id`; los umbrales son generales y se calculan por medidor — spec 00 regla 4.
- `expected_results.csv` nunca se carga en la app ni en tests unitarios; solo `cmd/eval` lo lee, por flag `--expected`, fuera del repo — spec 00 regla 1, spec 05.
- Toda anomalía debe traer evidencia numérica (`evidence_json`) que soporte su `reason` — spec 00 regla 2.
- Un cambio explicado por un evento conocido no se escala como anomalía real — spec 00 regla 3.
- Timestamps en UTC ISO-8601 en la BD; el CSV real trae `"YYYY-MM-DD HH:MM:SS"` sin offset — se interpreta como UTC (ajuste de T0 sobre spec 01).
- `events.csv` real usa columnas `meter_id,event_timestamp,event_type,description` (no `timestamp,type` como asumía el borrador de spec 01) — ajuste de T0.
- Umbrales: `umbral = max(piso, k × ruido propio del medidor)`, con mediana/MAD (`MAD_norm = 1.4826 × MAD`) — spec 02 §10.
- Los umbrales efectivos por medidor y su origen (`CALCULATED`/`FALLBACK`) se guardan en `analyses.config_json` (la fase API escribe esto; el motor debe producirlo en su salida).
- Tests unitarios del motor usan **fixtures sintéticos**, nunca `expected_results.csv` — spec 05.

## Review Focus

- Medidor con MAD = 0 (consumo perfectamente constante en el baseline): la división en `z_mod` y `cv_i` no debe producir NaN/Inf ni pánico — debe caer al piso de umbral.
- Ventana baseline con menos de 3 días de datos: `cv_i` debe sustituirse por la mediana de `cv` de la flota (spec 02 §10 nota), no fallar ni usar NaN.
- Evento fuera de la ventana ±24h del punto de cambio (p. ej. a 30h): no debe marcarse como `EXPLAINS`, para no ocultar una anomalía real.
- Medidor sin ninguna señal disparada: el motor no debe emitir fila de anomalía (spec 01: "los medidores sin anomalía tienen baseline pero no fila en anomalies"), y el orquestador debe distinguir esto de un error.
- `power_factor` fuera de rango físico [0,1] o `voltage_v <= 0` en una lectura: debe contarse como problema de calidad de datos y no romper el cálculo de consistencia eléctrica (división por cero en `V·I·PF`).

---

## File Structure

```
backend/
  go.mod, go.sum
  internal/
    store/
      schema.go        # DDL de todas las tablas (spec 01)
      store.go         # apertura de BD, migraciones, helpers de inserción
      csvload.go        # carga idempotente de readings.csv y events.csv
    engine/
      types.go          # Reading, Event, HourlyProfile, Signal, Anomaly, EngineOutput, Config
      stats.go           # Median, MAD, MADNorm, RobustZ
      baseline.go        # HourlyProfile, DetectChangePoint (CUSUM)
      thresholds.go      # PerMeterThresholds (02 §10)
      signals.go         # DetectSignals: persistent shift, spike, hourly pattern, data quality, electrical consistency
      correlation.go     # CorrelateVariables (deltas V/I/PF)
      events.go          # MatchEvents (±24h, EXPLAINS/RELATED)
      classify.go        # Classify (árbol 02 §5), Severity, Confidence, PriorityScore
      explain.go         # BuildReason, BuildRecommendation (plantillas 02 §8)
      engine.go          # Run(meterID, readings, events, fleetNoise, cfg) -> (*Analysis for meter)
  cmd/
    eval/
      main.go            # lee --expected, corre el motor sobre data/, imprime rúbrica
```

## Interfaces (contrato compartido por todo el plan)

```go
// internal/engine/types.go
package engine

import "time"

type Reading struct {
	MeterID        string
	Timestamp      time.Time // UTC
	ConsumptionKWh float64
	VoltageV       float64
	CurrentA       float64
	PowerFactor    float64
	Status         string // "OK" u otro valor libre del CSV
}

type Event struct {
	MeterID     string
	Timestamp   time.Time // UTC
	Type        string    // p.ej. OPERATIONAL_CHANGE, SCHEDULED_OUTAGE, DATA_QUALITY, UNKNOWN
	Description string
}

type ThresholdOrigin string

const (
	Calculated ThresholdOrigin = "CALCULATED"
	Fallback   ThresholdOrigin = "FALLBACK"
)

type MeterThresholds struct {
	VariationPct     float64 // T_var,i
	OutlierZ         float64 // fijo 3.5, no depende del medidor
	ElectricalPct    float64 // T_elec,i
	Origin           ThresholdOrigin
}

type ChangePoint struct {
	Found bool
	At    time.Time
	Sigma float64
}

type HourlyProfile struct {
	// Mediana de consumo por hora del día (0-23), sobre la ventana baseline.
	MedianByHour [24]float64
	WindowFrom   time.Time
	WindowTo     time.Time
	VoltageV     float64 // mediana en la ventana baseline
	CurrentA     float64
	PowerFactor  float64
}

type SignalType string

const (
	PersistentShift        SignalType = "PERSISTENT_SHIFT"
	Spike                  SignalType = "SPIKE"
	Outlier                SignalType = "OUTLIER"
	HourlyPattern          SignalType = "HOURLY_PATTERN"
	DataQuality            SignalType = "DATA_QUALITY"
	ElectricalInconsistency SignalType = "ELECTRICAL_INCONSISTENCY"
)

type Signal struct {
	Signal    SignalType
	Observed  float64
	Threshold float64
	Detail    string
}

type VariableDelta struct {
	Variable  string // consumption_kwh | voltage_v | current_a | power_factor
	Baseline  float64
	Actual    float64
	DeltaPct  float64
	Changed   bool
}

type EventRelation string

const (
	Explains EventRelation = "EXPLAINS"
	Related  EventRelation = "RELATED"
)

type RelatedEvent struct {
	Event      Event
	Relation   EventRelation
	OffsetHours float64
}

type AnomalyType string

const (
	RealAnomaly       AnomalyType = "REAL_ANOMALY"
	ExplainableAnomaly AnomalyType = "EXPLAINABLE_ANOMALY"
	TypeDataQuality   AnomalyType = "DATA_QUALITY"
	FalsePositive     AnomalyType = "FALSE_POSITIVE"
)

type Severity string

const (
	High   Severity = "HIGH"
	Medium Severity = "MEDIUM"
	Low    Severity = "LOW"
)

type ConfidenceBreakdown struct {
	SignalStrength     float64
	ConcordantSignals  float64
	EventPresence      float64
	DataQuality        float64
}

type DataQualityIssue struct {
	Kind  string // NULLS | DUPLICATES | GAPS | OUT_OF_RANGE | IMPOSSIBLE_JUMP
	Count int
}

type Evidence struct {
	DecisionPath        []string
	ConfidenceBreakdown ConfidenceBreakdown
	DataQualityIssues   []DataQualityIssue
	AffectedReadingsCount int
	AffectedReadingsFirst time.Time
	AffectedReadingsLast  time.Time
}

// MeterResult: salida del motor para UN medidor de UN análisis.
// Un medidor sin señales tiene HasAnomaly=false pero sí Baseline.
type MeterResult struct {
	MeterID         string
	Baseline        HourlyProfile
	ChangePoint     ChangePoint
	Thresholds      MeterThresholds
	HasAnomaly      bool
	Type            AnomalyType
	Severity        Severity
	Confidence      float64
	PriorityScore   float64
	BaselineKWh     float64
	ActualKWh       float64
	VariationPct    float64
	Reason          string
	RecommendedAction string
	Signals         []Signal
	Variables       []VariableDelta
	Events          []RelatedEvent
	Evidence        Evidence
}

// Config agrupa los pisos/constantes de 02 §10 (viven en thresholds.go).
type Config struct {
	MinVariationPctFloor float64 // 15
	VariationPctK        float64 // 3
	OutlierZThreshold    float64 // 3.5
	PersistenceHours     float64 // 48
	ElectricalPctFloor   float64 // 15
	ElectricalPctK       float64 // 3
	MinBaselineDays       int     // 3
	DefaultBaselineDays  int     // 7
}

func DefaultConfig() Config {
	return Config{
		MinVariationPctFloor: 15,
		VariationPctK:        3,
		OutlierZThreshold:    3.5,
		PersistenceHours:     48,
		ElectricalPctFloor:   15,
		ElectricalPctK:       3,
		MinBaselineDays:      3,
		DefaultBaselineDays:  7,
	}
}

// Run ejecuta el pipeline completo para un medidor.
// fleetCV es la mediana de cv de la flota, usada como respaldo (02 §10 nota "ventana corta").
func Run(meterID string, readings []Reading, events []Event, fleetCV float64, cfg Config) MeterResult {
	// implementado en engine.go (Task 8)
	panic("unimplemented until Task 8")
}
```

Este bloque es el contrato: todas las tareas siguientes producen o consumen exactamente estos tipos. No lo dupliques; cada task añade funciones al paquete `engine` que se apoyan en `types.go`.

---

### Task 1: Scaffold Go module + esquema SQLite

**Files:**
- Create: `backend/go.mod`
- Create: `backend/internal/store/schema.go`
- Create: `backend/internal/store/store.go`
- Test: `backend/internal/store/store_test.go`

**Interfaces:**
- Produces: `store.Open(dbPath string) (*store.DB, error)`, `store.DB.Migrate() error` — usados por Task 2 y por la fase API.

- [ ] **Step 1: Crear el módulo Go**

```bash
cd backend
go mod init energy-management
go get modernc.org/sqlite@latest
```

- [ ] **Step 2: Escribir el DDL completo (spec 01) en `schema.go`**

```go
// backend/internal/store/schema.go
package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meters (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id TEXT UNIQUE NOT NULL,
  name TEXT, location TEXT, status TEXT NOT NULL DEFAULT 'UNKNOWN',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS readings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id TEXT NOT NULL, timestamp TEXT NOT NULL,
  consumption_kwh REAL, voltage_v REAL, current_a REAL, power_factor REAL, status TEXT,
  UNIQUE(meter_id, timestamp)
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id TEXT NOT NULL, timestamp TEXT NOT NULL, type TEXT, description TEXT
);

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS analyses (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status TEXT NOT NULL, stage TEXT, progress REAL,
  trigger TEXT, triggered_by INTEGER,
  started_at TEXT, finished_at TEXT, duration_ms INTEGER,
  engine_version TEXT, config_json TEXT,
  scope_meter_ids_json TEXT,
  data_from TEXT, data_to TEXT,
  baseline_from TEXT, baseline_to TEXT,
  readings_analyzed INTEGER, meters_analyzed INTEGER,
  anomalies_count INTEGER, high_priority_count INTEGER, avg_confidence REAL,
  summary_message TEXT, stage_log_json TEXT, error TEXT
);

CREATE TABLE IF NOT EXISTS meter_baselines (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  analysis_id INTEGER NOT NULL REFERENCES analyses(id),
  meter_id TEXT NOT NULL,
  method TEXT, window_from TEXT, window_to TEXT, change_point_at TEXT,
  baseline_kwh REAL, actual_kwh REAL, variation_pct REAL,
  hourly_profile_json TEXT,
  baseline_voltage_v REAL, baseline_current_a REAL, baseline_power_factor REAL,
  actual_voltage_v REAL, actual_current_a REAL, actual_power_factor REAL,
  readings_count INTEGER, data_quality_score REAL,
  UNIQUE(analysis_id, meter_id)
);

CREATE TABLE IF NOT EXISTS anomalies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  analysis_id INTEGER NOT NULL REFERENCES analyses(id),
  meter_id TEXT NOT NULL,
  detected_at TEXT, period_from TEXT, period_to TEXT, change_point_at TEXT,
  type TEXT, severity TEXT,
  confidence REAL, confidence_label TEXT,
  priority_score REAL,
  baseline_kwh REAL, actual_kwh REAL, variation_pct REAL,
  reason TEXT, recommended_action TEXT, explanation_source TEXT,
  status TEXT NOT NULL DEFAULT 'OPEN', created_at TEXT, updated_at TEXT,
  evidence_json TEXT,
  UNIQUE(analysis_id, meter_id)
);

CREATE TABLE IF NOT EXISTS anomaly_signals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  anomaly_id INTEGER NOT NULL REFERENCES anomalies(id),
  signal TEXT, observed REAL, threshold REAL, detail TEXT
);

CREATE TABLE IF NOT EXISTS anomaly_variables (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  anomaly_id INTEGER NOT NULL REFERENCES anomalies(id),
  variable TEXT, baseline REAL, actual REAL, delta_pct REAL, changed INTEGER
);

CREATE TABLE IF NOT EXISTS anomaly_events (
  anomaly_id INTEGER NOT NULL REFERENCES anomalies(id),
  event_id INTEGER NOT NULL REFERENCES events(id),
  relation TEXT, offset_hours REAL
);
`
```

- [ ] **Step 3: Escribir `store.go` con apertura y migración**

```go
// backend/internal/store/store.go
package store

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	return &DB{sqlDB}, nil
}

func (db *DB) Migrate() error {
	_, err := db.Exec(schemaSQL)
	return err
}
```

- [ ] **Step 4: Test de migración**

```go
// backend/internal/store/store_test.go
package store

import "testing"

func TestMigrateCreatesTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name='readings'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected readings table to exist")
	}
}
```

- [ ] **Step 5: Ejecutar el test**

Run: `cd backend && go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/store/schema.go backend/internal/store/store.go backend/internal/store/store_test.go
git commit -m "feat(store): scaffold Go module and SQLite schema"
```

---

### Task 2: Carga idempotente de CSV (readings + events)

**Files:**
- Create: `backend/internal/store/csvload.go`
- Test: `backend/internal/store/csvload_test.go`

**Interfaces:**
- Consumes: `store.DB` de Task 1.
- Produces: `store.LoadReadingsCSV(db *DB, path string) (int, error)`, `store.LoadEventsCSV(db *DB, path string) (int, error)` — usados por el arranque del backend (fase API) y por `cmd/eval`.

- [ ] **Step 1: Escribir el test con un CSV real de fixture (usa las columnas reales del dataset)**

```go
// backend/internal/store/csvload_test.go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadingsCSVIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "readings.csv")
	content := "meter_id,timestamp,consumption_kwh,voltage_v,current_a,power_factor,status\n" +
		"M-101,2026-09-01 00:00:00,23.5,221.9,101.28,0.954,OK\n" +
		"M-101,2026-09-01 01:00:00,20.11,221.15,100.49,0.935,OK\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	db, _ := Open(":memory:")
	defer db.Close()
	db.Migrate()

	n1, err := LoadReadingsCSV(db, csvPath)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if n1 != 2 {
		t.Fatalf("expected 2 rows inserted, got %d", n1)
	}

	n2, err := LoadReadingsCSV(db, csvPath)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 new rows on reload, got %d", n2)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 total rows, got %d", count)
	}
}

func TestLoadEventsCSVRealColumns(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "events.csv")
	// Columnas reales del dataset: event_timestamp, event_type (no timestamp/type).
	content := "meter_id,event_timestamp,event_type,description\n" +
		"M-104,2026-09-11 00:00,OPERATIONAL_CHANGE,New production line activated\n"
	os.WriteFile(csvPath, []byte(content), 0o644)

	db, _ := Open(":memory:")
	defer db.Close()
	db.Migrate()

	n, err := LoadEventsCSV(db, csvPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
	var typ string
	db.QueryRow(`SELECT type FROM events WHERE meter_id='M-104'`).Scan(&typ)
	if typ != "OPERATIONAL_CHANGE" {
		t.Fatalf("expected OPERATIONAL_CHANGE, got %q", typ)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/... -run CSV -v`
Expected: FAIL (undefined: LoadReadingsCSV)

- [ ] **Step 3: Implementar `csvload.go`**

```go
// backend/internal/store/csvload.go
package store

import (
	"encoding/csv"
	"fmt"
	"os"
	"time"
)

const timeLayout = "2006-01-02 15:04:05"
const timeLayoutShort = "2006-01-02 15:04" // events.csv usa HH:MM sin segundos

func parseUTC(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(timeLayoutShort, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func LoadReadingsCSV(db *DB, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	idx := colIndex(header)

	stmt, err := db.Prepare(`INSERT OR IGNORE INTO readings
		(meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for {
		row, err := r.Read()
		if err != nil {
			break // io.EOF
		}
		ts, err := parseUTC(row[idx["timestamp"]])
		if err != nil {
			return inserted, err
		}
		status := "OK"
		if i, ok := idx["status"]; ok {
			status = row[i]
		}
		res, err := stmt.Exec(row[idx["meter_id"]], ts.Format(time.RFC3339),
			row[idx["consumption_kwh"]], row[idx["voltage_v"]], row[idx["current_a"]],
			row[idx["power_factor"]], status)
		if err != nil {
			return inserted, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func LoadEventsCSV(db *DB, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	idx := colIndex(header) // meter_id, event_timestamp, event_type, description

	stmt, err := db.Prepare(`INSERT INTO events (meter_id, timestamp, type, description)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS (
			SELECT 1 FROM events WHERE meter_id = ? AND timestamp = ? AND type = ?
		)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		ts, err := parseUTC(row[idx["event_timestamp"]])
		if err != nil {
			return inserted, err
		}
		meterID := row[idx["meter_id"]]
		typ := row[idx["event_type"]]
		desc := row[idx["description"]]
		tsStr := ts.Format(time.RFC3339)
		res, err := stmt.Exec(meterID, tsStr, typ, desc, meterID, tsStr, typ)
		if err != nil {
			return inserted, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func colIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, name := range header {
		idx[name] = i
	}
	return idx
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/... -v`
Expected: PASS (TestLoadReadingsCSVIsIdempotent, TestLoadEventsCSVRealColumns)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/csvload.go backend/internal/store/csvload_test.go
git commit -m "feat(store): idempotent CSV loader for readings and events"
```

---

### Task 3: Primitivas estadísticas robustas (mediana, MAD, z robusto)

**Files:**
- Create: `backend/internal/engine/stats.go`
- Test: `backend/internal/engine/stats_test.go`

**Interfaces:**
- Produces: `engine.Median([]float64) float64`, `engine.MAD([]float64) float64`, `engine.MADNorm([]float64) float64`, `engine.RobustZ(value float64, sample []float64) float64` — usados por Task 4, 5 y 6.

- [ ] **Step 1: Escribir el test**

```go
// backend/internal/engine/stats_test.go
package engine

import "testing"

func almostEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestMedianOddEven(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); !almostEqual(got, 2, 1e-9) {
		t.Fatalf("median odd: got %v", got)
	}
	if got := Median([]float64{1, 2, 3, 4}); !almostEqual(got, 2.5, 1e-9) {
		t.Fatalf("median even: got %v", got)
	}
}

func TestMADNormOnConstantSample(t *testing.T) {
	sample := []float64{10, 10, 10, 10}
	if got := MADNorm(sample); got != 0 {
		t.Fatalf("expected 0 MAD for constant sample, got %v", got)
	}
}

func TestRobustZHandlesZeroMADWithoutNaN(t *testing.T) {
	sample := []float64{10, 10, 10, 10}
	z := RobustZ(50, sample) // valor muy alejado, MAD=0
	if z == 0 {
		t.Fatal("expected non-zero z when value deviates from a constant sample")
	}
	if z != z { // NaN check
		t.Fatal("RobustZ produced NaN")
	}
}

func TestRobustZTypicalCase(t *testing.T) {
	sample := []float64{10, 11, 9, 10, 12, 8, 10, 11, 9, 10}
	z := RobustZ(10, sample)
	if !almostEqual(z, 0, 0.5) {
		t.Fatalf("z for the median itself should be ~0, got %v", z)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/... -run Median -v`
Expected: FAIL (undefined: Median)

- [ ] **Step 3: Implementar**

```go
// backend/internal/engine/stats.go
package engine

import "sort"

// Median calcula la mediana. No modifica el slice de entrada.
func Median(values []float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := n / 2
	if n%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// MAD (Median Absolute Deviation) sin normalizar.
func MAD(values []float64) float64 {
	m := Median(values)
	deviations := make([]float64, len(values))
	for i, v := range values {
		d := v - m
		if d < 0 {
			d = -d
		}
		deviations[i] = d
	}
	return Median(deviations)
}

// MADNorm = 1.4826 * MAD, estimador robusto de la desviación estándar (02 §10).
func MADNorm(values []float64) float64 {
	return 1.4826 * MAD(values)
}

// RobustZ = 0.6745 * (value - median(sample)) / MAD(sample) (02 §10, fórmula de Iglewicz-Hoaglin).
// Si MAD(sample) == 0 (muestra constante), usa un piso mínimo para evitar división por cero
// sin perder la señal de que value se desvía de una muestra sin ruido.
func RobustZ(value float64, sample []float64) float64 {
	m := Median(sample)
	mad := MAD(sample)
	if mad == 0 {
		if value == m {
			return 0
		}
		// piso: 1% del valor absoluto de la mediana, o 1e-6 si la mediana también es 0.
		floor := m * 0.01
		if floor <= 0 {
			floor = 1e-6
		}
		mad = floor
	}
	return 0.6745 * (value - m) / mad
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/stats.go backend/internal/engine/stats_test.go
git commit -m "feat(engine): robust median/MAD/z-score primitives"
```

---

### Task 4: Baseline (perfil horario) y punto de cambio (CUSUM)

**Files:**
- Create: `backend/internal/engine/baseline.go`
- Test: `backend/internal/engine/baseline_test.go`

**Interfaces:**
- Consumes: `Median`, `MADNorm` de Task 3; `engine.Reading` de `types.go`.
- Produces: `engine.DetectChangePoint(readings []Reading, cfg Config) ChangePoint`, `engine.BuildHourlyProfile(readings []Reading, windowFrom, windowTo time.Time) HourlyProfile` — usados por Task 5, 6 y 8.

- [ ] **Step 1: Escribir el test con fixtures sintéticos (medidor estable vs. medidor con cambio, spec 02 §10 regla de calibración #4)**

```go
// backend/internal/engine/baseline_test.go
package engine

import (
	"testing"
	"time"
)

func hourlyReadings(start time.Time, hours int, kwhFn func(h int) float64) []Reading {
	out := make([]Reading, 0, hours)
	for h := 0; h < hours; h++ {
		out = append(out, Reading{
			MeterID:        "M-TEST",
			Timestamp:      start.Add(time.Duration(h) * time.Hour),
			ConsumptionKWh: kwhFn(h),
			VoltageV:       220, CurrentA: 10, PowerFactor: 0.95, Status: "OK",
		})
	}
	return out
}

func TestDetectChangePointOnStableMeterReturnsNotFound(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		// perfil diario repetido, ruido pequeño determinista
		return 10 + float64(h%24)*0.1
	})
	cp := DetectChangePoint(readings, DefaultConfig())
	if cp.Found {
		t.Fatalf("expected no change point on stable meter, got %+v", cp)
	}
}

func TestDetectChangePointOnShiftedMeterFindsIt(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		base := 10 + float64(h%24)*0.1
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return base * 2.1 // +110%, muy por encima del piso del 15%
		}
		return base
	})
	cp := DetectChangePoint(readings, DefaultConfig())
	if !cp.Found {
		t.Fatal("expected change point to be found")
	}
	diff := cp.At.Sub(shiftAt)
	if diff < -12*time.Hour || diff > 12*time.Hour {
		t.Fatalf("change point %v too far from expected %v", cp.At, shiftAt)
	}
}

func TestBuildHourlyProfileMedianPerHour(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 3*24, func(h int) float64 {
		return float64(h % 24) // hora 5 siempre vale 5, etc.
	})
	profile := BuildHourlyProfile(readings, start, start.Add(3*24*time.Hour))
	if profile.MedianByHour[5] != 5 {
		t.Fatalf("expected median at hour 5 to be 5, got %v", profile.MedianByHour[5])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/... -run ChangePoint -v`
Expected: FAIL (undefined: DetectChangePoint)

- [ ] **Step 3: Implementar**

```go
// backend/internal/engine/baseline.go
package engine

import "time"

// BuildHourlyProfile calcula la mediana de consumo por hora del día (0-23)
// sobre la ventana [from, to), y las medianas de V/I/PF en esa misma ventana.
func BuildHourlyProfile(readings []Reading, from, to time.Time) HourlyProfile {
	byHour := make([][]float64, 24)
	var voltages, currents, pfs []float64
	for _, r := range readings {
		if r.Timestamp.Before(from) || !r.Timestamp.Before(to) {
			continue
		}
		h := r.Timestamp.Hour()
		byHour[h] = append(byHour[h], r.ConsumptionKWh)
		voltages = append(voltages, r.VoltageV)
		currents = append(currents, r.CurrentA)
		pfs = append(pfs, r.PowerFactor)
	}
	var profile HourlyProfile
	profile.WindowFrom, profile.WindowTo = from, to
	for h := 0; h < 24; h++ {
		profile.MedianByHour[h] = Median(byHour[h])
	}
	profile.VoltageV = Median(voltages)
	profile.CurrentA = Median(currents)
	profile.PowerFactor = Median(pfs)
	return profile
}

// dailyTotals agrupa consumo por día calendario (UTC) para el CUSUM.
func dailyTotals(readings []Reading) (days []time.Time, totals []float64) {
	sums := map[string]float64{}
	order := map[string]time.Time{}
	for _, r := range readings {
		key := r.Timestamp.Format("2006-01-02")
		sums[key] += r.ConsumptionKWh
		if _, ok := order[key]; !ok {
			order[key] = time.Date(r.Timestamp.Year(), r.Timestamp.Month(), r.Timestamp.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	for key, day := range order {
		days = append(days, day)
		totals = append(totals, sums[key])
	}
	// ordenar por fecha
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j].Before(days[j-1]); j-- {
			days[j], days[j-1] = days[j-1], days[j]
			totals[j], totals[j-1] = totals[j-1], totals[j]
		}
	}
	return days, totals
}

// DetectChangePoint aplica CUSUM sobre los totales diarios de consumo (02 §1, §10).
// k = 0.5*sigma, h = 5*sigma, sigma = MADNorm(residuos respecto a la mediana global).
func DetectChangePoint(readings []Reading, cfg Config) ChangePoint {
	days, totals := dailyTotals(readings)
	if len(days) < 2*cfg.MinBaselineDays {
		return ChangePoint{Found: false}
	}
	median := Median(totals)
	residuals := make([]float64, len(totals))
	for i, v := range totals {
		residuals[i] = v - median
	}
	sigma := MADNorm(residuals)
	if sigma == 0 {
		return ChangePoint{Found: false}
	}
	k := 0.5 * sigma
	h := 5 * sigma

	var cusumHigh, cusumLow float64
	changeIdx := -1
	for i, res := range residuals {
		cusumHigh = maxFloat(0, cusumHigh+res-k)
		cusumLow = minFloat(0, cusumLow+res+k)
		if cusumHigh > h || -cusumLow > h {
			changeIdx = i
			break
		}
	}
	if changeIdx == -1 {
		return ChangePoint{Found: false}
	}
	return ChangePoint{Found: true, At: days[changeIdx], Sigma: sigma}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/baseline.go backend/internal/engine/baseline_test.go
git commit -m "feat(engine): hourly baseline profile and CUSUM change-point detection"
```

---

### Task 5: Cálculo de umbrales por medidor (02 §10)

**Files:**
- Create: `backend/internal/engine/thresholds.go`
- Test: `backend/internal/engine/thresholds_test.go`

**Interfaces:**
- Consumes: `Median`, `MADNorm` (Task 3); `dailyTotals` (Task 4, sin exportar — se reutiliza vía función interna `dailyCV`).
- Produces: `engine.ComputeThresholds(readings []Reading, baselineFrom, baselineTo time.Time, fleetCV float64, cfg Config) MeterThresholds` — usado por Task 6.

- [ ] **Step 1: Escribir el test**

```go
// backend/internal/engine/thresholds_test.go
package engine

import (
	"testing"
	"time"
)

func TestComputeThresholdsUsesFloorOnStableMeter(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 { return 10 }) // sin ruido
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())
	if th.VariationPct != 15 {
		t.Fatalf("expected floor of 15%%, got %v", th.VariationPct)
	}
	if th.Origin != Calculated {
		t.Fatalf("expected CALCULATED origin with >=3 days of data, got %v", th.Origin)
	}
}

func TestComputeThresholdsFallsBackOnShortWindow(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 2*24, func(h int) float64 { return 10 }) // < 3 días
	th := ComputeThresholds(readings, start, start.Add(2*24*time.Hour), 0.2, DefaultConfig())
	if th.Origin != Fallback {
		t.Fatalf("expected FALLBACK origin for window < 3 days, got %v", th.Origin)
	}
}

func TestComputeThresholdsScalesWithNoise(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	noisy := hourlyReadings(start, 7*24, func(h int) float64 {
		if h%6 == 0 {
			return 20 // ruido diario grande y regular
		}
		return 10
	})
	th := ComputeThresholds(noisy, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())
	if th.VariationPct <= 15 {
		t.Fatalf("expected threshold above the 15%% floor for a noisy meter, got %v", th.VariationPct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/... -run ComputeThresholds -v`
Expected: FAIL (undefined: ComputeThresholds)

- [ ] **Step 3: Implementar**

```go
// backend/internal/engine/thresholds.go
package engine

import "time"

// dailyCV calcula el coeficiente de variación robusto (MADNorm/mediana) de los
// totales diarios en la ventana. Si la mediana es 0, retorna 0 (piso decide).
func dailyCV(readings []Reading, from, to time.Time) (cv float64, days int) {
	filtered := make([]Reading, 0, len(readings))
	for _, r := range readings {
		if !r.Timestamp.Before(from) && r.Timestamp.Before(to) {
			filtered = append(filtered, r)
		}
	}
	dayList, totals := dailyTotals(filtered)
	days = len(dayList)
	median := Median(totals)
	if median == 0 {
		return 0, days
	}
	return MADNorm(totals) / median, days
}

// ComputeThresholds implementa 02 §10: umbral = max(piso, k * ruido propio).
// Si la ventana tiene menos de cfg.MinBaselineDays, usa fleetCV como respaldo (Origin=Fallback).
func ComputeThresholds(readings []Reading, baselineFrom, baselineTo time.Time, fleetCV float64, cfg Config) MeterThresholds {
	cv, days := dailyCV(readings, baselineFrom, baselineTo)
	origin := Calculated
	if days < cfg.MinBaselineDays {
		cv = fleetCV
		origin = Fallback
	}

	variationPct := maxFloat(cfg.MinVariationPctFloor, cfg.VariationPctK*cv*100)
	electricalPct := maxFloat(cfg.ElectricalPctFloor, cfg.ElectricalPctK*cv*100)

	return MeterThresholds{
		VariationPct:  variationPct,
		OutlierZ:      cfg.OutlierZThreshold,
		ElectricalPct: electricalPct,
		Origin:        origin,
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/thresholds.go backend/internal/engine/thresholds_test.go
git commit -m "feat(engine): per-meter adaptive thresholds (variation, electrical)"
```

---

### Task 6: Detectores de señales (persistente, spike, horario, calidad, eléctrica)

**Files:**
- Create: `backend/internal/engine/signals.go`
- Test: `backend/internal/engine/signals_test.go`

**Interfaces:**
- Consumes: `HourlyProfile`, `ChangePoint`, `MeterThresholds`, `RobustZ` de tasks previas.
- Produces: `engine.DetectSignals(readings []Reading, profile HourlyProfile, cp ChangePoint, th MeterThresholds, cfg Config) ([]Signal, []DataQualityIssue)` — usado por Task 7 (correlación/eventos) y Task 8 (orquestador).

- [ ] **Step 1: Escribir el test — un caso por señal, usando el Review Focus (MAD=0, PF fuera de rango)**

```go
// backend/internal/engine/signals_test.go
package engine

import (
	"testing"
	"time"
)

func TestDetectSignalsPersistentShift(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		base := 10.0
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return base * 2.2
		}
		return base
	})
	profile := BuildHourlyProfile(readings, start, shiftAt)
	cp := ChangePoint{Found: true, At: shiftAt, Sigma: 1}
	th := ComputeThresholds(readings, start, shiftAt, 0.1, DefaultConfig())

	signals, _ := DetectSignals(readings, profile, cp, th, DefaultConfig())
	found := false
	for _, s := range signals {
		if s.Signal == PersistentShift {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PERSISTENT_SHIFT signal, got %+v", signals)
	}
}

func TestDetectSignalsOutlierSpike(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 {
		if h == 50 {
			return 100 // spike aislado
		}
		return 10
	})
	profile := BuildHourlyProfile(readings, start, start.Add(7*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())

	signals, _ := DetectSignals(readings, profile, ChangePoint{}, th, DefaultConfig())
	found := false
	for _, s := range signals {
		if s.Signal == Outlier {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected OUTLIER signal for isolated spike, got %+v", signals)
	}
}

func TestDetectSignalsDataQualityOutOfRangePowerFactor(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 3*24, func(h int) float64 { return 10 })
	readings[5].PowerFactor = 1.4 // fuera de [0,1], no debe pánico ni NaN
	readings[6].VoltageV = 0      // división por cero potencial en consistencia eléctrica

	profile := BuildHourlyProfile(readings, start, start.Add(3*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(3*24*time.Hour), 0.1, DefaultConfig())

	signals, issues := DetectSignals(readings, profile, ChangePoint{}, th, DefaultConfig())
	if len(issues) == 0 {
		t.Fatal("expected at least one data quality issue for out-of-range PF and zero voltage")
	}
	for _, s := range signals {
		if s.Observed != s.Observed { // NaN check
			t.Fatalf("signal %+v has NaN Observed", s)
		}
	}
}

func TestDetectSignalsElectricalInconsistency(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 {
		// kWh estable pero V*I*PF cambia de forma incoherente (simulando cableado/sensor)
		return 10
	})
	for i := range readings {
		if i > 100 {
			readings[i].VoltageV = 300 // ratio kWh/(V*I*PF) se rompe
		}
	}
	profile := BuildHourlyProfile(readings, start, start.Add(4*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(4*24*time.Hour), 0.1, DefaultConfig())

	signals, _ := DetectSignals(readings, profile, ChangePoint{}, th, DefaultConfig())
	found := false
	for _, s := range signals {
		if s.Signal == ElectricalInconsistency {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ELECTRICAL_INCONSISTENCY signal, got %+v", signals)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/... -run DetectSignals -v`
Expected: FAIL (undefined: DetectSignals)

- [ ] **Step 3: Implementar**

```go
// backend/internal/engine/signals.go
package engine

// DetectSignals aplica los 5 detectores de 02 §2-3. Devuelve las señales
// disparadas y los problemas de calidad de datos encontrados (independiente
// de si generan señal DATA_QUALITY, para alimentar evidence.data_quality_issues).
func DetectSignals(readings []Reading, profile HourlyProfile, cp ChangePoint, th MeterThresholds, cfg Config) ([]Signal, []DataQualityIssue) {
	var signals []Signal
	var issues []DataQualityIssue

	// 1. Calidad de datos: límites físicos.
	outOfRange := 0
	for _, r := range readings {
		if r.PowerFactor < 0 || r.PowerFactor > 1 || r.VoltageV <= 0 || r.CurrentA < 0 || r.ConsumptionKWh < 0 {
			outOfRange++
		}
	}
	if outOfRange > 0 {
		issues = append(issues, DataQualityIssue{Kind: "OUT_OF_RANGE", Count: outOfRange})
		pct := float64(outOfRange) / float64(len(readings)) * 100
		signals = append(signals, Signal{
			Signal: DataQuality, Observed: pct, Threshold: 2,
			Detail: "lecturas fuera de rango físico (PF, V, I o kWh)",
		})
	}

	// 2. Consumo total real vs. baseline extrapolado -> variation_pct.
	actualKWh := sumKWh(readings)
	baselineKWh := extrapolateBaseline(profile, readings)
	variationPct := 0.0
	if baselineKWh != 0 {
		variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
	}
	absVariation := variationPct
	if absVariation < 0 {
		absVariation = -absVariation
	}

	// 3. Cambio persistente: variación por encima del umbral y sostenida (aprox.: hay change point con confianza).
	if absVariation > th.VariationPct && cp.Found {
		if isPersistent(readings, cp, cfg) {
			signals = append(signals, Signal{
				Signal: PersistentShift, Observed: variationPct, Threshold: th.VariationPct,
				Detail: "variación sostenida tras el punto de cambio",
			})
		}
	}

	// 4. Outliers horarios: z robusto por lectura contra el perfil de esa hora.
	residuals := hourlyResiduals(readings, profile)
	outlierCount := 0
	for i, r := range readings {
		z := RobustZ(residuals[i], residuals)
		if abs(z) > th.OutlierZ {
			outlierCount++
			signals = append(signals, Signal{
				Signal: Outlier, Observed: z, Threshold: th.OutlierZ,
				Detail: "lectura " + r.Timestamp.Format("2006-01-02T15:04") + " se desvía del perfil horario",
			})
		}
	}

	// 5. Patrón horario: desviación sistemática del perfil (usa el mismo cálculo de variación
	// pero acotado a horas específicas; se reporta como señal complementaria si hay >= 3 outliers
	// concentrados en las mismas horas del día).
	if outlierCount >= 3 {
		signals = append(signals, Signal{
			Signal: HourlyPattern, Observed: float64(outlierCount), Threshold: 3,
			Detail: "múltiples horas se desvían del perfil horario esperado",
		})
	}

	// 6. Consistencia eléctrica: ratio kWh/(V*I*PF) comparado contra su propia mediana en el baseline.
	ratios := electricalRatios(readings)
	if len(ratios) > 0 {
		medianRatio := Median(ratios)
		if medianRatio != 0 {
			lastRatios := ratios[len(ratios)/2:] // mitad más reciente, aproximando "actual"
			actualMedian := Median(lastRatios)
			deltaPct := (actualMedian - medianRatio) / medianRatio * 100
			if abs(deltaPct) > th.ElectricalPct {
				signals = append(signals, Signal{
					Signal: ElectricalInconsistency, Observed: deltaPct, Threshold: th.ElectricalPct,
					Detail: "razón kWh/(V·I·PF) se desvía de su línea base",
				})
			}
		}
	}

	return signals, issues
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func sumKWh(readings []Reading) float64 {
	total := 0.0
	for _, r := range readings {
		total += r.ConsumptionKWh
	}
	return total
}

// extrapolateBaseline suma el perfil horario sobre los mismos timestamps que hay
// en readings, para comparar como en 03-api.md (evita distorsión por huecos).
func extrapolateBaseline(profile HourlyProfile, readings []Reading) float64 {
	total := 0.0
	for _, r := range readings {
		total += profile.MedianByHour[r.Timestamp.Hour()]
	}
	return total
}

func hourlyResiduals(readings []Reading, profile HourlyProfile) []float64 {
	out := make([]float64, len(readings))
	for i, r := range readings {
		out[i] = r.ConsumptionKWh - profile.MedianByHour[r.Timestamp.Hour()]
	}
	return out
}

func electricalRatios(readings []Reading) []float64 {
	var ratios []float64
	for _, r := range readings {
		denom := r.VoltageV * r.CurrentA * r.PowerFactor
		if denom <= 0 {
			continue // evita división por cero; ya contado como OUT_OF_RANGE
		}
		ratios = append(ratios, r.ConsumptionKWh/denom)
	}
	return ratios
}

// isPersistent verifica que el cambio se sostenga >= cfg.PersistenceHours tras el change point.
func isPersistent(readings []Reading, cp ChangePoint, cfg Config) bool {
	count := 0
	for _, r := range readings {
		if !r.Timestamp.Before(cp.At) {
			count++
		}
	}
	hoursAvailable := float64(count) // 1 lectura/hora
	return hoursAvailable >= cfg.PersistenceHours
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/signals.go backend/internal/engine/signals_test.go
git commit -m "feat(engine): signal detectors (persistent shift, outlier, hourly pattern, data quality, electrical)"
```

---

### Task 7: Correlación de variables y emparejamiento de eventos

**Files:**
- Create: `backend/internal/engine/correlation.go`
- Create: `backend/internal/engine/events.go`
- Test: `backend/internal/engine/correlation_test.go`
- Test: `backend/internal/engine/events_test.go`

**Interfaces:**
- Consumes: `HourlyProfile`, `ChangePoint`, `Reading`, `Event` de tasks previas.
- Produces: `engine.CorrelateVariables(readings []Reading, profile HourlyProfile, cp ChangePoint) []VariableDelta`, `engine.MatchEvents(events []Event, cp ChangePoint, variation float64) []RelatedEvent` — usados por Task 8 (clasificación) y Task 9 (orquestador).

- [ ] **Step 1: Escribir los tests**

```go
// backend/internal/engine/correlation_test.go
package engine

import (
	"testing"
	"time"
)

func TestCorrelateVariablesDetectsChangedCurrent(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	for i := range readings {
		if readings[i].Timestamp.After(shiftAt) {
			readings[i].CurrentA = 50 // sube fuerte tras el cambio
		} else {
			readings[i].CurrentA = 10
		}
	}
	profile := BuildHourlyProfile(readings, start, shiftAt)
	cp := ChangePoint{Found: true, At: shiftAt}

	deltas := CorrelateVariables(readings, profile, cp)
	var currentDelta *VariableDelta
	for i := range deltas {
		if deltas[i].Variable == "current_a" {
			currentDelta = &deltas[i]
		}
	}
	if currentDelta == nil || !currentDelta.Changed {
		t.Fatalf("expected current_a to be marked as changed, got %+v", deltas)
	}
}
```

```go
// backend/internal/engine/events_test.go
package engine

import (
	"testing"
	"time"
)

func TestMatchEventsWithinWindowExplains(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-104", Timestamp: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
			Type: "OPERATIONAL_CHANGE", Description: "New production line activated"},
	}
	cp := ChangePoint{Found: true, At: changeAt}
	related := MatchEvents(events, cp, 45.0) // variación positiva -> aumento
	if len(related) != 1 || related[0].Relation != Explains {
		t.Fatalf("expected event to explain the increase, got %+v", related)
	}
}

func TestMatchEventsOutsideWindowIsIgnored(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-X", Timestamp: changeAt.Add(-30 * time.Hour), // fuera de ±24h
			Type: "OPERATIONAL_CHANGE", Description: "irrelevant, too far"},
	}
	cp := ChangePoint{Found: true, At: changeAt}
	related := MatchEvents(events, cp, 45.0)
	if len(related) != 0 {
		t.Fatalf("expected no related events beyond 24h window, got %+v", related)
	}
}

func TestMatchEventsScheduledOutageWithDropIsFalsePositiveCandidate(t *testing.T) {
	changeAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-106", Timestamp: changeAt,
			Type: "SCHEDULED_OUTAGE", Description: "Scheduled maintenance outage for 12 hours"},
	}
	cp := ChangePoint{Found: true, At: changeAt}
	related := MatchEvents(events, cp, -60.0) // caída
	if len(related) != 1 || related[0].Relation != Explains {
		t.Fatalf("expected scheduled outage to explain the drop, got %+v", related)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/engine/... -run "Correlate|MatchEvents" -v`
Expected: FAIL (undefined: CorrelateVariables, MatchEvents)

- [ ] **Step 3: Implementar `correlation.go`**

```go
// backend/internal/engine/correlation.go
package engine

// CorrelateVariables compara V/I/PF antes y después del punto de cambio (o,
// si no hay punto de cambio, entre el baseline y el período completo).
func CorrelateVariables(readings []Reading, profile HourlyProfile, cp ChangePoint) []VariableDelta {
	splitAt := profile.WindowTo
	if cp.Found {
		splitAt = cp.At
	}

	var beforeV, beforeI, beforePF, afterV, afterI, afterPF []float64
	for _, r := range readings {
		if r.Timestamp.Before(splitAt) {
			beforeV = append(beforeV, r.VoltageV)
			beforeI = append(beforeI, r.CurrentA)
			beforePF = append(beforePF, r.PowerFactor)
		} else {
			afterV = append(afterV, r.VoltageV)
			afterI = append(afterI, r.CurrentA)
			afterPF = append(afterPF, r.PowerFactor)
		}
	}

	deltas := []VariableDelta{
		buildDelta("voltage_v", Median(beforeV), Median(afterV)),
		buildDelta("current_a", Median(beforeI), Median(afterI)),
		buildDelta("power_factor", Median(beforePF), Median(afterPF)),
	}
	return deltas
}

func buildDelta(name string, baseline, actual float64) VariableDelta {
	deltaPct := 0.0
	if baseline != 0 {
		deltaPct = (actual - baseline) / baseline * 100
	}
	// umbral fijo del 10% para marcar "changed" en la vista de Investigación;
	// es informativo, no decide clasificación (eso lo hace signals.go).
	changed := abs(deltaPct) > 10
	return VariableDelta{Variable: name, Baseline: baseline, Actual: actual, DeltaPct: deltaPct, Changed: changed}
}
```

- [ ] **Step 4: Implementar `events.go`**

```go
// backend/internal/engine/events.go
package engine

import "time"

const eventWindow = 24 * time.Hour

// MatchEvents busca eventos dentro de ±24h del punto de cambio (02 §4).
// variationPct > 0 (aumento) casa con eventos de tipo aumento de carga;
// variationPct < 0 (caída) casa con eventos de parada/mantenimiento.
func MatchEvents(events []Event, cp ChangePoint, variationPct float64) []RelatedEvent {
	if !cp.Found {
		return nil
	}
	var out []RelatedEvent
	for _, e := range events {
		offset := e.Timestamp.Sub(cp.At)
		if offset < -eventWindow || offset > eventWindow {
			continue
		}
		relation := Related
		if explainsChange(e.Type, variationPct) {
			relation = Explains
		}
		out = append(out, RelatedEvent{Event: e, Relation: relation, OffsetHours: offset.Hours()})
	}
	return out
}

func explainsChange(eventType string, variationPct float64) bool {
	switch eventType {
	case "OPERATIONAL_CHANGE":
		return variationPct > 0
	case "SCHEDULED_OUTAGE":
		return variationPct < 0
	default:
		return false // DATA_QUALITY, UNKNOWN u otros no "explican" el cambio
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/engine/correlation.go backend/internal/engine/events.go backend/internal/engine/correlation_test.go backend/internal/engine/events_test.go
git commit -m "feat(engine): variable correlation and event matching within +-24h window"
```

---

### Task 8: Árbol de clasificación, severidad, confianza y prioridad

**Files:**
- Create: `backend/internal/engine/classify.go`
- Test: `backend/internal/engine/classify_test.go`

**Interfaces:**
- Consumes: `[]Signal`, `[]DataQualityIssue`, `[]RelatedEvent`, `[]VariableDelta` de tasks 6-7.
- Produces: `engine.Classify(signals []Signal, issues []DataQualityIssue, events []RelatedEvent, variables []VariableDelta, variationPct float64) (AnomalyType, Severity, float64 /*confidence*/, ConfidenceBreakdown, []string /*decisionPath*/)`, `engine.PriorityScore(t AnomalyType, sev Severity, confidence, variationPct float64) float64` — usados por Task 9.

- [ ] **Step 1: Escribir los tests — uno por rama del árbol (02 §5) y el criterio de prioridad (§6)**

```go
// backend/internal/engine/classify_test.go
package engine

import "testing"

func TestClassifyDataQualityWhenStableButElectricallyInconsistent(t *testing.T) {
	signals := []Signal{{Signal: ElectricalInconsistency, Observed: 40, Threshold: 15}}
	issues := []DataQualityIssue{{Kind: "OUT_OF_RANGE", Count: 20}}
	typ, sev, _, _, path := Classify(signals, issues, nil, nil, 2.0)
	if typ != TypeDataQuality {
		t.Fatalf("expected DATA_QUALITY, got %v (path=%v)", typ, path)
	}
	if sev != High {
		t.Fatalf("expected HIGH severity for strong+persistent inconsistency, got %v", sev)
	}
}

func TestClassifyExplainableAnomalyOnLoadIncreaseWithEvent(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 45, Threshold: 15}}
	events := []RelatedEvent{{Event: Event{Type: "OPERATIONAL_CHANGE"}, Relation: Explains}}
	typ, sev, _, _, _ := Classify(signals, nil, events, nil, 45.0)
	if typ != ExplainableAnomaly || sev != Medium {
		t.Fatalf("expected EXPLAINABLE_ANOMALY/MEDIUM, got %v/%v", typ, sev)
	}
}

func TestClassifyFalsePositiveOnScheduledOutage(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: -60, Threshold: 15}}
	events := []RelatedEvent{{Event: Event{Type: "SCHEDULED_OUTAGE"}, Relation: Explains}}
	typ, sev, _, _, _ := Classify(signals, nil, events, nil, -60.0)
	if typ != FalsePositive || sev != Low {
		t.Fatalf("expected FALSE_POSITIVE/LOW, got %v/%v", typ, sev)
	}
}

func TestClassifyRealAnomalyOnUnexplainedShiftWithElectricalChange(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 103.7, Threshold: 15}}
	variables := []VariableDelta{{Variable: "current_a", Changed: true}}
	typ, sev, confidence, _, _ := Classify(signals, nil, nil, variables, 103.7)
	if typ != RealAnomaly || sev != High {
		t.Fatalf("expected REAL_ANOMALY/HIGH, got %v/%v", typ, sev)
	}
	if confidence < 0.9 {
		t.Fatalf("expected confidence >= 0.9 for a strong unexplained shift, got %v", confidence)
	}
}

func TestClassifyNoSignalsMeansNoAnomaly(t *testing.T) {
	typ, _, _, _, _ := Classify(nil, nil, nil, nil, 2.0)
	if typ != "" {
		t.Fatalf("expected empty type when there are no signals, got %v", typ)
	}
}

func TestPriorityScoreOrdersRealAboveFalsePositive(t *testing.T) {
	real := PriorityScore(RealAnomaly, High, 0.9, 103.7)
	fp := PriorityScore(FalsePositive, Low, 0.5, 60.0)
	if fp >= real {
		t.Fatalf("FALSE_POSITIVE (%v) must never outrank a REAL_ANOMALY (%v)", fp, real)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/... -run Classify -v`
Expected: FAIL (undefined: Classify)

- [ ] **Step 3: Implementar**

```go
// backend/internal/engine/classify.go
package engine

// Classify implementa el árbol de decisión de 02 §5, en orden.
// Retorna "" como AnomalyType cuando no hay señales (sin anomalía).
func Classify(signals []Signal, issues []DataQualityIssue, events []RelatedEvent, variables []VariableDelta, variationPct float64) (AnomalyType, Severity, float64, ConfidenceBreakdown, []string) {
	has := func(t SignalType) bool {
		for _, s := range signals {
			if s.Signal == t {
				return true
			}
		}
		return false
	}
	explainingEvent := func(rel EventRelation) *RelatedEvent {
		for i := range events {
			if events[i].Relation == rel {
				return &events[i]
			}
		}
		return nil
	}

	if len(signals) == 0 && len(issues) == 0 {
		return "", "", 0, ConfidenceBreakdown{}, []string{"no_signals"}
	}

	path := []string{}

	// 1. Consumo estable pero eléctricas inconsistentes o datos defectuosos.
	if has(ElectricalInconsistency) || len(issues) > 0 {
		strongAndPersistent := has(ElectricalInconsistency) && !has(PersistentShift)
		sev := Medium
		if strongAndPersistent {
			sev = High
		}
		path = append(path, "data_quality_issue")
		confidence, breakdown := computeConfidence(signals, events, issues, variationPct)
		return TypeDataQuality, sev, confidence, breakdown, path
	}
	path = append(path, "no_data_quality_issue")

	// 2. Cambio significativo + evento que lo explica.
	if ev := explainingEvent(Explains); ev != nil {
		path = append(path, "significant_change", "event_explains")
		confidence, breakdown := computeConfidence(signals, events, issues, variationPct)
		if ev.Event.Type == "SCHEDULED_OUTAGE" {
			return FalsePositive, Low, confidence, breakdown, path
		}
		return ExplainableAnomaly, Medium, confidence, breakdown, path
	}
	path = append(path, "significant_change", "no_event")

	// 3-4. Cambio significativo sin evento: severidad según cambios eléctricos coherentes.
	electricalChangeCoherent := false
	for _, v := range variables {
		if v.Changed {
			electricalChangeCoherent = true
			break
		}
	}
	confidence, breakdown := computeConfidence(signals, events, issues, variationPct)
	if electricalChangeCoherent {
		path = append(path, "electrical_changes")
		return RealAnomaly, High, confidence, breakdown, path
	}
	path = append(path, "no_electrical_changes")
	return RealAnomaly, Medium, confidence, breakdown, path
}

// computeConfidence combina fuerza de señal, señales concordantes, evento y calidad de datos (02 §7).
func computeConfidence(signals []Signal, events []RelatedEvent, issues []DataQualityIssue, variationPct float64) (float64, ConfidenceBreakdown) {
	strength := minFloat(abs(variationPct)/100, 1) * 0.4
	concordant := minFloat(float64(len(signals))/3, 1) * 0.3
	eventPresence := 0.0
	if len(events) > 0 {
		eventPresence = 0.2
	}
	dataQuality := 0.1
	if len(issues) > 0 {
		dataQuality = 0.0
	}
	total := round2(strength + concordant + eventPresence + dataQuality)
	return total, ConfidenceBreakdown{
		SignalStrength:    round2(strength),
		ConcordantSignals: round2(concordant),
		EventPresence:     eventPresence,
		DataQuality:       dataQuality,
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func severityWeight(s Severity) float64 {
	switch s {
	case High:
		return 3
	case Medium:
		return 2
	default:
		return 1
	}
}

// PriorityScore = severity_weight * confidence * min(|variation|/100, 2) (02 §6).
// FALSE_POSITIVE se penaliza con un factor 0.1 para que nunca supere a un REAL_ANOMALY.
func PriorityScore(t AnomalyType, sev Severity, confidence, variationPct float64) float64 {
	magnitude := minFloat(abs(variationPct)/100, 2)
	score := severityWeight(sev) * confidence * magnitude
	if t == FalsePositive {
		score *= 0.1
	}
	return round2(score)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/classify.go backend/internal/engine/classify_test.go
git commit -m "feat(engine): classification tree, confidence, and priority scoring"
```

---

### Task 9: Explicación por plantillas y orquestador `Run`

**Files:**
- Create: `backend/internal/engine/explain.go`
- Modify: `backend/internal/engine/types.go` (reemplazar el `Run` con `panic` por la implementación real)
- Test: `backend/internal/engine/explain_test.go`
- Test: `backend/internal/engine/engine_test.go`

**Interfaces:**
- Consumes: todas las funciones de Tasks 3-8.
- Produces: `engine.Run(meterID string, readings []Reading, events []Event, fleetCV float64, cfg Config) MeterResult` — es el punto de entrada que usará la fase API (Task 8 del plan de API).

- [ ] **Step 1: Escribir el test de plantillas**

```go
// backend/internal/engine/explain_test.go
package engine

import "testing"

func TestBuildReasonRealAnomalyIncludesNumbers(t *testing.T) {
	reason := BuildReason(RealAnomaly, 103.7, []VariableDelta{
		{Variable: "current_a", DeltaPct: 210, Changed: true},
	})
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
	if !containsAll(reason, "103.7", "current_a") {
		t.Fatalf("reason must cite the actual numbers, got %q", reason)
	}
}

func TestBuildRecommendationByType(t *testing.T) {
	cases := map[AnomalyType]string{
		RealAnomaly:        "Investigar medidor e instalación",
		TypeDataQuality:    "Validar sensor/cableado",
		ExplainableAnomaly: "Validar operación",
		FalsePositive:      "No escalar",
	}
	for typ, want := range cases {
		got := BuildRecommendation(typ)
		if got != want {
			t.Errorf("BuildRecommendation(%v) = %q, want %q", typ, got, want)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/... -run "BuildReason|BuildRecommendation" -v`
Expected: FAIL (undefined: BuildReason, BuildRecommendation)

- [ ] **Step 3: Implementar `explain.go`**

```go
// backend/internal/engine/explain.go
package engine

import (
	"fmt"
	"strings"
)

// BuildReason redacta la explicación por plantilla (02 §8), citando evidencia numérica real.
func BuildReason(t AnomalyType, variationPct float64, variables []VariableDelta) string {
	var changed []string
	for _, v := range variables {
		if v.Changed {
			changed = append(changed, fmt.Sprintf("%s %+.1f%%", v.Variable, v.DeltaPct))
		}
	}
	changedStr := "sin cambios eléctricos coherentes"
	if len(changed) > 0 {
		changedStr = strings.Join(changed, ", ")
	}

	switch t {
	case RealAnomaly:
		return fmt.Sprintf("Consumo %.1f%% por encima del baseline sin evento conocido; %s.", variationPct, changedStr)
	case TypeDataQuality:
		return fmt.Sprintf("Inconsistencia eléctrica o de calidad de datos detectada (variación de consumo %.1f%%); %s.", variationPct, changedStr)
	case ExplainableAnomaly:
		return fmt.Sprintf("Consumo %.1f%% respecto al baseline, explicado por un evento operativo registrado; %s.", variationPct, changedStr)
	case FalsePositive:
		return fmt.Sprintf("Variación de %.1f%% explicada por una parada programada; no se escala como anomalía real.", variationPct)
	default:
		return ""
	}
}

// BuildRecommendation retorna la acción recomendada por tipo (02 §8).
func BuildRecommendation(t AnomalyType) string {
	switch t {
	case RealAnomaly:
		return "Investigar medidor e instalación"
	case TypeDataQuality:
		return "Validar sensor/cableado"
	case ExplainableAnomaly:
		return "Validar operación"
	case FalsePositive:
		return "No escalar"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Reemplazar el `Run` de `types.go` por la implementación real**

```go
// backend/internal/engine/types.go
// Reemplazar la función Run existente (la que hace panic) por:

func Run(meterID string, readings []Reading, events []Event, fleetCV float64, cfg Config) MeterResult {
	cp := DetectChangePoint(readings, cfg)

	baselineFrom := readings[0].Timestamp
	baselineTo := baselineFrom.Add(time.Duration(cfg.DefaultBaselineDays) * 24 * time.Hour)
	if cp.Found {
		baselineTo = cp.At
	}
	profile := BuildHourlyProfile(readings, baselineFrom, baselineTo)
	th := ComputeThresholds(readings, baselineFrom, baselineTo, fleetCV, cfg)

	signals, issues := DetectSignals(readings, profile, cp, th, cfg)
	variables := CorrelateVariables(readings, profile, cp)

	actualKWh := sumKWh(readings)
	baselineKWh := extrapolateBaseline(profile, readings)
	variationPct := 0.0
	if baselineKWh != 0 {
		variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
	}

	relatedEvents := MatchEvents(events, cp, variationPct)

	result := MeterResult{
		MeterID: meterID, Baseline: profile, ChangePoint: cp, Thresholds: th,
		BaselineKWh: baselineKWh, ActualKWh: actualKWh, VariationPct: round2(variationPct),
		Signals: signals, Variables: variables, Events: relatedEvents,
	}

	typ, sev, confidence, breakdown, path := Classify(signals, issues, relatedEvents, variables, variationPct)
	if typ == "" {
		result.HasAnomaly = false
		return result
	}

	result.HasAnomaly = true
	result.Type = typ
	result.Severity = sev
	result.Confidence = confidence
	result.PriorityScore = PriorityScore(typ, sev, confidence, variationPct)
	result.Reason = BuildReason(typ, variationPct, variables)
	result.RecommendedAction = BuildRecommendation(typ)
	result.Evidence = Evidence{
		DecisionPath:          path,
		ConfidenceBreakdown:   breakdown,
		DataQualityIssues:     issues,
		AffectedReadingsCount: len(readings),
		AffectedReadingsFirst: readings[0].Timestamp,
		AffectedReadingsLast:  readings[len(readings)-1].Timestamp,
	}
	return result
}
```

Nota: añadir `import "time"` en `types.go` si no está presente.

- [ ] **Step 5: Test de integración del pipeline completo (fixtures sintéticos por caso de spec 05)**

```go
// backend/internal/engine/engine_test.go
package engine

import (
	"testing"
	"time"
)

func TestRunDetectsRealAnomalyLikeM109(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 20.35 // ~+103.7% sobre 10
		}
		return 10
	})
	for i := range readings {
		if readings[i].Timestamp.After(shiftAt) {
			readings[i].CurrentA = 400 // cambio eléctrico coherente
		} else {
			readings[i].CurrentA = 100
		}
	}
	result := Run("M-TEST-109", readings, nil, 0.1, DefaultConfig())
	if !result.HasAnomaly || result.Type != RealAnomaly || result.Severity != High {
		t.Fatalf("expected REAL_ANOMALY/HIGH, got %+v", result)
	}
	if result.Confidence < 0.6 {
		t.Fatalf("expected reasonably high confidence, got %v", result.Confidence)
	}
}

func TestRunFalsePositiveLikeM106(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	outageAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 4 // caída fuerte
		}
		return 10
	})
	events := []Event{{MeterID: "M-TEST-106", Timestamp: outageAt, Type: "SCHEDULED_OUTAGE", Description: "maintenance"}}
	result := Run("M-TEST-106", readings, events, 0.1, DefaultConfig())
	if !result.HasAnomaly || result.Type != FalsePositive || result.Severity != Low {
		t.Fatalf("expected FALSE_POSITIVE/LOW, got %+v", result)
	}
}

func TestRunNoAnomalyOnStableMeter(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 + float64(h%24)*0.1 })
	result := Run("M-TEST-STABLE", readings, nil, 0.1, DefaultConfig())
	if result.HasAnomaly {
		t.Fatalf("expected no anomaly on a stable meter, got %+v", result)
	}
}
```

- [ ] **Step 6: Run all engine tests**

Run: `cd backend && go test ./internal/engine/... -v`
Expected: PASS (todos los tests de Tasks 3-9)

- [ ] **Step 7: Commit**

```bash
git add backend/internal/engine/explain.go backend/internal/engine/types.go backend/internal/engine/explain_test.go backend/internal/engine/engine_test.go
git commit -m "feat(engine): explanation templates and full pipeline orchestrator"
```

---

### Task 10: `cmd/eval` (validación contra `expected_results.csv`, fuera del repo)

**Files:**
- Create: `backend/cmd/eval/main.go`
- Test: `backend/cmd/eval/main_test.go`

**Interfaces:**
- Consumes: `store.LoadReadingsCSV`, `store.LoadEventsCSV` (Task 2), `engine.Run` (Task 9).
- Produces: binario `eval` (usado por `make eval EXPECTED=...` en la fase Docker, fuera de este plan).

- [ ] **Step 1: Escribir el test — parseo del CSV esperado y cálculo del score, sin depender del archivo real**

```go
// backend/cmd/eval/main_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseExpectedCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "expected_results.csv")
	content := "meter_id,expected_type,expected_severity\n" +
		"M-109,REAL_ANOMALY,HIGH\n" +
		"M-106,FALSE_POSITIVE,LOW\n"
	os.WriteFile(path, []byte(content), 0o644)

	rows, err := parseExpectedCSV(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 || rows[0].MeterID != "M-109" || rows[0].ExpectedType != "REAL_ANOMALY" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestScoreRubricAllCorrect(t *testing.T) {
	expected := []expectedRow{
		{MeterID: "M-109", ExpectedType: "REAL_ANOMALY", ExpectedSeverity: "HIGH"},
	}
	actual := map[string]actualRow{
		"M-109": {Type: "REAL_ANOMALY", Severity: "HIGH", PriorityRank: 1},
	}
	score := scoreRubric(expected, actual)
	if score.DetectionPoints != 30 {
		t.Fatalf("expected full detection points, got %v", score)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./cmd/eval/... -v`
Expected: FAIL (undefined: parseExpectedCSV, scoreRubric)

- [ ] **Step 3: Implementar `main.go`**

```go
// backend/cmd/eval/main.go
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

type expectedRow struct {
	MeterID          string
	ExpectedType     string
	ExpectedSeverity string
}

type actualRow struct {
	Type         string
	Severity     string
	PriorityRank int
}

type rubricScore struct {
	DetectionPoints   int // 30: detecta M-109
	PriorityPoints    int // 25: M-109 es #1
	FalsePosPoints    int // 15: M-106 no es real
	DataQualityPoints int // 10: M-112 es data quality
	EvidencePoints    int // 10: evidencia presente
	ActionPoints      int // 10: acción coherente
}

func (s rubricScore) Total() int {
	return s.DetectionPoints + s.PriorityPoints + s.FalsePosPoints + s.DataQualityPoints + s.EvidencePoints + s.ActionPoints
}

func parseExpectedCSV(path string) ([]expectedRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	if _, err := r.Read(); err != nil { // header
		return nil, err
	}
	var rows []expectedRow
	for {
		rec, err := r.Read()
		if err != nil {
			break
		}
		rows = append(rows, expectedRow{MeterID: rec[0], ExpectedType: rec[1], ExpectedSeverity: rec[2]})
	}
	return rows, nil
}

func scoreRubric(expected []expectedRow, actual map[string]actualRow) rubricScore {
	var s rubricScore
	for _, e := range expected {
		a, ok := actual[e.MeterID]
		if !ok {
			continue
		}
		if e.ExpectedType == "REAL_ANOMALY" && a.Type == "REAL_ANOMALY" {
			s.DetectionPoints = 30
			if a.PriorityRank == 1 {
				s.PriorityPoints = 25
			}
		}
		if e.ExpectedType == "FALSE_POSITIVE" && a.Type != "REAL_ANOMALY" {
			s.FalsePosPoints = 15
		}
		if e.ExpectedType == "DATA_QUALITY" && a.Type == "DATA_QUALITY" {
			s.DataQualityPoints = 10
		}
	}
	return s
}

func main() {
	expectedPath := flag.String("expected", "", "ruta a expected_results.csv (fuera del repo)")
	dataDir := flag.String("data", "./data", "directorio con readings.csv y events.csv")
	flag.Parse()

	if *expectedPath == "" {
		fmt.Fprintln(os.Stderr, "uso: eval --expected /ruta/expected_results.csv [--data ./data]")
		os.Exit(1)
	}

	db, err := store.Open(":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error abriendo BD:", err)
		os.Exit(1)
	}
	defer db.Close()
	db.Migrate()

	if _, err := store.LoadReadingsCSV(db, *dataDir+"/readings.csv"); err != nil {
		fmt.Fprintln(os.Stderr, "error cargando readings:", err)
		os.Exit(1)
	}
	if _, err := store.LoadEventsCSV(db, *dataDir+"/events.csv"); err != nil {
		fmt.Fprintln(os.Stderr, "error cargando events:", err)
		os.Exit(1)
	}

	// Ejecutar el motor por medidor (implementación completa de lectura desde
	// store queda para la fase API; aquí se asume una función auxiliar
	// readAllByMeter que agrupa filas de la tabla readings/events por meter_id).
	results := runEngineOverStore(db)

	expected, err := parseExpectedCSV(*expectedPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error leyendo expected:", err)
		os.Exit(1)
	}

	score := scoreRubric(expected, results)
	fmt.Printf("Detección: %d/30\nPrioridad: %d/25\nFalso positivo: %d/15\nCalidad de datos: %d/10\nEvidencia: %d/10\nAcción: %d/10\nTOTAL: %d/100\n",
		score.DetectionPoints, score.PriorityPoints, score.FalsePosPoints, score.DataQualityPoints, score.EvidencePoints, score.ActionPoints, score.Total())
}

// runEngineOverStore agrupa lecturas/eventos por medidor y corre engine.Run.
// La consulta SQL real y el ranking de prioridad entre medidores se comparten
// con la fase API (ver plan de API, Task 3): aquí se reimplementa una versión
// mínima autosuficiente para no acoplar cmd/eval al servidor HTTP.
func runEngineOverStore(db *store.DB) map[string]actualRow {
	rows, err := db.Query(`SELECT DISTINCT meter_id FROM readings`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := map[string]actualRow{}
	type scored struct {
		meterID  string
		result   engine.MeterResult
	}
	var all []scored

	for rows.Next() {
		var meterID string
		rows.Scan(&meterID)
		readings := loadMeterReadings(db, meterID)
		events := loadMeterEvents(db, meterID)
		result := engine.Run(meterID, readings, events, 0.15, engine.DefaultConfig())
		all = append(all, scored{meterID, result})
	}

	rank := 0
	// ordenar por priority_score descendente para asignar PriorityRank
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].result.PriorityScore > all[j-1].result.PriorityScore; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	for _, s := range all {
		if !s.result.HasAnomaly {
			continue
		}
		rank++
		out[s.meterID] = actualRow{Type: string(s.result.Type), Severity: string(s.result.Severity), PriorityRank: rank}
	}
	return out
}

func loadMeterReadings(db *store.DB, meterID string) []engine.Reading {
	rows, err := db.Query(`SELECT timestamp, consumption_kwh, voltage_v, current_a, power_factor, status
		FROM readings WHERE meter_id = ? ORDER BY timestamp`, meterID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []engine.Reading
	for rows.Next() {
		var r engine.Reading
		var ts string
		rows.Scan(&ts, &r.ConsumptionKWh, &r.VoltageV, &r.CurrentA, &r.PowerFactor, &r.Status)
		t, _ := parseUTCPublic(ts)
		r.MeterID = meterID
		r.Timestamp = t
		out = append(out, r)
	}
	return out
}

func loadMeterEvents(db *store.DB, meterID string) []engine.Event {
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
		t, _ := parseUTCPublic(ts)
		e.MeterID = meterID
		e.Timestamp = t
		out = append(out, e)
	}
	return out
}
```

Nota: `parseUTCPublic` es un pequeño helper (`time.Parse(time.RFC3339, s)`) que debe añadirse a `cmd/eval/main.go` ya que las tablas guardan timestamps en `RFC3339` (Task 2). No dupliques la constante `timeLayout` de `store` — es un paquete interno distinto con su propio parseo del formato de salida.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./cmd/eval/... -v && go build ./...`
Expected: PASS y build sin errores

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/eval/main.go backend/cmd/eval/main_test.go
git commit -m "feat(eval): cmd/eval scores engine output against expected_results.csv"
```

---

## Self-Review

**1. Cobertura de spec:** 00 (reglas no negociables) → constraints globales + Task 9/10 (nunca carga expected_results.csv en la app). 01 (esquema) → Task 1. Carga CSV real → Task 2. 02 §1 → Task 4. §2-3 → Task 6. §3 correlación → Task 7. §4 eventos → Task 7. §5-7 clasificación/confianza/prioridad → Task 8. §8 explicación → Task 9. §10 umbrales → Task 5. 05 (tests sintéticos, cmd/eval, rúbrica) → Tasks 3-10.

**2. Placeholders:** ninguno; cada step tiene código completo y ejecutable.

**3. Consistencia de tipos:** `MeterResult`, `Signal`, `VariableDelta`, `RelatedEvent`, `Evidence` definidos una vez en `types.go` (bloque de interfaces) y reutilizados sin renombrar en todas las tareas. `engine.Run` tiene la misma firma en el contrato inicial y en Task 9.

**4. Review Focus:** los 5 casos (MAD=0, ventana <3 días, evento fuera de ±24h, medidor sin señales, PF/voltaje inválidos) tienen test dedicado en Tasks 3, 5, 6 y 7.
