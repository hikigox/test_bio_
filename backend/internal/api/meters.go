package api

import (
	"database/sql"
	"net/http"
	"time"

	"energy-management/internal/store"
	"github.com/go-chi/chi/v5"
)

type meterListItem struct {
	MeterID        string          `json:"meter_id"`
	Name           string          `json:"name"`
	ConsumptionKWh *float64        `json:"consumption_kwh"`
	BaselineKWh    *float64        `json:"baseline_kwh"`
	VariationPct   *float64        `json:"variation_pct"`
	Status         string          `json:"status"`
	AnalysisID     *int64          `json:"analysis_id"`
	Anomaly        *anomalySummary `json:"anomaly"`
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

// meterAnalysisFields fetches the current anomaly (if any) for meterID and
// fills the analysis-derived fields of item: consumption/baseline/variation,
// analysis_id, and, if a range is given, the in_range flag on the anomaly
// summary. A never-analyzed meter (CurrentAnomaly returns nil) leaves item's
// pointer fields nil, which json.Marshal renders as `null` — no anomaly
// summary and no baseline/variation, exactly as spec 03 requires.
func meterAnalysisFields(s *Server, meterID string, item *meterListItem, ranks map[int64]int, from, to time.Time, hasRange bool) {
	anomaly, err := CurrentAnomaly(s.DB, meterID)
	if err != nil || anomaly == nil {
		return
	}
	item.AnalysisID = &anomaly.AnalysisID

	var baselineKWh, actualKWh, variationPct float64
	if !hasRange {
		// No caller-supplied date filter: report the analysis's own stored
		// figures for its full period, rather than recomputing from
		// readings (which would silently zero out if the raw readings for
		// that period are no longer retained).
		baselineKWh, actualKWh, variationPct = anomaly.BaselineKWh, anomaly.ActualKWh, anomaly.VariationPct
	} else {
		var err error
		baselineKWh, actualKWh, err = BaselineForRange(s.DB, meterID, from, to)
		if err != nil {
			return
		}
		if baselineKWh != 0 {
			variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
		}
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

func registerMeterRoutes(r chi.Router, s *Server) {
	r.Get("/meters", func(w http.ResponseWriter, req *http.Request) {
		from, to, hasRange := parseRangeParams(req)
		ranks := priorityRanks(s.DB)

		rows, err := s.DB.Query(`SELECT meter_id, name, status FROM meters`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo medidores")
			return
		}
		// Buffer the meter rows fully before running any nested query. The
		// in-memory test DB is pinned to a single pooled connection
		// (store.Open), so issuing a second Query while this cursor is still
		// open would block forever waiting for a connection that will never
		// be granted (classic self-deadlock).
		type meterRow struct {
			meterID, status string
			name            sql.NullString
		}
		var meterRows []meterRow
		for rows.Next() {
			var mr meterRow
			if err := rows.Scan(&mr.meterID, &mr.name, &mr.status); err != nil {
				rows.Close()
				writeError(w, http.StatusInternalServerError, "error leyendo medidores")
				return
			}
			meterRows = append(meterRows, mr)
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo medidores")
			return
		}

		items := []meterListItem{}
		for _, mr := range meterRows {
			item := meterListItem{MeterID: mr.meterID, Name: mr.name.String, Status: mr.status}
			meterAnalysisFields(s, mr.meterID, &item, ranks, from, to, hasRange)
			items = append(items, item)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
	})

	r.Get("/meters/{meterId}", func(w http.ResponseWriter, req *http.Request) {
		meterID := chi.URLParam(req, "meterId")
		var status string
		var name sql.NullString
		err := s.DB.QueryRow(`SELECT name, status FROM meters WHERE meter_id = ?`, meterID).Scan(&name, &status)
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "medidor no encontrado")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error leyendo medidor")
			return
		}
		item := meterListItem{MeterID: meterID, Name: name.String, Status: status}
		ranks := priorityRanks(s.DB)
		var from, to time.Time
		meterAnalysisFields(s, meterID, &item, ranks, from, to, false)
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
			Timestamp      string  `json:"timestamp"`
			ConsumptionKWh float64 `json:"consumption_kwh"`
			VoltageV       float64 `json:"voltage_v"`
			CurrentA       float64 `json:"current_a"`
			PowerFactor    float64 `json:"power_factor"`
		}
		out := []point{}
		for rows.Next() {
			var p point
			if err := rows.Scan(&p.Timestamp, &p.ConsumptionKWh, &p.VoltageV, &p.CurrentA, &p.PowerFactor); err != nil {
				writeError(w, http.StatusInternalServerError, "error leyendo lecturas")
				return
			}
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
			Timestamp   string `json:"timestamp"`
			Type        string `json:"type"`
			Description string `json:"description"`
		}
		out := []ev{}
		for rows.Next() {
			var e ev
			if err := rows.Scan(&e.Timestamp, &e.Type, &e.Description); err != nil {
				writeError(w, http.StatusInternalServerError, "error leyendo eventos")
				return
			}
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
			ID         int64   `json:"id"`
			Type       string  `json:"type"`
			Severity   string  `json:"severity"`
			Confidence float64 `json:"confidence"`
			Status     string  `json:"status"`
		}
		out := []item{}
		for rows.Next() {
			var i item
			if err := rows.Scan(&i.ID, &i.Type, &i.Severity, &i.Confidence, &i.Status); err != nil {
				writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
				return
			}
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
// ordenadas por priority_score descendente (spec 03: "no se guarda, se
// calcula al consultar"). "Vigente" replica la semántica de currentAnalysisID
// por medidor: solo la anomalía del último análisis COMPLETED que incluyó a
// cada medidor cuenta hacia su rank. Task 7 reutiliza esta función por nombre
// exacto para el dashboard.
func priorityRanks(db *store.DB) map[int64]int {
	rows, err := db.Query(`SELECT a.id FROM anomalies a
		JOIN analyses an ON an.id = a.analysis_id
		WHERE an.status = 'COMPLETED' AND an.id = (
			SELECT MAX(an2.id) FROM analyses an2
			JOIN meter_baselines mb2 ON mb2.analysis_id = an2.id
			WHERE mb2.meter_id = a.meter_id AND an2.status = 'COMPLETED'
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
		if err := rows.Scan(&id); err != nil {
			return map[int64]int{}
		}
		rank++
		ranks[id] = rank
	}
	return ranks
}
