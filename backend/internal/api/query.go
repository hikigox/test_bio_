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

// currentAnalysisID determina el análisis vigente para meterID: el último
// análisis COMPLETED que lo incluyó, es decir que tiene una fila en
// meter_baselines para ese medidor (spec 03 línea 38: "el último análisis
// COMPLETED que lo incluyó", no el último que le generó una anomalía; spec 01:
// todo medidor incluido en un análisis recibe fila en meter_baselines, tenga
// o no anomalía). Retorna found=false si el medidor nunca fue analizado.
func currentAnalysisID(db *store.DB, meterID string) (analysisID int64, found bool, err error) {
	row := db.QueryRow(`
		SELECT mb.analysis_id FROM meter_baselines mb
		JOIN analyses an ON an.id = mb.analysis_id
		WHERE mb.meter_id = ? AND an.status = 'COMPLETED'
		ORDER BY an.finished_at DESC, an.id DESC
		LIMIT 1`, meterID)
	if err := row.Scan(&analysisID); err == sql.ErrNoRows {
		return 0, false, nil
	} else if err != nil {
		return 0, false, err
	}
	return analysisID, true, nil
}

// CurrentAnomaly retorna la anomalía del análisis vigente (spec 03 "vigencia":
// el último análisis COMPLETED que incluyó a meterID), o nil si el medidor
// nunca fue analizado o si su análisis vigente no le generó anomalía (medidor
// actualmente normal, incluso si tuvo una anomalía en un análisis anterior).
func CurrentAnomaly(db *store.DB, meterID string) (*AnomalyRow, error) {
	analysisID, found, err := currentAnalysisID(db, meterID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	row := db.QueryRow(`
		SELECT a.id, a.analysis_id, a.meter_id, a.type, a.severity, a.confidence, a.priority_score,
		       a.baseline_kwh, a.actual_kwh, a.variation_pct, a.period_from, a.period_to, a.change_point_at, a.status
		FROM anomalies a
		WHERE a.meter_id = ? AND a.analysis_id = ?`, meterID, analysisID)

	var a AnomalyRow
	var periodFrom, periodTo string
	var changePointAt sql.NullString
	err = row.Scan(&a.ID, &a.AnalysisID, &a.MeterID, &a.Type, &a.Severity, &a.Confidence, &a.PriorityScore,
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
	analysisID, found, err := currentAnalysisID(db, meterID)
	if err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, 0, nil
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
