package engine

import (
	"testing"
	"time"
)

func findDelta(t *testing.T, deltas []VariableDelta, name string) VariableDelta {
	t.Helper()
	for i := range deltas {
		if deltas[i].Variable == name {
			return deltas[i]
		}
	}
	t.Fatalf("variable %q not present in deltas %+v", name, deltas)
	return VariableDelta{}
}

func TestCorrelateVariablesDetectsChangedCurrent(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	for i := range readings {
		if readings[i].Timestamp.After(shiftAt) {
			readings[i].CurrentA = 50 // sube fuerte tras el cambio
		} else {
			readings[i].CurrentA = 10
		}
	}
	profile := BuildHourlyProfile(readings, start, shiftAt)
	cp := ChangePoint{Found: true, At: shiftAt}

	deltas := CorrelateVariables(readings, profile, cp)
	var currentDelta *VariableDelta
	for i := range deltas {
		if deltas[i].Variable == "current_a" {
			currentDelta = &deltas[i]
		}
	}
	if currentDelta == nil || !currentDelta.Changed {
		t.Fatalf("expected current_a to be marked as changed, got %+v", deltas)
	}
	if currentDelta.Baseline != 10 || currentDelta.Actual != 50 {
		t.Fatalf("expected baseline 10 / actual 50, got %+v", *currentDelta)
	}
	if currentDelta.DeltaPct != 400 {
		t.Fatalf("expected deltaPct 400, got %v", currentDelta.DeltaPct)
	}
}

func TestCorrelateVariablesReturnsAllThreeElectricalVariables(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	profile := BuildHourlyProfile(readings, start, start.Add(7*24*time.Hour))
	cp := ChangePoint{Found: true, At: start.Add(7 * 24 * time.Hour)}

	deltas := CorrelateVariables(readings, profile, cp)
	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas (voltage_v, current_a, power_factor), got %+v", deltas)
	}
	for _, name := range []string{"voltage_v", "current_a", "power_factor"} {
		d := findDelta(t, deltas, name)
		if d.Changed {
			t.Fatalf("%s should not be marked changed on a perfectly stable meter, got %+v", name, d)
		}
		if d.DeltaPct != 0 {
			t.Fatalf("%s should have deltaPct 0 on a stable meter, got %v", name, d.DeltaPct)
		}
	}
}

func TestCorrelateVariablesWithoutChangePointSplitsAtBaselineWindowEnd(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	windowTo := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	for i := range readings {
		if readings[i].Timestamp.Before(windowTo) {
			readings[i].PowerFactor = 0.95
		} else {
			readings[i].PowerFactor = 0.60
		}
	}
	profile := BuildHourlyProfile(readings, start, windowTo)

	deltas := CorrelateVariables(readings, profile, ChangePoint{Found: false})
	pf := findDelta(t, deltas, "power_factor")
	if pf.Baseline != 0.95 || pf.Actual != 0.60 {
		t.Fatalf("expected split at profile.WindowTo (baseline 0.95 / actual 0.60), got %+v", pf)
	}
	if !pf.Changed || pf.DeltaPct >= 0 {
		t.Fatalf("expected power_factor marked changed with a negative delta, got %+v", pf)
	}
}

func TestCorrelateVariablesBelowTenPercentIsNotChanged(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	for i := range readings {
		if readings[i].Timestamp.Before(shiftAt) {
			readings[i].VoltageV = 220
		} else {
			readings[i].VoltageV = 231 // +5%
		}
	}
	profile := BuildHourlyProfile(readings, start, shiftAt)

	deltas := CorrelateVariables(readings, profile, ChangePoint{Found: true, At: shiftAt})
	v := findDelta(t, deltas, "voltage_v")
	if v.Changed {
		t.Fatalf("a 5%% shift is below the 10%% threshold and must not be marked changed, got %+v", v)
	}
	if v.DeltaPct <= 0 {
		t.Fatalf("expected a positive deltaPct, got %v", v.DeltaPct)
	}
}

// Si un lado del corte queda vacío (medidor cuya serie completa cabe en la
// ventana baseline y sin punto de cambio), no hay comparación posible: el delta
// debe ser neutro, no un -100% espurio.
func TestCorrelateVariablesWithEmptySideIsNotChanged(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 3*24, func(h int) float64 { return 10 })
	windowTo := start.Add(3 * 24 * time.Hour) // no hay lecturas en o después del corte
	profile := BuildHourlyProfile(readings, start, windowTo)

	deltas := CorrelateVariables(readings, profile, ChangePoint{Found: false})
	for _, name := range []string{"voltage_v", "current_a", "power_factor"} {
		d := findDelta(t, deltas, name)
		if d.Changed || d.DeltaPct != 0 {
			t.Fatalf("%s with no readings after the split must be neutral, got %+v", name, d)
		}
	}
}
