// Command eval is the ONLY place in the whole system allowed to read
// expected_results.csv (spec 00 regla 1): it must never be loaded by the
// application itself or by unit tests, only by this standalone tool via the
// explicit --expected flag, which is expected to point outside the repo.
//
// It loads the real dataset (readings.csv, events.csv) into an in-memory
// SQLite database, runs the engine per meter, and scores the result against
// the expected CSV per the fixed rubric (spec 05: 30/25/15/10/10/10).
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

type expectedRow struct {
	MeterID          string
	ExpectedType     string
	ExpectedSeverity string
}

type actualRow struct {
	Type         string
	Severity     string
	PriorityRank int
}

type rubricScore struct {
	DetectionPoints   int // 30: detecta correctamente cada REAL_ANOMALY esperado
	PriorityPoints    int // 25: el/los REAL_ANOMALY detectados tienen rank 1
	FalsePosPoints    int // 15: los FALSE_POSITIVE esperados no se clasifican como REAL_ANOMALY
	DataQualityPoints int // 10: los DATA_QUALITY esperados se clasifican como DATA_QUALITY
	EvidencePoints    int // 10: evidencia presente (no evaluado sin acceso a Evidence aquí; ver nota abajo)
	ActionPoints      int // 10: acción coherente (idem)
}

func (s rubricScore) Total() int {
	return s.DetectionPoints + s.PriorityPoints + s.FalsePosPoints + s.DataQualityPoints + s.EvidencePoints + s.ActionPoints
}

func parseExpectedCSV(path string) ([]expectedRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	if _, err := r.Read(); err != nil { // header
		return nil, err
	}
	var rows []expectedRow
	for {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		rows = append(rows, expectedRow{MeterID: rec[0], ExpectedType: rec[1], ExpectedSeverity: rec[2]})
	}
	return rows, nil
}

// scoreRubric compares the expected results against the actual engine
// output per meter, accumulating points across all expected rows (not just
// the first). Each expected meter contributes independently to the points
// for its own category; a mismatch or a missing actual result scores zero
// for that meter without aborting the rest.
func scoreRubric(expected []expectedRow, actual map[string]actualRow) rubricScore {
	var s rubricScore
	for _, e := range expected {
		a, ok := actual[e.MeterID]
		if !ok {
			continue
		}
		switch e.ExpectedType {
		case "REAL_ANOMALY":
			if a.Type == "REAL_ANOMALY" {
				s.DetectionPoints += 30
				if a.PriorityRank == 1 {
					s.PriorityPoints += 25
				}
			}
		case "FALSE_POSITIVE":
			if a.Type != "REAL_ANOMALY" {
				s.FalsePosPoints += 15
			}
		case "DATA_QUALITY":
			if a.Type == "DATA_QUALITY" {
				s.DataQualityPoints += 10
			}
		}
	}
	return s
}

func main() {
	expectedPath := flag.String("expected", "", "ruta a expected_results.csv (fuera del repo)")
	dataDir := flag.String("data", "./data", "directorio con readings.csv y events.csv")
	flag.Parse()

	if *expectedPath == "" {
		fmt.Fprintln(os.Stderr, "uso: eval --expected /ruta/expected_results.csv [--data ./data]")
		os.Exit(1)
	}

	db, err := store.Open(":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error abriendo BD:", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		fmt.Fprintln(os.Stderr, "error migrando BD:", err)
		os.Exit(1)
	}

	if _, err := store.LoadReadingsCSV(db, *dataDir+"/readings.csv"); err != nil {
		fmt.Fprintln(os.Stderr, "error cargando readings:", err)
		os.Exit(1)
	}
	if _, err := store.LoadEventsCSV(db, *dataDir+"/events.csv"); err != nil {
		fmt.Fprintln(os.Stderr, "error cargando events:", err)
		os.Exit(1)
	}

	// Ejecutar el motor por medidor. La consulta SQL real y el ranking de
	// prioridad entre medidores se comparten conceptualmente con la fase API
	// (Task 3 del plan de API); aquí se reimplementa una versión mínima
	// autosuficiente para no acoplar cmd/eval al servidor HTTP.
	results := runEngineOverStore(db)

	expected, err := parseExpectedCSV(*expectedPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error leyendo expected:", err)
		os.Exit(1)
	}

	score := scoreRubric(expected, results)
	fmt.Printf("Deteccion: %d/30\nPrioridad: %d/25\nFalso positivo: %d/15\nCalidad de datos: %d/10\nEvidencia: %d/10\nAccion: %d/10\nTOTAL: %d/100\n",
		score.DetectionPoints, score.PriorityPoints, score.FalsePosPoints, score.DataQualityPoints, score.EvidencePoints, score.ActionPoints, score.Total())
}

// runEngineOverStore agrupa lecturas/eventos por medidor y corre engine.Run
// para cada uno, asignando PriorityRank por orden descendente de
// PriorityScore entre los medidores con anomalia.
func runEngineOverStore(db *store.DB) map[string]actualRow {
	rows, err := db.Query(`SELECT DISTINCT meter_id FROM readings`)
	if err != nil {
		return nil
	}

	// Drain and close the meter_id query fully before issuing any per-meter
	// queries below. Opening a nested query on the same *sql.DB while these
	// rows are still open forces database/sql to grab a second pooled
	// connection — and for a ":memory:" SQLite database (as used by cmd/eval
	// and its tests), a second connection is a completely separate, empty
	// database with no shared cache. That silently made loadMeterReadings /
	// loadMeterEvents return zero rows for every meter, so engine.Run always
	// saw empty readings and every result came back HasAnomaly=false. Fully
	// collecting meterIDs first (single connection, then returned to the
	// pool) and querying sequentially afterward keeps everything on one
	// connection.
	var meterIDs []string
	for rows.Next() {
		var meterID string
		if err := rows.Scan(&meterID); err != nil {
			continue
		}
		meterIDs = append(meterIDs, meterID)
	}
	rows.Close()

	type scored struct {
		meterID string
		result  engine.MeterResult
	}
	var all []scored

	for _, meterID := range meterIDs {
		readings := loadMeterReadings(db, meterID)
		events := loadMeterEvents(db, meterID)
		result := engine.Run(meterID, readings, events, 0.15, engine.DefaultConfig())
		all = append(all, scored{meterID, result})
	}

	// Ordenar por PriorityScore descendente para asignar PriorityRank
	// (insertion sort: el numero de medidores del dataset es pequeno).
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].result.PriorityScore > all[j-1].result.PriorityScore; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}

	out := map[string]actualRow{}
	rank := 0
	for _, s := range all {
		if !s.result.HasAnomaly {
			continue
		}
		rank++
		out[s.meterID] = actualRow{Type: string(s.result.Type), Severity: string(s.result.Severity), PriorityRank: rank}
	}
	return out
}

func loadMeterReadings(db *store.DB, meterID string) []engine.Reading {
	rows, err := db.Query(`SELECT timestamp, consumption_kwh, voltage_v, current_a, power_factor, status
		FROM readings WHERE meter_id = ? ORDER BY timestamp`, meterID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []engine.Reading
	for rows.Next() {
		var r engine.Reading
		var ts string
		if err := rows.Scan(&ts, &r.ConsumptionKWh, &r.VoltageV, &r.CurrentA, &r.PowerFactor, &r.Status); err != nil {
			continue
		}
		t, err := parseUTCPublic(ts)
		if err != nil {
			continue
		}
		r.MeterID = meterID
		r.Timestamp = t
		out = append(out, r)
	}
	return out
}

func loadMeterEvents(db *store.DB, meterID string) []engine.Event {
	rows, err := db.Query(`SELECT timestamp, type, description FROM events WHERE meter_id = ?`, meterID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []engine.Event
	for rows.Next() {
		var e engine.Event
		var ts string
		if err := rows.Scan(&ts, &e.Type, &e.Description); err != nil {
			continue
		}
		t, err := parseUTCPublic(ts)
		if err != nil {
			continue
		}
		e.MeterID = meterID
		e.Timestamp = t
		out = append(out, e)
	}
	return out
}

// parseUTCPublic parses the RFC3339 timestamps that store.LoadReadingsCSV /
// store.LoadEventsCSV write into the database (Task 2). cmd/eval is a
// separate package from internal/store, so it cannot reuse store's
// unexported timeLayout constant/parseUTC helper and re-parses the RFC3339
// output format directly instead.
func parseUTCPublic(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
