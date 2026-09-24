package engine

import "time"

// dailyCV calcula el coeficiente de variación robusto (MADNorm/mediana) de los
// totales diarios en la ventana. Si la mediana es 0, retorna 0 (piso decide).
func dailyCV(readings []Reading, from, to time.Time) (cv float64, days int) {
	filtered := make([]Reading, 0, len(readings))
	for _, r := range readings {
		if !r.Timestamp.Before(from) && r.Timestamp.Before(to) {
			filtered = append(filtered, r)
		}
	}
	dayList, totals := dailyTotals(filtered)
	days = len(dayList)
	median := Median(totals)
	if median == 0 {
		return 0, days
	}
	return MADNorm(totals) / median, days
}

// ComputeThresholds implementa 02 §10: umbral = max(piso, k * ruido propio).
// Si la ventana tiene menos de cfg.MinBaselineDays días de datos, usa fleetCV
// como respaldo (Origin=Fallback).
func ComputeThresholds(readings []Reading, baselineFrom, baselineTo time.Time, fleetCV float64, cfg Config) MeterThresholds {
	cv, days := dailyCV(readings, baselineFrom, baselineTo)
	origin := Calculated
	if days < cfg.MinBaselineDays {
		cv = fleetCV
		origin = Fallback
	}

	variationPct := maxFloat(cfg.MinVariationPctFloor, cfg.VariationPctK*cv*100)
	electricalPct := maxFloat(cfg.ElectricalPctFloor, cfg.ElectricalPctK*cv*100)

	return MeterThresholds{
		VariationPct:  variationPct,
		OutlierZ:      cfg.OutlierZThreshold,
		ElectricalPct: electricalPct,
		Origin:        origin,
	}
}
