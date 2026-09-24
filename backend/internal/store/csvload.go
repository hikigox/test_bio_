package store

import (
	"encoding/csv"
	"fmt"
	"os"
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
			break // io.EOF
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
			break
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
		idx[name] = i
	}
	return idx
}
