package engine

import (
	"testing"
	"time"
)

func hourlyReadings(start time.Time, hours int, kwhFn func(h int) float64) []Reading {
	out := make([]Reading, 0, hours)
	for h := 0; h < hours; h++ {
		out = append(out, Reading{
			MeterID:        "M-TEST",
			Timestamp:      start.Add(time.Duration(h) * time.Hour),
			ConsumptionKWh: kwhFn(h),
			VoltageV:       220, CurrentA: 10, PowerFactor: 0.95, Status: "OK",
		})
	}
	return out
}

func TestDetectChangePointOnStableMeterReturnsNotFound(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		// perfil diario repetido, ruido pequeño determinista
		return 10 + float64(h%24)*0.1
	})
	cp := DetectChangePoint(readings, DefaultConfig())
	if cp.Found {
		t.Fatalf("expected no change point on stable meter, got %+v", cp)
	}
}

func TestDetectChangePointOnShiftedMeterFindsIt(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		base := 10 + float64(h%24)*0.1
		if time.Duration(h)*time.Hour >= 7*24*time.Hour {
			return base * 2.1 // +110%, muy por encima del piso del 15%
		}
		return base
	})
	cp := DetectChangePoint(readings, DefaultConfig())
	if !cp.Found {
		t.Fatal("expected change point to be found")
	}
	diff := cp.At.Sub(shiftAt)
	if diff < -12*time.Hour || diff > 12*time.Hour {
		t.Fatalf("change point %v too far from expected %v", cp.At, shiftAt)
	}
}

func TestBuildHourlyProfileMedianPerHour(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 3*24, func(h int) float64 {
		return float64(h % 24) // hora 5 siempre vale 5, etc.
	})
	profile := BuildHourlyProfile(readings, start, start.Add(3*24*time.Hour))
	if profile.MedianByHour[5] != 5 {
		t.Fatalf("expected median at hour 5 to be 5, got %v", profile.MedianByHour[5])
	}
}
