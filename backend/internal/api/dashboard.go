package api

import (
	"database/sql"
	"net/http"
	"time"

	"energy-management/internal/store"
	"github.com/go-chi/chi/v5"
)

type dashboardByMeterItem struct {
	MeterID        string  `json:"meter_id"`
	ConsumptionKWh float64 `json:"consumption_kwh"`
	SharePct       float64 `json:"share_pct"`
	Status         string  `json:"status"`
	Severity       string  `json:"severity"`
}

type lastAnalysisSummary struct {
	At     string `json:"at"`
	Status string `json:"status"`
}

// consumptionForRange suma consumption_kwh de las lecturas del medidor en
// [from, to]. Extremo superior inclusivo, como el resto de los rangos de
// consulta del API (/meters/:id/readings, /meters/:id/events): el rango por
// defecto del dashboard es exactamente [MIN(timestamp), MAX(timestamp)], así
// que un `<` exclusivo se comería la última lectura de cada medidor.
func consumptionForRange(db *store.DB, meterID string, from, to time.Time) float64 {
	var total sql.NullFloat64
	db.QueryRow(`SELECT SUM(consumption_kwh) FROM readings
		WHERE meter_id = ? AND timestamp >= ? AND timestamp <= ?`,
		meterID, from.Format(time.RFC3339), to.Format(time.RFC3339)).Scan(&total)
	return total.Float64
}

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
		// Buffer meter rows before running any nested per-meter query: the
		// in-memory test DB is pinned to a single pooled connection
		// (store.Open), so a second Query while this cursor is open would
		// self-deadlock (same fix as meters.go's registerMeterRoutes).
		type meterRow struct{ meterID, status string }
		var meterRows []meterRow
		for rows.Next() {
			var mr meterRow
			if err := rows.Scan(&mr.meterID, &mr.status); err != nil {
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

		byMeter := make([]dashboardByMeterItem, 0, len(meterRows))
		total := 0.0
		anomaliesCount, highPriority := 0, 0
		var confSum float64
		confCount := 0

		for _, mr := range meterRows {
			// Consumo del rango leído directamente de `readings`, NO vía
			// BaselineForRange: esa función está (correctamente) supeditada a
			// que exista un análisis vigente, y el consumo es un hecho de las
			// lecturas crudas, no un artefacto del análisis. Enrutarlo por ahí
			// dejaba el dashboard en 0 kWh para toda la flota antes del primer
			// análisis — justo la primera pantalla de la demo (spec 05:
			// "Dashboard (12 medidores, 'Sin análisis')").
			actualKWh := consumptionForRange(s.DB, mr.meterID, from, to)
			item := dashboardByMeterItem{MeterID: mr.meterID, ConsumptionKWh: actualKWh, Status: mr.status}
			total += actualKWh

			// severity/status are meter-level, "current anomaly" attributes
			// that don't change with the date filter (spec 03: "El status no
			// cambia con el filtro de fechas"); only the anomalies/
			// high_priority/avg_confidence *counts* are restricted to
			// anomalies whose active window overlaps the queried range.
			anomaly, _ := CurrentAnomaly(s.DB, mr.meterID)
			if anomaly != nil {
				item.Severity = anomaly.Severity
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
			byMeter = append(byMeter, item)
		}

		// share_pct sobre el total del rango (spec 03 "by_meter... share_pct
		// sobre el total del rango").
		if total > 0 {
			for i := range byMeter {
				byMeter[i].SharePct = byMeter[i].ConsumptionKWh / total * 100
			}
		}

		// ordenar by_meter de mayor a menor consumo (alimenta el pastel).
		for i := 1; i < len(byMeter); i++ {
			for j := i; j > 0 && byMeter[j].ConsumptionKWh > byMeter[j-1].ConsumptionKWh; j-- {
				byMeter[j], byMeter[j-1] = byMeter[j-1], byMeter[j]
			}
		}

		avgConfidence := 0.0
		if confCount > 0 {
			avgConfidence = confSum / float64(confCount)
		}

		var lastAnalysis *lastAnalysisSummary
		var lastStatus string
		var startedAt, finishedAt sql.NullString
		err = s.DB.QueryRow(`SELECT status, started_at, finished_at FROM analyses ORDER BY id DESC LIMIT 1`).
			Scan(&lastStatus, &startedAt, &finishedAt)
		if err == nil {
			at := startedAt.String
			if finishedAt.Valid && finishedAt.String != "" {
				at = finishedAt.String
			}
			lastAnalysis = &lastAnalysisSummary{At: at, Status: lastStatus}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data_range":            map[string]string{"from": dataFrom.Format(time.RFC3339), "to": dataTo.Format(time.RFC3339)},
			"period":                map[string]string{"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339)},
			"meters":                len(byMeter),
			"total_consumption_kwh": total,
			"anomalies":             anomaliesCount,
			"high_priority":         highPriority,
			"avg_confidence":        avgConfidence,
			"last_analysis":         lastAnalysis,
			"by_meter":              byMeter,
		})
	})
}
