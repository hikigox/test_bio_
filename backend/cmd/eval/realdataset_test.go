package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"energy-management/internal/engine"
	"energy-management/internal/store"
)

// TestRealDatasetMatchesAcceptanceTable runs the full pipeline over the real
// dataset shipped in the repo (data/readings.csv, data/events.csv) and asserts
// the acceptance criteria that spec 05 documents in prose.
//
// The expected values below are transcribed from spec 05's own table, NOT read
// from expected_results.csv: spec 00 regla 1 forbids that file from entering
// the application or any test, and cmd/eval's --expected flag remains the only
// place it is ever read.
//
//	M-109  REAL_ANOMALY · HIGH · confidence >= 0.9 · #1 priority · ~+103.7%
//	M-112  DATA_QUALITY · HIGH
//	M-104  EXPLAINABLE_ANOMALY · MEDIUM
//	M-106  FALSE_POSITIVE · LOW (never outranks a REAL_ANOMALY)
//	rest   no anomaly
//	total  exactly 4 anomalies
func TestRealDatasetMatchesAcceptanceTable(t *testing.T) {
	dataDir := findRepoDataDir(t)
	if dataDir == "" {
		t.Skip("real dataset (data/readings.csv, data/events.csv) not found relative to repo root")
	}

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := store.LoadReadingsCSV(db, filepath.Join(dataDir, "readings.csv")); err != nil {
		t.Fatalf("load readings: %v", err)
	}
	if _, err := store.LoadEventsCSV(db, filepath.Join(dataDir, "events.csv")); err != nil {
		t.Fatalf("load events: %v", err)
	}

	rows, err := db.Query(`SELECT DISTINCT meter_id FROM readings`)
	if err != nil {
		t.Fatalf("meter ids: %v", err)
	}
	var meterIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		meterIDs = append(meterIDs, id)
	}
	rows.Close()
	sort.Strings(meterIDs)

	results := make([]engine.MeterResult, 0, len(meterIDs))
	for _, id := range meterIDs {
		results = append(results, engine.Run(id, loadMeterReadings(db, id), loadMeterEvents(db, id), 0.15, engine.DefaultConfig()))
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].PriorityScore > results[j].PriorityScore })

	byMeter := map[string]engine.MeterResult{}
	ranks := map[string]int{}
	anomalies := 0
	for _, r := range results {
		byMeter[r.MeterID] = r
		if r.HasAnomaly {
			anomalies++
			ranks[r.MeterID] = anomalies
		}
		t.Logf("%-6s rank=%-2s type=%-20s sev=%-6s conf=%.2f var=%+7.1f%% score=%.2f",
			r.MeterID, rankLabel(ranks[r.MeterID]), r.Type, r.Severity, r.Confidence, r.VariationPct, r.PriorityScore)
	}

	if anomalies != 4 {
		t.Errorf("spec 05: exactly 4 anomalies expected, got %d", anomalies)
	}

	want := []struct {
		meter string
		typ   engine.AnomalyType
		sev   engine.Severity
	}{
		{"M-109", engine.RealAnomaly, engine.High},
		{"M-112", engine.TypeDataQuality, engine.High},
		{"M-104", engine.ExplainableAnomaly, engine.Medium},
		{"M-106", engine.FalsePositive, engine.Low},
	}
	for _, w := range want {
		got, ok := byMeter[w.meter]
		if !ok {
			t.Errorf("%s: missing from results", w.meter)
			continue
		}
		if !got.HasAnomaly {
			t.Errorf("%s: expected an anomaly, got none", w.meter)
			continue
		}
		if got.Type != w.typ || got.Severity != w.sev {
			t.Errorf("%s: want %s/%s, got %s/%s", w.meter, w.typ, w.sev, got.Type, got.Severity)
		}
		if got.Reason == "" || got.RecommendedAction == "" {
			t.Errorf("%s: reason and recommended action must be filled", w.meter)
		}
		if len(got.Evidence.DecisionPath) == 0 {
			t.Errorf("%s: evidence must carry the decision path", w.meter)
		}
	}

	for _, id := range []string{"M-101", "M-102", "M-103", "M-105", "M-107", "M-108", "M-110", "M-111"} {
		if got := byMeter[id]; got.HasAnomaly {
			t.Errorf("%s: expected no anomaly, got %s/%s (var %+.1f%%)", id, got.Type, got.Severity, got.VariationPct)
		}
	}

	m109 := byMeter["M-109"]
	if ranks["M-109"] != 1 {
		t.Errorf("M-109 must be #1 in priority, got rank %d", ranks["M-109"])
	}
	if m109.Confidence < 0.9 {
		t.Errorf("M-109 confidence must be >= 0.9, got %v", m109.Confidence)
	}
	// Spec 05 documents M-109 as +103,7%. The engine must land on that jump's
	// real magnitude; a little tolerance keeps the test from pinning an exact
	// float, but the old whole-period span (~+19%) is nowhere near it.
	if m109.VariationPct < 95 || m109.VariationPct > 112 {
		t.Errorf("M-109 variation must be ~+103.7%%, got %+.1f%%", m109.VariationPct)
	}
	if !m109.ChangePoint.Found || m109.ChangePoint.At.Format("2006-01-02") != "2026-09-12" {
		t.Errorf("M-109 change point must land on 2026-09-12, got %+v", m109.ChangePoint)
	}

	// 02 §6: FALSE_POSITIVE never outranks a REAL_ANOMALY.
	if byMeter["M-106"].PriorityScore >= m109.PriorityScore {
		t.Errorf("M-106 (FALSE_POSITIVE, %v) must not outrank M-109 (%v)",
			byMeter["M-106"].PriorityScore, m109.PriorityScore)
	}
}

func rankLabel(rank int) string {
	if rank == 0 {
		return "-"
	}
	return fmt.Sprint(rank)
}
