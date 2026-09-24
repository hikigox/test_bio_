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

// electricalCV calcula el coeficiente de variación robusto (MADNorm/mediana) de
// la razón r = kWh/(V·I·PF) dentro de la ventana baseline. Si no hay razones
// válidas o la mediana es 0, retorna 0 y decide el piso.
func electricalCV(readings []Reading, from, to time.Time) float64 {
	var ratios []float64
	for _, r := range readings {
		if r.Timestamp.Before(from) || !r.Timestamp.Before(to) {
			continue
		}
		if v, ok := electricalRatio(r); ok {
			ratios = append(ratios, v)
		}
	}
	median := Median(ratios)
	if median == 0 {
		return 0
	}
	return MADNorm(ratios) / median
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

	// 02 §10: T_elec,i = max(15 %, 3 × cv(r)_i × 100), donde cv(r) es el ruido
	// propio de la RAZÓN r = kWh/(V·I·PF), no el del consumo diario. Son cosas
	// distintas: un medidor puede tener consumo diario muy estable y una razón
	// eléctrica ruidosa (o al revés), y usar el cv del consumo para ambos
	// umbrales hace que la consistencia eléctrica dispare por el ruido normal
	// de la razón en medidores perfectamente sanos.
	elecCV := electricalCV(readings, baselineFrom, baselineTo)
	if days < cfg.MinBaselineDays {
		elecCV = fleetCV
	}
	electricalPct := maxFloat(cfg.ElectricalPctFloor, cfg.ElectricalPctK*elecCV*100)

	return MeterThresholds{
		VariationPct:  variationPct,
		OutlierZ:      cfg.OutlierZThreshold,
		ElectricalPct: electricalPct,
		Origin:        origin,
	}
}
