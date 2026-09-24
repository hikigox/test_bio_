package engine

import (
	"testing"
	"time"
)

func TestRunDetectsRealAnomalyLikeM109(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 20.35 // ~+103.7% sobre 10
		}
		return 10
	})
	for i := range readings {
		if readings[i].Timestamp.After(shiftAt) {
			readings[i].CurrentA = 400 // cambio eléctrico coherente
		} else {
			readings[i].CurrentA = 100
		}
	}
	result := Run("M-TEST-109", readings, nil, 0.1, DefaultConfig())
	if !result.HasAnomaly || result.Type != RealAnomaly || result.Severity != High {
		t.Fatalf("expected REAL_ANOMALY/HIGH, got %+v", result)
	}
	if result.Confidence < 0.6 {
		t.Fatalf("expected reasonably high confidence, got %v", result.Confidence)
	}
	if result.Reason == "" || result.RecommendedAction == "" {
		t.Fatalf("expected reason and recommendation to be filled, got %+v", result)
	}
	if result.PriorityScore < 2 || result.PriorityScore >= 3 {
		t.Fatalf("expected REAL_ANOMALY priority score in band [2,3), got %v", result.PriorityScore)
	}
}

func TestRunFalsePositiveLikeM106(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	outageAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 4 // caída fuerte
		}
		return 10
	})
	events := []Event{{MeterID: "M-TEST-106", Timestamp: outageAt, Type: "SCHEDULED_OUTAGE", Description: "maintenance"}}
	result := Run("M-TEST-106", readings, events, 0.1, DefaultConfig())
	if !result.HasAnomaly || result.Type != FalsePositive || result.Severity != Low {
		t.Fatalf("expected FALSE_POSITIVE/LOW, got %+v", result)
	}
}

func TestRunNoAnomalyOnStableMeter(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 + float64(h%24)*0.1 })
	result := Run("M-TEST-STABLE", readings, nil, 0.1, DefaultConfig())
	if result.HasAnomaly {
		t.Fatalf("expected no anomaly on a stable meter, got %+v", result)
	}
	if result.Type != "" || result.Severity != "" {
		t.Fatalf("expected zero-value type/severity when there is no anomaly, got %+v", result)
	}
}

// TestRunNoChangePointDoesNotPanic verifies the "no change point" carry-forward
// concern directly: a meter with too little history for DetectChangePoint to
// find a change point (< 2*MinBaselineDays) must fall through Run cleanly
// (HasAnomaly: false, no panic, no garbage baseline window).
func TestRunNoChangePointDoesNotPanic(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 2*24, func(h int) float64 { return 10 })
	result := Run("M-TEST-SHORT", readings, nil, 0.1, DefaultConfig())
	if result.ChangePoint.Found {
		t.Fatalf("expected no change point on 2 days of flat data, got %+v", result.ChangePoint)
	}
	if result.HasAnomaly {
		t.Fatalf("expected no anomaly, got %+v", result)
	}
}

// TestRunEmptyReadingsDoesNotPanic covers the degenerate zero-readings input:
// the brief's reference Run indexes readings[0] unconditionally, which would
// panic on an empty slice.
func TestRunEmptyReadingsDoesNotPanic(t *testing.T) {
	result := Run("M-TEST-EMPTY", nil, nil, 0.1, DefaultConfig())
	if result.HasAnomaly {
		t.Fatalf("expected no anomaly for empty readings, got %+v", result)
	}
	if result.MeterID != "M-TEST-EMPTY" {
		t.Fatalf("expected MeterID to still be set, got %+v", result)
	}
}

// TestRunDataQualityPriorityScoreReflectsSignalStrength is the mandatory
// carry-forward test #2: PriorityScore's magnitude term must not degenerate
// to a constant for DATA_QUALITY, since variationPct is ~0 by construction in
// that branch (Classify requires stable consumption). Two meters with stable
// consumption but electrical inconsistencies of very different magnitude
// (weak vs. strong deviation of the kWh/(V·I·PF) ratio) must get different
// priority scores, proportional to how strong the inconsistency actually is.
func TestRunDataQualityPriorityScoreReflectsSignalStrength(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	build := func(meterID string, voltageAfter float64) MeterResult {
		readings := hourlyReadings(start, 7*24, func(h int) float64 { return 10 })
		for i := range readings {
			readings[i].MeterID = meterID
			if i > 100 {
				readings[i].VoltageV = voltageAfter // rompe el ratio kWh/(V*I*PF)
			}
		}
		return Run(meterID, readings, nil, 0.1, DefaultConfig())
	}

	weak := build("M-TEST-DQ-WEAK", 260)     // desviación leve del ratio
	strong := build("M-TEST-DQ-STRONG", 500) // desviación fuerte del ratio

	if weak.Type != TypeDataQuality || strong.Type != TypeDataQuality {
		t.Fatalf("expected both meters classified as DATA_QUALITY, got weak=%+v strong=%+v", weak, strong)
	}
	if weak.VariationPct != 0 && abs(weak.VariationPct) > 1 {
		t.Fatalf("expected ~stable consumption (variationPct near 0) for the DATA_QUALITY branch, got %v", weak.VariationPct)
	}
	if strong.PriorityScore <= weak.PriorityScore {
		t.Fatalf("expected strong inconsistency to score higher than weak one, got weak=%v strong=%v",
			weak.PriorityScore, strong.PriorityScore)
	}
}

// TestRunFiltersEventsByMeterID is the mandatory carry-forward test: MatchEvents
// only filters by time window, not by MeterID, so Run itself must filter events
// down to the meter it is currently processing. Two meters share a change-point
// window; only meter B has an event of its own. Meter A must NOT pick it up.
func TestRunFiltersEventsByMeterID(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dropAt := start.Add(7 * 24 * time.Hour)
	kwhFn := func(h int) float64 {
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return 4 // caída fuerte en ambos medidores, mismo instante
		}
		return 10
	}
	readingsA := hourlyReadings(start, 14*24, kwhFn)
	for i := range readingsA {
		readingsA[i].MeterID = "M-TEST-A"
	}
	readingsB := hourlyReadings(start, 14*24, kwhFn)
	for i := range readingsB {
		readingsB[i].MeterID = "M-TEST-B"
	}

	// Only meter B has a SCHEDULED_OUTAGE event, timed to fall inside the
	// ±24h window of BOTH meters' change points (they share the same drop).
	events := []Event{
		{MeterID: "M-TEST-B", Timestamp: dropAt, Type: "SCHEDULED_OUTAGE", Description: "planned maintenance for B"},
	}

	resultA := Run("M-TEST-A", readingsA, events, 0.1, DefaultConfig())
	resultB := Run("M-TEST-B", readingsB, events, 0.1, DefaultConfig())

	if len(resultA.Events) != 0 {
		t.Fatalf("meter A must not pick up meter B's event, got %+v", resultA.Events)
	}
	if resultA.Type != RealAnomaly {
		t.Fatalf("meter A has no explaining event of its own, expected REAL_ANOMALY, got %+v", resultA)
	}

	if len(resultB.Events) != 1 {
		t.Fatalf("meter B should match its own event, got %+v", resultB.Events)
	}
	if resultB.Type != FalsePositive {
		t.Fatalf("meter B's own SCHEDULED_OUTAGE should explain the drop as FALSE_POSITIVE, got %+v", resultB)
	}
}
