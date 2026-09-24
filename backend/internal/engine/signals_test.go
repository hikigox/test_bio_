package engine

import (
	"testing"
	"time"
)

func TestDetectSignalsPersistentShift(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		base := 10.0
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return base * 2.2
		}
		return base
	})
	profile := BuildHourlyProfile(readings, start, shiftAt)
	cp := ChangePoint{Found: true, At: shiftAt, Sigma: 1}
	th := ComputeThresholds(readings, start, shiftAt, 0.1, DefaultConfig())

	signals, _ := DetectSignals(readings, profile, cp, th, DefaultConfig())
	found := false
	for _, s := range signals {
		if s.Signal == PersistentShift {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PERSISTENT_SHIFT signal, got %+v", signals)
	}
}

func TestDetectSignalsOutlierSpike(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 {
		if h == 50 {
			return 100 // spike aislado
		}
		return 10
	})
	profile := BuildHourlyProfile(readings, start, start.Add(7*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())

	signals, _ := DetectSignals(readings, profile, ChangePoint{}, th, DefaultConfig())
	found := false
	for _, s := range signals {
		if s.Signal == Outlier {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected OUTLIER signal for isolated spike, got %+v", signals)
	}
}

func TestDetectSignalsDataQualityOutOfRangePowerFactor(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 3*24, func(h int) float64 { return 10 })
	readings[5].PowerFactor = 1.4 // fuera de [0,1], no debe pánico ni NaN
	readings[6].VoltageV = 0      // división por cero potencial en consistencia eléctrica

	profile := BuildHourlyProfile(readings, start, start.Add(3*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(3*24*time.Hour), 0.1, DefaultConfig())

	signals, issues := DetectSignals(readings, profile, ChangePoint{}, th, DefaultConfig())
	if len(issues) == 0 {
		t.Fatal("expected at least one data quality issue for out-of-range PF and zero voltage")
	}
	for _, s := range signals {
		if s.Observed != s.Observed { // NaN check
			t.Fatalf("signal %+v has NaN Observed", s)
		}
	}
}

func TestDetectSignalsElectricalInconsistency(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 {
		// kWh estable pero V*I*PF cambia de forma incoherente (simulando cableado/sensor)
		return 10
	})
	for i := range readings {
		if i > 100 {
			readings[i].VoltageV = 300 // ratio kWh/(V*I*PF) se rompe
		}
	}
	profile := BuildHourlyProfile(readings, start, start.Add(4*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(4*24*time.Hour), 0.1, DefaultConfig())

	signals, _ := DetectSignals(readings, profile, ChangePoint{}, th, DefaultConfig())
	found := false
	for _, s := range signals {
		if s.Signal == ElectricalInconsistency {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ELECTRICAL_INCONSISTENCY signal, got %+v", signals)
	}
}
