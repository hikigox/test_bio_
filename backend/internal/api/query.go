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
