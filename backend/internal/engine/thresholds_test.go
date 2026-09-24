package engine

import (
	"testing"
	"time"
)

func TestComputeThresholdsUsesFloorOnStableMeter(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 { return 10 }) // sin ruido
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())
	if th.VariationPct != 15 {
		t.Fatalf("expected floor of 15%%, got %v", th.VariationPct)
	}
	if th.Origin != Calculated {
		t.Fatalf("expected CALCULATED origin with >=3 days of data, got %v", th.Origin)
	}
}

func TestComputeThresholdsFallsBackOnShortWindow(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 2*24, func(h int) float64 { return 10 }) // < 3 días
	th := ComputeThresholds(readings, start, start.Add(2*24*time.Hour), 0.2, DefaultConfig())
	if th.Origin != Fallback {
		t.Fatalf("expected FALLBACK origin for window < 3 days, got %v", th.Origin)
	}
}

func TestComputeThresholdsScalesWithNoise(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// Nota: h%6==0 ocurriría exactamente 4 veces por cada día de 24h (ya que
	// 24 es múltiplo de 6), produciendo el mismo total diario todos los días
	// (MAD=0, CV=0) — el piso siempre ganaría y la aserción sería trivialmente
	// falsa. En su lugar, variamos el consumo por día calendario para que los
	// totales diarios difieran de verdad entre sí (ruido "day-to-day" real).
	noisy := hourlyReadings(start, 7*24, func(h int) float64 {
		day := h / 24
		return 10 + float64(day) // cada día suma más kWh/hora que el anterior
	})
	th := ComputeThresholds(noisy, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())
	if th.VariationPct <= 15 {
		t.Fatalf("expected threshold above the 15%% floor for a noisy meter, got %v", th.VariationPct)
	}
}

func TestComputeThresholdsFallbackUsesFleetCV(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 2*24, func(h int) float64 { return 10 })
	fleetCV := 0.5 // grande, para que domine sobre el piso
	th := ComputeThresholds(readings, start, start.Add(2*24*time.Hour), fleetCV, DefaultConfig())
	cfg := DefaultConfig()
	expected := cfg.VariationPctK * fleetCV * 100
	if th.VariationPct != expected {
		t.Fatalf("expected fallback threshold %v (from fleetCV), got %v", expected, th.VariationPct)
	}
}

func TestComputeThresholdsSetsOutlierZFromConfig(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 7*24, func(h int) float64 { return 10 })
	cfg := DefaultConfig()
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, cfg)
	if th.OutlierZ != cfg.OutlierZThreshold {
		t.Fatalf("expected OutlierZ %v, got %v", cfg.OutlierZThreshold, th.OutlierZ)
	}
}
