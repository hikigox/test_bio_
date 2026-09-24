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

// dataRangeOrDefault usa from/to del body si vienen, o el rango completo de
// datos disponibles (spec 03 "sin cuerpo" -> analiza toda la flota).
func dataRangeOrDefault(db *store.DB, from, to string) (time.Time, time.Time) {
	if from != "" && to != "" {
		f, _ := time.Parse(time.RFC3339, from)
		t, _ := time.Parse(time.RFC3339, to)
		return f, t
	}
	return dataRangeFromDB(db)
}

func dataRangeFromDB(db *store.DB) (time.Time, time.Time) {
	var minTS, maxTS string
	db.QueryRow(`SELECT MIN(timestamp), MAX(timestamp) FROM readings`).Scan(&minTS, &maxTS)
	from, _ := time.Parse(time.RFC3339, minTS)
	to, _ := time.Parse(time.RFC3339, maxTS)
	return from, to
}

// analysisScopeResponse is the `scope` sub-object of spec 03's
// GET /ai/analysis/:id sample. Every field is already a column on `analyses`,
// written by startAnalysisRow.
type analysisScopeResponse struct {
	MeterIDs     []string `json:"meter_ids"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	BaselineFrom *string  `json:"baseline_from"`
	BaselineTo   *string  `json:"baseline_to"`
}

// analysisStatusResponse matches spec 03's "Contratos clave" sample for
// GET /ai/analysis/:id. `stage_log` is the one documented field deliberately
// left out: there is no per-stage timing recorded anywhere yet, so it is
// deferred rather than faked.
type analysisStatusResponse struct {
	ID               int64                 `json:"id"`
	Status           string                `json:"status"`
	Stage            string                `json:"stage"`
	Progress         float64               `json:"progress"`
	Scope            analysisScopeResponse `json:"scope"`
	StartedAt        string                `json:"started_at"`
	FinishedAt       *string               `json:"finished_at"`
	DurationMs       *int64                `json:"duration_ms"`
	EngineVersion    string                `json:"engine_version"`
	ReadingsAnalyzed int                   `json:"readings_analyzed"`
	MetersAnalyzed   int                   `json:"meters_analyzed"`
	Summary          struct {
		Anomalies int     `json:"anomalies"`
		Priority  int     `json:"priority"`
		AvgConf   float64 `json:"avg_confidence"`
		Message   string  `json:"message"`
	} `json:"summary"`
}

const analysisSelectColumns = `id, status, stage, progress, anomalies_count, high_priority_count,
	avg_confidence, summary_message, scope_meter_ids_json, data_from, data_to,
	baseline_from, baseline_to, started_at, finished_at, duration_ms, engine_version,
	readings_analyzed, meters_analyzed`

// scanAnalysisRow decodes one `analyses` row (selected with
// analysisSelectColumns) into the response shape. Shared by the single-status
// and history readers so the two can't drift apart.
func scanAnalysisRow(scan func(dest ...interface{}) error) (*analysisStatusResponse, error) {
	var resp analysisStatusResponse
	var anomaliesCount, highPriority sql.NullInt64
	var readingsAnalyzed, metersAnalyzed, durationMs sql.NullInt64
	var avgConf, progress sql.NullFloat64
	var summary, stage sql.NullString
	var meterIDsJSON, dataFrom, dataTo, baselineFrom, baselineTo sql.NullString
	var startedAt, finishedAt, engineVersion sql.NullString
	if err := scan(&resp.ID, &resp.Status, &stage, &progress, &anomaliesCount, &highPriority,
		&avgConf, &summary, &meterIDsJSON, &dataFrom, &dataTo,
		&baselineFrom, &baselineTo, &startedAt, &finishedAt, &durationMs, &engineVersion,
		&readingsAnalyzed, &metersAnalyzed); err != nil {
		return nil, err
	}
	resp.Stage = stage.String
	resp.Progress = progress.Float64
	resp.Summary.Anomalies = int(anomaliesCount.Int64)
	resp.Summary.Priority = int(highPriority.Int64)
	resp.Summary.AvgConf = avgConf.Float64
	resp.Summary.Message = summary.String

	resp.Scope.From, resp.Scope.To = dataFrom.String, dataTo.String
	if meterIDsJSON.Valid && meterIDsJSON.String != "" {
		// A nil/"null" scope means "toda la flota" (spec 03), which the
		// contract renders as meter_ids: null — exactly what unmarshalling
		// "null" into the nil slice leaves behind.
		json.Unmarshal([]byte(meterIDsJSON.String), &resp.Scope.MeterIDs)
	}
	if baselineFrom.Valid {
		v := baselineFrom.String
		resp.Scope.BaselineFrom = &v
	}
	if baselineTo.Valid {
		v := baselineTo.String
		resp.Scope.BaselineTo = &v
	}
	resp.StartedAt = startedAt.String
	if finishedAt.Valid && finishedAt.String != "" {
		v := finishedAt.String
		resp.FinishedAt = &v
	}
	if durationMs.Valid {
		v := durationMs.Int64
		resp.DurationMs = &v
	}
	resp.EngineVersion = engineVersion.String
	resp.ReadingsAnalyzed = int(readingsAnalyzed.Int64)
	resp.MetersAnalyzed = int(metersAnalyzed.Int64)
	return &resp, nil
}

func loadAnalysisStatus(db *store.DB, id int64) (*analysisStatusResponse, error) {
	row := db.QueryRow(`SELECT `+analysisSelectColumns+` FROM analyses WHERE id = ?`, id)
	return scanAnalysisRow(row.Scan)
}

func loadAnalysisHistory(db *store.DB, limit int) ([]analysisStatusResponse, error) {
	rows, err := db.Query(`SELECT `+analysisSelectColumns+` FROM analyses ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []analysisStatusResponse{}
	for rows.Next() {
		resp, err := scanAnalysisRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *resp)
	}
	return out, nil
}
