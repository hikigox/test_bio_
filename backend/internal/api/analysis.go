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

type analysisStatusResponse struct {
	ID       int64   `json:"id"`
	Status   string  `json:"status"`
	Stage    string  `json:"stage"`
	Progress float64 `json:"progress"`
	Summary  struct {
		Anomalies int     `json:"anomalies"`
		Priority  int     `json:"priority"`
		AvgConf   float64 `json:"avg_confidence"`
		Message   string  `json:"message"`
	} `json:"summary"`
}

func loadAnalysisStatus(db *store.DB, id int64) (*analysisStatusResponse, error) {
	var resp analysisStatusResponse
	row := db.QueryRow(`SELECT id, status, stage, progress, anomalies_count, high_priority_count, avg_confidence, summary_message
		FROM analyses WHERE id = ?`, id)
	var anomaliesCount, highPriority sql.NullInt64
	var avgConf sql.NullFloat64
	var summary sql.NullString
	var stage sql.NullString
	var progress sql.NullFloat64
	if err := row.Scan(&resp.ID, &resp.Status, &stage, &progress, &anomaliesCount, &highPriority, &avgConf, &summary); err != nil {
		return nil, err
	}
	resp.Stage = stage.String
	resp.Progress = progress.Float64
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
	out := []analysisStatusResponse{}
	for rows.Next() {
		var resp analysisStatusResponse
		var anomaliesCount, highPriority sql.NullInt64
		var avgConf sql.NullFloat64
		var summary sql.NullString
		var stage sql.NullString
		var progress sql.NullFloat64
		if err := rows.Scan(&resp.ID, &resp.Status, &stage, &progress, &anomaliesCount, &highPriority, &avgConf, &summary); err != nil {
			return nil, err
		}
		resp.Stage = stage.String
		resp.Progress = progress.Float64
		resp.Summary.Anomalies = int(anomaliesCount.Int64)
		resp.Summary.Priority = int(highPriority.Int64)
		resp.Summary.AvgConf = avgConf.Float64
		resp.Summary.Message = summary.String
		out = append(out, resp)
	}
	return out, nil
}
