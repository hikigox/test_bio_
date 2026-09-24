package api

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"energy-management/internal/store"
	"github.com/go-chi/chi/v5"
)

type periodRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type baselineRefSummary struct {
	WindowFrom string `json:"window_from"`
	WindowTo   string `json:"window_to"`
	Method     string `json:"method"`
}

type meterListItem struct {
	MeterID        string              `json:"meter_id"`
	Name           string              `json:"name"`
	ConsumptionKWh *float64            `json:"consumption_kwh"`
	BaselineKWh    *float64            `json:"baseline_kwh"`
	VariationPct   *float64            `json:"variation_pct"`
	Status         string              `json:"status"`
	AnalysisID     *int64              `json:"analysis_id"`
	BaselineRef    *baselineRefSummary `json:"baseline_ref"`
	AnalysisPeriod *periodRange        `json:"analysis_period"`
	Anomaly        *anomalySummary     `json:"anomaly"`
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

// baselineDetail is the full baseline block on GET /meters/:meterId (spec 03:
// "igual que un item de la lista, más baseline{hourly_profile,voltage,
// current,power_factor,change_point_at}...").
type baselineDetail struct {
	HourlyProfile []float64 `json:"hourly_profile"`
	Voltage       float64   `json:"voltage"`
	Current       float64   `json:"current"`
	PowerFactor   float64   `json:"power_factor"`
	ChangePointAt *string   `json:"change_point_at"`
}

type meterDetail struct {
	meterListItem
	Baseline           *baselineDetail `json:"baseline"`
	CurrentVoltage     *float64        `json:"current_voltage"`
	CurrentCurrent     *float64        `json:"current_current"`
	CurrentPowerFactor *float64        `json:"current_power_factor"`
	LatestAnomaly      *anomalySummary `json:"latest_anomaly"`
}

// meterAnalysisFields fills the analysis-derived fields of item. Every meter
// that has ever been analyzed (Task 3's currentAnalysisID found a vigente
// analysis) gets analysis_id, baseline_ref, consumption/baseline/variation and
// analysis_period — whether or not that analysis produced an anomaly, since a
// `meter_baselines` row exists either way. Spec 03 line 82 only allows those
// fields to be null for a meter that was NEVER analyzed (where analysis_id is
// null too); returning them as null next to a non-null analysis_id would be
// self-contradictory. The anomaly summary is set only when the vigente
// analysis produced an anomaly. A never-analyzed meter leaves every pointer
// field nil, which json.Marshal renders as `null`.
//
// Returns the resolved analysisID/found/anomaly so callers building the
// GET /meters/:meterId detail response (baseline block, current V/I/PF,
// latest_anomaly) don't need to re-run the same lookups.
func meterAnalysisFields(s *Server, meterID string, item *meterListItem, ranks map[int64]int, from, to time.Time, hasRange bool) (analysisID int64, found bool, anomaly *AnomalyRow) {
	analysisID, found, err := currentAnalysisID(s.DB, meterID)
	if err != nil || !found {
		return 0, false, nil
	}
	item.AnalysisID = &analysisID

	if ref, err := fetchBaselineRef(s.DB, analysisID, meterID); err == nil {
		item.BaselineRef = ref
	}

	anomaly, err = CurrentAnomaly(s.DB, meterID)
	if err != nil {
		anomaly = nil
	}

	var baselineKWh, actualKWh, variationPct float64
	var figuresOK bool
	if !hasRange {
		// No caller-supplied date filter: report the analysis's own stored
		// figures for its full period, rather than recomputing from
		// readings (which would silently zero out if the raw readings for
		// that period are no longer retained). The anomaly row carries them
		// when there is one; otherwise they come from the meter_baselines
		// row that insertMeterBaseline writes for every analyzed meter.
		if anomaly != nil {
			baselineKWh, actualKWh, variationPct = anomaly.BaselineKWh, anomaly.ActualKWh, anomaly.VariationPct
			figuresOK = true
		} else if b, a, v, err := fetchBaselineFigures(s.DB, analysisID, meterID); err == nil {
			baselineKWh, actualKWh, variationPct = b, a, v
			figuresOK = true
		}
	} else if b, a, err := BaselineForRange(s.DB, meterID, from, to); err == nil {
		baselineKWh, actualKWh = b, a
		if baselineKWh != 0 {
			variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
		}
		figuresOK = true
	}
	if figuresOK {
		item.ConsumptionKWh, item.BaselineKWh, item.VariationPct = &actualKWh, &baselineKWh, &variationPct
	}

	// analysis_period is the period the analysis covered. With C2's fix the
	// anomaly's own period_from/period_to is exactly that period; for an
	// analyzed-but-normal meter there is no anomaly row, so it comes from the
	// analyses row's data_from/data_to.
	if anomaly != nil {
		item.AnalysisPeriod = &periodRange{From: anomaly.PeriodFrom.Format(time.RFC3339), To: anomaly.PeriodTo.Format(time.RFC3339)}
	} else if dataFrom, dataTo, err := fetchAnalysisDataPeriod(s.DB, analysisID); err == nil {
		item.AnalysisPeriod = &periodRange{From: dataFrom.Format(time.RFC3339), To: dataTo.Format(time.RFC3339)}
	}

	if anomaly == nil {
		return analysisID, true, nil
	}

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
	return analysisID, true, anomaly
}

// fetchBaselineRef reads the (window_from, window_to, method) reference from
// the meter's meter_baselines row for analysisID (spec 03 item.baseline_ref).
func fetchBaselineRef(db *store.DB, analysisID int64, meterID string) (*baselineRefSummary, error) {
	var windowFrom, windowTo, method sql.NullString
	err := db.QueryRow(`SELECT window_from, window_to, method FROM meter_baselines WHERE analysis_id = ? AND meter_id = ?`,
		analysisID, meterID).Scan(&windowFrom, &windowTo, &method)
	if err != nil {
		return nil, err
	}
	return &baselineRefSummary{WindowFrom: windowFrom.String, WindowTo: windowTo.String, Method: method.String}, nil
}

// fetchBaselineFigures reads the analysis's own stored baseline/actual/
// variation figures for a meter from meter_baselines. Used for a meter that
// was analyzed but found normal (no anomalies row to read them from).
func fetchBaselineFigures(db *store.DB, analysisID int64, meterID string) (baselineKWh, actualKWh, variationPct float64, err error) {
	var b, a, v sql.NullFloat64
	err = db.QueryRow(`SELECT baseline_kwh, actual_kwh, variation_pct FROM meter_baselines
		WHERE analysis_id = ? AND meter_id = ?`, analysisID, meterID).Scan(&b, &a, &v)
	if err != nil {
		return 0, 0, 0, err
	}
	return b.Float64, a.Float64, v.Float64, nil
}

// fetchBaselineDetail reads the full baseline block for GET /meters/:meterId
// (spec 03: hourly_profile, voltage/current/power_factor baseline, and
// change_point_at) from the meter's meter_baselines row for analysisID.
func fetchBaselineDetail(db *store.DB, analysisID int64, meterID string) (*baselineDetail, error) {
	var profileJSON string
	var changePointAt sql.NullString
	var voltage, current, powerFactor sql.NullFloat64
	err := db.QueryRow(`SELECT hourly_profile_json, change_point_at, baseline_voltage_v, baseline_current_a, baseline_power_factor
		FROM meter_baselines WHERE analysis_id = ? AND meter_id = ?`, analysisID, meterID).
		Scan(&profileJSON, &changePointAt, &voltage, &current, &powerFactor)
	if err != nil {
		return nil, err
	}
	var profile []float64
	if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
		return nil, err
	}
	detail := &baselineDetail{HourlyProfile: profile, Voltage: voltage.Float64, Current: current.Float64, PowerFactor: powerFactor.Float64}
	if changePointAt.Valid {
		v := changePointAt.String
		detail.ChangePointAt = &v
	}
	return detail, nil
}

// fetchAnalysisDataPeriod reads the overall data_from/data_to covered by
// analysisID (used as the "current" V/I/PF window when the meter has no
// current anomaly — i.e. it was analyzed but is currently normal — and the
// caller supplied no explicit ?from=&to=).
func fetchAnalysisDataPeriod(db *store.DB, analysisID int64) (from, to time.Time, err error) {
	var fromStr, toStr string
	if err := db.QueryRow(`SELECT data_from, data_to FROM analyses WHERE id = ?`, analysisID).Scan(&fromStr, &toStr); err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, _ = time.Parse(time.RFC3339, fromStr)
	to, _ = time.Parse(time.RFC3339, toStr)
	return from, to, nil
}

// averageElectricals computes the average voltage/current/power_factor over
// the meter's readings in [from, to) — the "current" V/I/PF for the period
// (spec 03 GET /meters/:meterId), mirroring how BaselineForRange sums
// consumption over the same kind of window.
func averageElectricals(db *store.DB, meterID string, from, to time.Time) (voltage, current, powerFactor *float64, err error) {
	var v, c, pf sql.NullFloat64
	err = db.QueryRow(`SELECT AVG(voltage_v), AVG(current_a), AVG(power_factor) FROM readings
		WHERE meter_id = ? AND timestamp >= ? AND timestamp < ?`,
		meterID, from.Format(time.RFC3339), to.Format(time.RFC3339)).Scan(&v, &c, &pf)
	if err != nil {
		return nil, nil, nil, err
	}
	if v.Valid {
		voltage = &v.Float64
	}
	if c.Valid {
		current = &c.Float64
	}
	if pf.Valid {
		powerFactor = &pf.Float64
	}
	return voltage, current, powerFactor, nil
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

		rowsOut := []meterListRow{}
		for _, mr := range meterRows {
			item := meterListItem{MeterID: mr.meterID, Name: mr.name.String, Status: mr.status}
			_, _, anomaly := meterAnalysisFields(s, mr.meterID, &item, ranks, from, to, hasRange)
			rowsOut = append(rowsOut, meterListRow{item: item, anomaly: anomaly})
		}
		rowsOut = filterMeterRows(rowsOut, req.URL.Query().Get("status"), req.URL.Query().Get("q"))
		sortMeterRows(rowsOut, req.URL.Query().Get("sort"), req.URL.Query().Get("order"))
		items := make([]meterListItem, 0, len(rowsOut))
		for _, r := range rowsOut {
			items = append(items, r.item)
		}
		// Top-level period echoes the caller's ?from=&to= verbatim. There is
		// no single well-defined "period" for a fleet-wide list when no
		// filter is given — different meters can have different analysis
		// periods — so it's left blank (matching spec 03's own JSON example,
		// which shows blank placeholders here) rather than picking one
		// meter's period arbitrarily.
		period := periodRange{}
		if hasRange {
			period.From, period.To = from.Format(time.RFC3339), to.Format(time.RFC3339)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"period": period, "items": items})
	})

	r.Get("/meters/{meterId}", func(w http.ResponseWriter, req *http.Request) {
		meterID := chi.URLParam(req, "meterId")
		from, to, hasRange := parseRangeParams(req)
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
		analysisID, found, anomaly := meterAnalysisFields(s, meterID, &item, ranks, from, to, hasRange)

		detail := meterDetail{meterListItem: item, LatestAnomaly: item.Anomaly}
		if found {
			if b, err := fetchBaselineDetail(s.DB, analysisID, meterID); err == nil {
				detail.Baseline = b
			}
			vipFrom, vipTo := from, to
			if !hasRange {
				if anomaly != nil {
					vipFrom, vipTo = anomaly.PeriodFrom, anomaly.PeriodTo
				} else if dataFrom, dataTo, err := fetchAnalysisDataPeriod(s.DB, analysisID); err == nil {
					vipFrom, vipTo = dataFrom, dataTo
				}
			}
			if v, c, pf, err := averageElectricals(s.DB, meterID, vipFrom, vipTo); err == nil {
				detail.CurrentVoltage, detail.CurrentCurrent, detail.CurrentPowerFactor = v, c, pf
			}
		}
		writeJSON(w, http.StatusOK, detail)
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
		from, to, hasRange := parseRangeParams(req)
		query := `SELECT timestamp, type, description FROM events WHERE meter_id = ?`
		args := []interface{}{meterID}
		if hasRange {
			query += ` AND timestamp >= ? AND timestamp <= ?`
			args = append(args, from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
		query += ` ORDER BY timestamp`
		rows, err := s.DB.Query(query, args...)
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
		from, to, hasRange := parseRangeParams(req)
		// scope=current (el valor por defecto) = la anomalía del análisis
		// VIGENTE, es decir el último COMPLETED que INCLUYÓ al medidor
		// (currentAnalysisID, spec 03 línea 38) — no el último que le generó
		// una fila en anomalies. Si el análisis vigente no le encontró nada,
		// la respuesta es vacía: no se cae hacia una anomalía más antigua,
		// que es justamente el dato obsoleto que la regla de vigencia
		// descarta (mismo criterio que CurrentAnomaly/priorityRanks/
		// GET /anomalies).
		query := `SELECT a.id, a.type, a.severity, a.confidence, a.status, a.period_from, a.period_to, a.change_point_at
			FROM anomalies a
			JOIN analyses an ON an.id = a.analysis_id WHERE a.meter_id = ? AND an.status = 'COMPLETED'`
		args := []interface{}{meterID}
		if scope != "history" {
			analysisID, found, err := currentAnalysisID(s.DB, meterID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
				return
			}
			if !found {
				writeJSON(w, http.StatusOK, map[string]interface{}{"items": []interface{}{}})
				return
			}
			query += ` AND a.analysis_id = ?`
			args = append(args, analysisID)
		}
		query += ` ORDER BY an.finished_at DESC`
		rows, err := s.DB.Query(query, args...)
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
			var periodFrom, periodTo string
			var changePointAt sql.NullString
			if err := rows.Scan(&i.ID, &i.Type, &i.Severity, &i.Confidence, &i.Status, &periodFrom, &periodTo, &changePointAt); err != nil {
				writeError(w, http.StatusInternalServerError, "error leyendo anomalías")
				return
			}
			if hasRange {
				a := AnomalyRow{}
				a.PeriodFrom, _ = time.Parse(time.RFC3339, periodFrom)
				a.PeriodTo, _ = time.Parse(time.RFC3339, periodTo)
				if changePointAt.Valid {
					t, _ := time.Parse(time.RFC3339, changePointAt.String)
					a.ChangePointAt = &t
				}
				activeFrom, activeTo := ActiveWindow(a)
				if !Overlaps(activeFrom, activeTo, from, to) {
					continue
				}
			}
			out = append(out, i)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
	})
}

// meterListRow carries a response item plus the vigente anomaly row it came
// from. The anomaly isn't part of the JSON shape, but sort=severity needs its
// priority_score for the tie-break (spec 03 line 69), which the response's
// anomaly summary doesn't expose.
type meterListRow struct {
	item    meterListItem
	anomaly *AnomalyRow
}

// meterStatusFilter maps spec 03 line 70's filter vocabulary
// ("normal = OK, alert = ALERT, critical = CRITICAL") onto meters.status.
var meterStatusFilter = map[string]string{"normal": "OK", "alert": "ALERT", "critical": "CRITICAL"}

// filterMeterRows applies ?status= and ?q=.
//
//   - status: all|normal|alert|critical (spec 03). "all", empty, or any
//     unrecognized value means no filter — a nonsense value returning the
//     whole fleet is friendlier for a demo UI than a 400, and spec 03 doesn't
//     ask for validation here.
//   - q: case-insensitive substring match on meter_id. Spec 03 doesn't define
//     the search field; meter_id is what the Meters screen shows and filters
//     on, so it's the minimal plausible reading. Judgment call.
func filterMeterRows(rows []meterListRow, status, q string) []meterListRow {
	wantStatus := meterStatusFilter[strings.ToLower(status)]
	needle := strings.ToLower(strings.TrimSpace(q))
	if wantStatus == "" && needle == "" {
		return rows
	}
	out := make([]meterListRow, 0, len(rows))
	for _, r := range rows {
		if wantStatus != "" && r.item.Status != wantStatus {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.item.MeterID), needle) {
			continue
		}
		out = append(out, r)
	}
	return out
}

var severityRank = map[string]int{"LOW": 1, "MEDIUM": 2, "HIGH": 3}

// sortMeterRows applies ?sort= and ?order= (spec 03 line 22/69).
//
//   - sort: consumption|variation|severity. Anything else (including empty)
//     leaves the natural meter_id order untouched.
//   - order: asc|desc, defaulting to desc. Spec 03 lists the param but not its
//     values; desc is the only default that satisfies line 69's documented
//     severity ordering ("HIGH > MEDIUM > LOW > sin anomalía; desempate por
//     priority_score") and matches the dashboard's highest-consumption-first
//     convention. Judgment call.
//
// A never-analyzed meter has no consumption/variation figure; it sorts as the
// lowest possible value, so it lands last under the default desc order.
func sortMeterRows(rows []meterListRow, sortBy, order string) {
	var key func(meterListRow) (float64, float64)
	switch strings.ToLower(sortBy) {
	case "consumption":
		key = func(r meterListRow) (float64, float64) { return nullableFloat(r.item.ConsumptionKWh), 0 }
	case "variation":
		key = func(r meterListRow) (float64, float64) { return nullableFloat(r.item.VariationPct), 0 }
	case "severity":
		key = func(r meterListRow) (float64, float64) {
			if r.anomaly == nil {
				return 0, 0
			}
			return float64(severityRank[r.anomaly.Severity]), r.anomaly.PriorityScore
		}
	default:
		return
	}
	asc := strings.EqualFold(order, "asc")
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := i, j
		if !asc {
			a, b = j, i
		}
		ka, ta := key(rows[a])
		kb, tb := key(rows[b])
		if ka != kb {
			return ka < kb
		}
		return ta < tb
	})
}

// nullableFloat maps a missing (never-analyzed) figure to -Inf so it always
// sorts below any real value, in either direction.
func nullableFloat(v *float64) float64 {
	if v == nil {
		return math.Inf(-1)
	}
	return *v
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
