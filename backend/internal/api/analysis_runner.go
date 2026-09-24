package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
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

func (e *ConflictError) Error() string {
	return fmt.Sprintf("ya hay un análisis en curso (id=%d)", e.RunningID)
}

// startAnalysisMu guards the "check for a RUNNING analysis, then insert a new
// RUNNING row" critical section of startAnalysisRow. Without it, two
// concurrent StartAnalysis calls could both see no RUNNING analysis (via the
// SELECT) before either commits its INSERT, both proceed, and violate spec
// 03's "solo un análisis RUNNING a la vez" invariant. store.Open only pins
// SQLite to a single connection for the ":memory:" DSN used in tests — a
// file-backed deployment can and does hand out multiple connections/goroutines
// concurrently, so the race is real there, not just theoretical.
//
// A process-local sync.Mutex is enough (rather than a SQLite-level
// transaction such as BEGIN IMMEDIATE, e.g. via the driver's _txlock DSN
// param) because this invariant only needs to hold within a single API
// server process: nothing else writes to the `analyses` table, and this is
// not a multi-process/distributed deployment. It also keeps the fix local to
// this file instead of changing store.Open's DSN handling, which is shared
// by callers outside this task's scope.
var startAnalysisMu sync.Mutex

// startAnalysisRow performs the atomic "reject if a RUNNING analysis exists,
// otherwise insert a new RUNNING row" step of StartAnalysis. Extracted so it
// can be exercised directly under concurrent goroutines in tests, and so the
// mutex only covers this critical section — not the (slower, unbounded)
// pipeline that runAnalysis drives afterward.
func startAnalysisRow(db *store.DB, scope AnalysisScope, triggeredBy int64) (int64, error) {
	if scope.BaselineFrom != nil && scope.BaselineTo != nil {
		if scope.BaselineTo.Sub(*scope.BaselineFrom) < 3*24*time.Hour {
			return 0, &ValidationError{Message: "baseline_from/baseline_to debe cubrir al menos 3 días"}
		}
		if scope.BaselineFrom.Before(scope.From) || scope.BaselineTo.After(scope.To) {
			return 0, &ValidationError{Message: "baseline_from/baseline_to debe estar dentro de from/to"}
		}
	}

	startAnalysisMu.Lock()
	defer startAnalysisMu.Unlock()

	var runningID sql.NullInt64
	db.QueryRow(`SELECT id FROM analyses WHERE status = 'RUNNING' LIMIT 1`).Scan(&runningID)
	if runningID.Valid {
		return 0, &ConflictError{RunningID: runningID.Int64}
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
	return analysisID, nil
}

// StartAnalysis valida el scope, crea la fila `analyses` y lanza el pipeline
// en una goroutine. Devuelve el id de inmediato (202 en el handler HTTP, Task 5).
func StartAnalysis(db *store.DB, scope AnalysisScope, triggeredBy int64) (int64, error) {
	analysisID, err := startAnalysisRow(db, scope, triggeredBy)
	if err != nil {
		return 0, err
	}

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
	"REAL_ANOMALY:HIGH":          "CRITICAL",
	"REAL_ANOMALY:MEDIUM":        "ALERT",
	"REAL_ANOMALY:LOW":           "ALERT",
	"EXPLAINABLE_ANOMALY:HIGH":   "ALERT",
	"EXPLAINABLE_ANOMALY:MEDIUM": "ALERT",
	"EXPLAINABLE_ANOMALY:LOW":    "ALERT",
	"DATA_QUALITY:HIGH":          "ALERT",
	"DATA_QUALITY:MEDIUM":        "ALERT",
	"DATA_QUALITY:LOW":           "ALERT",
	"FALSE_POSITIVE:HIGH":        "OK",
	"FALSE_POSITIVE:MEDIUM":      "OK",
	"FALSE_POSITIVE:LOW":         "OK",
}

func insertAnomaly(tx *sql.Tx, analysisID int64, r engine.MeterResult) error {
	evidenceJSON, _ := json.Marshal(map[string]interface{}{
		"decision_path":        r.Evidence.DecisionPath,
		"confidence_breakdown": r.Evidence.ConfidenceBreakdown,
		"data_quality_issues":  r.Evidence.DataQualityIssues,
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
