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

func (e *ConflictError) Error() string {
	return fmt.Sprintf("ya hay un análisis en curso (id=%d)", e.RunningID)
}

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

// insertMeterBaseline e insertAnomaly: implementación completa en Task 5.
// Aquí son stubs mínimos (return nil) solo para que el paquete compile y
// runAnalysis pueda llegar a COMPLETED en este Task; Task 5 los reemplaza con
// la persistencia real en meter_baselines/anomalies.
func insertMeterBaseline(tx *sql.Tx, analysisID int64, r engine.MeterResult) error { return nil }
func insertAnomaly(tx *sql.Tx, analysisID int64, r engine.MeterResult) error       { return nil }
