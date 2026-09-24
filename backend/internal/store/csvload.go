package store

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const timeLayout = "2006-01-02 15:04:05"
const timeLayoutShort = "2006-01-02 15:04" // events.csv usa HH:MM sin segundos

func parseUTC(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(timeLayoutShort, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func LoadReadingsCSV(db *DB, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	idx := colIndex(header)
	if err := checkRequiredColumns(idx, requiredReadingsColumns, path); err != nil {
		return 0, err
	}

	stmt, err := db.Prepare(`INSERT OR IGNORE INTO readings
		(meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for {
		row, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return inserted, err
		}
		ts, err := parseUTC(row[idx["timestamp"]])
		if err != nil {
			return inserted, err
		}
		status := "OK"
		if i, ok := idx["status"]; ok {
			status = row[i]
		}
		res, err := stmt.Exec(row[idx["meter_id"]], ts.Format(time.RFC3339),
			row[idx["consumption_kwh"]], row[idx["voltage_v"]], row[idx["current_a"]],
			row[idx["power_factor"]], status)
		if err != nil {
			return inserted, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func LoadEventsCSV(db *DB, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	idx := colIndex(header) // meter_id, event_timestamp, event_type, description
	if err := checkRequiredColumns(idx, requiredEventsColumns, path); err != nil {
		return 0, err
	}

	stmt, err := db.Prepare(`INSERT INTO events (meter_id, timestamp, type, description)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS (
			SELECT 1 FROM events WHERE meter_id = ? AND timestamp = ? AND type = ?
		)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for {
		row, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return inserted, err
		}
		ts, err := parseUTC(row[idx["event_timestamp"]])
		if err != nil {
			return inserted, err
		}
		meterID := row[idx["meter_id"]]
		typ := row[idx["event_type"]]
		desc := row[idx["description"]]
		tsStr := ts.Format(time.RFC3339)
		res, err := stmt.Exec(meterID, tsStr, typ, desc, meterID, tsStr, typ)
		if err != nil {
			return inserted, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func colIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, name := range header {
		idx[strings.TrimSpace(name)] = i
	}
	return idx
}

// ErrMissingColumn is returned when a CSV header is missing a column the
// loader requires. Without this check a lookup of an absent column would
// return Go's zero value 0 and silently be read as "column index 0", writing
// (for example) the meter_id string into consumption_kwh.
var ErrMissingColumn = errors.New("columna requerida ausente en la cabecera del CSV")

// requiredReadingsColumns / requiredEventsColumns list the header columns each
// loader indexes unconditionally. Optional columns (readings' "status") are
// deliberately excluded: they are already looked up with the comma-ok form.
var (
	requiredReadingsColumns = []string{"meter_id", "timestamp", "consumption_kwh", "voltage_v", "current_a", "power_factor"}
	requiredEventsColumns   = []string{"meter_id", "event_timestamp", "event_type", "description"}
)

// checkRequiredColumns verifies every required column is present in the header
// before any row is processed, and names all the missing ones at once.
func checkRequiredColumns(idx map[string]int, required []string, file string) error {
	var missing []string
	for _, name := range required {
		if _, ok := idx[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: %w: %s", file, ErrMissingColumn, strings.Join(missing, ", "))
	}
	return nil
}
