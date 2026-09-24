package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"energy-management/internal/store"
	"github.com/go-chi/chi/v5"
)

var validAnomalyStatuses = map[string]bool{
	"OPEN": true, "INVESTIGATING": true, "RESOLVED": true, "DISMISSED": true,
}

// anomalyListItem is one row of GET /anomalies (spec 03: meter_id/type/
// severity/confidence/status filters, from/to overlap filter, ordered by
// priority). No explicit JSON sample is given for the list (only for
// /anomalies/:id), so the shape mirrors the vocabulary the detail contract
// and /meters' embedded anomaly summary already use.
type anomalyListItem struct {
	ID            int64   `json:"id"`
	AnalysisID    int64   `json:"analysis_id"`
	MeterID       string  `json:"meter_id"`
	Type          string  `json:"type"`
	Severity      string  `json:"severity"`
	Confidence    float64 `json:"confidence"`
	Status        string  `json:"status"`
	PriorityRank  int     `json:"priority_rank"`
	PeriodFrom    string  `json:"period_from"`
	PeriodTo      string  `json:"period_to"`
	ChangePointAt *string `json:"change_point_at"`
	ActiveFrom    string  `json:"active_from"`
	ActiveTo      string  `json:"active_to"`
}

func registerAnomalyRoutes(r chi.Router, s *Server) {
	r.Get("/anomalies", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		from, to, hasRange := parseRangeParams(req)
		ranks := priorityRanks(s.DB)

		query := `SELECT a.id, a.analysis_id, a.meter_id, a.type, a.severity, a.confidence, a.status,
			a.period_from, a.period_to, a.change_point_at
			FROM anomalies a JOIN analyses an ON an.id = a.analysis_id
			WHERE an.status = 'COMPLETED'`
		var args []interface{}

		// spec 03: "Vigentes por defecto" — sin analysis_id explícito, solo la
		// anomalía del último análisis COMPLETED que incluyó a cada medidor
		// (mismo criterio que priorityRanks/CurrentAnomaly). analysis_id=
		// permite inspeccionar un análisis puntual (p.ej. histórico).
		if analysisIDStr := q.Get("analysis_id"); analysisIDStr != "" {
			analysisID, err := strconv.ParseInt(analysisIDStr, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "analysis_id inválido")
				return
			}
			query += ` AND a.analysis_id = ?`
			args = append(args, analysisID)
		} else {
			query += ` AND an.id = (
				SELECT MAX(an2.id) FROM analyses an2
				JOIN meter_baselines mb2 ON mb2.analysis_id = an2.id
				WHERE mb2.meter_id = a.meter_id AND an2.status = 'COMPLETED'
			)`
		}
		if meterID := q.Get("meter_id"); meterID != "" {
			query += ` AND a.meter_id = ?`
			args = append(args, meterID)
		}
		if typ := q.Get("type"); typ != "" {
			query += ` AND a.type = ?`
			args = append(args, typ)
		}
		if severity := q.Get("severity"); severity != "" {
			query += ` AND a.severity = ?`
			args = append(args, severity)
		}
		if status := q.Get("status"); status != "" {
			query += ` AND a.status = ?`
			args = append(args, status)
		}
		query += ` ORDER BY a.priority_score DESC`

		rows, err := s.DB.Query(query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
			return
		}
		defer rows.Close()

		out := []anomalyListItem{}
		for rows.Next() {
			var i anomalyListItem
			var periodFrom, periodTo string
			var changePointAt sql.NullString
			if err := rows.Scan(&i.ID, &i.AnalysisID, &i.MeterID, &i.Type, &i.Severity, &i.Confidence, &i.Status,
				&periodFrom, &periodTo, &changePointAt); err != nil {
				writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
				return
			}

			a := AnomalyRow{ID: i.ID}
			a.PeriodFrom, _ = time.Parse(time.RFC3339, periodFrom)
			a.PeriodTo, _ = time.Parse(time.RFC3339, periodTo)
			if changePointAt.Valid {
				cp, _ := time.Parse(time.RFC3339, changePointAt.String)
				a.ChangePointAt = &cp
				v := changePointAt.String
				i.ChangePointAt = &v
			}
			activeFrom, activeTo := ActiveWindow(a)
			if hasRange && !Overlaps(activeFrom, activeTo, from, to) {
				continue
			}
			i.PeriodFrom, i.PeriodTo = periodFrom, periodTo
			i.PriorityRank = ranks[i.ID]
			i.ActiveFrom, i.ActiveTo = activeFrom.Format(time.RFC3339), activeTo.Format(time.RFC3339)
			out = append(out, i)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
	})

	r.Get("/anomalies/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id inválido")
			return
		}
		ranks := priorityRanks(s.DB)
		detail, err := loadAnomalyDetail(s.DB, id, ranks)
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "anomalía no encontrada")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo anomalía")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})

	r.Patch("/anomalies/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id inválido")
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "cuerpo inválido")
			return
		}
		if !validAnomalyStatuses[body.Status] {
			writeError(w, http.StatusBadRequest, "status inválido")
			return
		}
		res, err := s.DB.Exec(`UPDATE anomalies SET status = ?, updated_at = ? WHERE id = ?`,
			body.Status, time.Now().UTC().Format(time.RFC3339), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error actualizando anomalía")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, "anomalía no encontrada")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
	})
}

type anomalySignal struct {
	Signal    string  `json:"signal"`
	Observed  float64 `json:"observed"`
	Threshold float64 `json:"threshold"`
	Detail    string  `json:"detail"`
}

type anomalyVariable struct {
	Variable string  `json:"variable"`
	Baseline float64 `json:"baseline"`
	Actual   float64 `json:"actual"`
	DeltaPct float64 `json:"delta_pct"`
	Changed  bool    `json:"changed"`
}

type anomalyEvent struct {
	ID          int64   `json:"id"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Relation    string  `json:"relation"`
	OffsetHours float64 `json:"offset_hours"`
}

// anomalyDetail is GET /anomalies/:id's response, matching spec 03's
// "Contratos clave" sample verbatim (id, analysis_id, meter_id, type,
// severity, confidence, confidence_label, priority_rank, priority_score,
// status, detected_at, period_from/to, change_point_at, active_from/to,
// duration_hours, baseline/actual/variation, reason, recommended_action,
// explanation_source, signals, variables, events, evidence).
type anomalyDetail struct {
	ID                int64             `json:"id"`
	AnalysisID        int64             `json:"analysis_id"`
	MeterID           string            `json:"meter_id"`
	Type              string            `json:"type"`
	Severity          string            `json:"severity"`
	Confidence        float64           `json:"confidence"`
	ConfidenceLabel   string            `json:"confidence_label"`
	PriorityRank      int               `json:"priority_rank"`
	PriorityScore     float64           `json:"priority_score"`
	Status            string            `json:"status"`
	DetectedAt        string            `json:"detected_at"`
	PeriodFrom        string            `json:"period_from"`
	PeriodTo          string            `json:"period_to"`
	ChangePointAt     *string           `json:"change_point_at"`
	ActiveFrom        string            `json:"active_from"`
	ActiveTo          string            `json:"active_to"`
	DurationHours     float64           `json:"duration_hours"`
	BaselineKWh       float64           `json:"baseline_kwh"`
	ActualKWh         float64           `json:"actual_kwh"`
	VariationPct      float64           `json:"variation_pct"`
	Reason            string            `json:"reason"`
	RecommendedAction string            `json:"recommended_action"`
	ExplanationSource string            `json:"explanation_source"`
	Signals           []anomalySignal   `json:"signals"`
	Variables         []anomalyVariable `json:"variables"`
	Events            []anomalyEvent    `json:"events"`
	Evidence          json.RawMessage   `json:"evidence"`
}

// loadAnomalyDetail reads a single anomaly plus its signals/variables/events
// (each a separate sequential query — store.DB is pinned to a single pooled
// connection, so nested queries must never run while another cursor from the
// same *store.DB is still open, per meters.go's established pattern).
func loadAnomalyDetail(db *store.DB, id int64, ranks map[int64]int) (*anomalyDetail, error) {
	var d anomalyDetail
	var periodFrom, periodTo string
	var detectedAt, confidenceLabel, reason, recommendedAction, explanationSource sql.NullString
	var changePointAt sql.NullString
	var evidenceStr sql.NullString
	err := db.QueryRow(`SELECT id, analysis_id, meter_id, type, severity, confidence, confidence_label,
		priority_score, status, detected_at, period_from, period_to, change_point_at,
		baseline_kwh, actual_kwh, variation_pct, reason, recommended_action, explanation_source, evidence_json
		FROM anomalies WHERE id = ?`, id).Scan(
		&d.ID, &d.AnalysisID, &d.MeterID, &d.Type, &d.Severity, &d.Confidence, &confidenceLabel,
		&d.PriorityScore, &d.Status, &detectedAt, &periodFrom, &periodTo, &changePointAt,
		&d.BaselineKWh, &d.ActualKWh, &d.VariationPct, &reason, &recommendedAction, &explanationSource, &evidenceStr)
	if err != nil {
		return nil, err
	}
	d.DetectedAt = detectedAt.String
	d.ConfidenceLabel = confidenceLabel.String
	d.Reason = reason.String
	d.RecommendedAction = recommendedAction.String
	d.ExplanationSource = explanationSource.String
	d.PeriodFrom, d.PeriodTo = periodFrom, periodTo
	d.PriorityRank = ranks[d.ID]

	a := AnomalyRow{ID: d.ID}
	a.PeriodFrom, _ = time.Parse(time.RFC3339, periodFrom)
	a.PeriodTo, _ = time.Parse(time.RFC3339, periodTo)
	if changePointAt.Valid {
		v := changePointAt.String
		d.ChangePointAt = &v
		cp, _ := time.Parse(time.RFC3339, v)
		a.ChangePointAt = &cp
	}
	activeFrom, activeTo := ActiveWindow(a)
	d.ActiveFrom, d.ActiveTo = activeFrom.Format(time.RFC3339), activeTo.Format(time.RFC3339)
	d.DurationHours = activeTo.Sub(activeFrom).Hours()

	if evidenceStr.Valid && evidenceStr.String != "" {
		d.Evidence = json.RawMessage(evidenceStr.String)
	} else {
		d.Evidence = json.RawMessage(`{}`)
	}

	sigRows, err := db.Query(`SELECT signal, observed, threshold, detail FROM anomaly_signals WHERE anomaly_id = ?`, id)
	if err != nil {
		return nil, err
	}
	d.Signals = []anomalySignal{}
	for sigRows.Next() {
		var s anomalySignal
		if err := sigRows.Scan(&s.Signal, &s.Observed, &s.Threshold, &s.Detail); err != nil {
			sigRows.Close()
			return nil, err
		}
		d.Signals = append(d.Signals, s)
	}
	sigRows.Close()

	varRows, err := db.Query(`SELECT variable, baseline, actual, delta_pct, changed FROM anomaly_variables WHERE anomaly_id = ?`, id)
	if err != nil {
		return nil, err
	}
	d.Variables = []anomalyVariable{}
	for varRows.Next() {
		var v anomalyVariable
		var changed int
		if err := varRows.Scan(&v.Variable, &v.Baseline, &v.Actual, &v.DeltaPct, &changed); err != nil {
			varRows.Close()
			return nil, err
		}
		v.Changed = changed != 0
		d.Variables = append(d.Variables, v)
	}
	varRows.Close()

	evRows, err := db.Query(`SELECT e.id, e.type, e.description, ae.relation, ae.offset_hours
		FROM anomaly_events ae JOIN events e ON e.id = ae.event_id WHERE ae.anomaly_id = ?`, id)
	if err != nil {
		return nil, err
	}
	d.Events = []anomalyEvent{}
	for evRows.Next() {
		var e anomalyEvent
		if err := evRows.Scan(&e.ID, &e.Type, &e.Description, &e.Relation, &e.OffsetHours); err != nil {
			evRows.Close()
			return nil, err
		}
		d.Events = append(d.Events, e)
	}
	evRows.Close()

	return &d, nil
}
