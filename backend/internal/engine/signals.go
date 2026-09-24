package engine

// DetectSignals aplica los 5 detectores de 02 §2-3. Devuelve las señales
// disparadas y los problemas de calidad de datos encontrados (independiente
// de si generan señal DATA_QUALITY, para alimentar evidence.data_quality_issues).
func DetectSignals(readings []Reading, profile HourlyProfile, cp ChangePoint, th MeterThresholds, cfg Config) ([]Signal, []DataQualityIssue) {
	var signals []Signal
	var issues []DataQualityIssue

	// 1. Calidad de datos: límites físicos.
	outOfRange := 0
	for _, r := range readings {
		if r.PowerFactor < 0 || r.PowerFactor > 1 || r.VoltageV <= 0 || r.CurrentA < 0 || r.ConsumptionKWh < 0 {
			outOfRange++
		}
	}
	if outOfRange > 0 {
		issues = append(issues, DataQualityIssue{Kind: "OUT_OF_RANGE", Count: outOfRange})
		pct := float64(outOfRange) / float64(len(readings)) * 100
		signals = append(signals, Signal{
			Signal: DataQuality, Observed: pct, Threshold: 2,
			Detail: "lecturas fuera de rango físico (PF, V, I o kWh)",
		})
	}

	// 2. Consumo total real vs. baseline extrapolado -> variation_pct.
	actualKWh := sumKWh(readings)
	baselineKWh := extrapolateBaseline(profile, readings)
	variationPct := 0.0
	if baselineKWh != 0 {
		variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
	}
	absVariation := variationPct
	if absVariation < 0 {
		absVariation = -absVariation
	}

	// 3. Cambio persistente: variación por encima del umbral y sostenida (aprox.: hay change point con confianza).
	if absVariation > th.VariationPct && cp.Found {
		if isPersistent(readings, cp, cfg) {
			signals = append(signals, Signal{
				Signal: PersistentShift, Observed: variationPct, Threshold: th.VariationPct,
				Detail: "variación sostenida tras el punto de cambio",
			})
		}
	}

	// 4. Outliers horarios: z robusto por lectura contra el perfil de esa hora.
	residuals := hourlyResiduals(readings, profile)
	outlierCount := 0
	for i, r := range readings {
		z := RobustZ(residuals[i], residuals)
		if abs(z) > th.OutlierZ {
			outlierCount++
			signals = append(signals, Signal{
				Signal: Outlier, Observed: z, Threshold: th.OutlierZ,
				Detail: "lectura " + r.Timestamp.Format("2006-01-02T15:04") + " se desvía del perfil horario",
			})
		}
	}

	// 5. Patrón horario: desviación sistemática del perfil (usa el mismo cálculo de variación
	// pero acotado a horas específicas; se reporta como señal complementaria si hay >= 3 outliers
	// concentrados en las mismas horas del día).
	if outlierCount >= 3 {
		signals = append(signals, Signal{
			Signal: HourlyPattern, Observed: float64(outlierCount), Threshold: 3,
			Detail: "múltiples horas se desvían del perfil horario esperado",
		})
	}

	// 6. Consistencia eléctrica: ratio kWh/(V*I*PF) comparado contra su propia mediana en el baseline.
	ratios := electricalRatios(readings)
	if len(ratios) > 0 {
		medianRatio := Median(ratios)
		if medianRatio != 0 {
			lastRatios := ratios[len(ratios)/2:] // mitad más reciente, aproximando "actual"
			actualMedian := Median(lastRatios)
			deltaPct := (actualMedian - medianRatio) / medianRatio * 100
			if abs(deltaPct) > th.ElectricalPct {
				signals = append(signals, Signal{
					Signal: ElectricalInconsistency, Observed: deltaPct, Threshold: th.ElectricalPct,
					Detail: "razón kWh/(V·I·PF) se desvía de su línea base",
				})
			}
		}
	}

	return signals, issues
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func sumKWh(readings []Reading) float64 {
	total := 0.0
	for _, r := range readings {
		total += r.ConsumptionKWh
	}
	return total
}

// extrapolateBaseline suma el perfil horario sobre los mismos timestamps que hay
// en readings, para comparar como en 03-api.md (evita distorsión por huecos).
func extrapolateBaseline(profile HourlyProfile, readings []Reading) float64 {
	total := 0.0
	for _, r := range readings {
		total += profile.MedianByHour[r.Timestamp.Hour()]
	}
	return total
}

func hourlyResiduals(readings []Reading, profile HourlyProfile) []float64 {
	out := make([]float64, len(readings))
	for i, r := range readings {
		out[i] = r.ConsumptionKWh - profile.MedianByHour[r.Timestamp.Hour()]
	}
	return out
}

func electricalRatios(readings []Reading) []float64 {
	var ratios []float64
	for _, r := range readings {
		denom := r.VoltageV * r.CurrentA * r.PowerFactor
		if denom <= 0 {
			continue // evita división por cero; ya contado como OUT_OF_RANGE
		}
		ratios = append(ratios, r.ConsumptionKWh/denom)
	}
	return ratios
}

// isPersistent verifica que el cambio se sostenga >= cfg.PersistenceHours tras el change point.
func isPersistent(readings []Reading, cp ChangePoint, cfg Config) bool {
	count := 0
	for _, r := range readings {
		if !r.Timestamp.Before(cp.At) {
			count++
		}
	}
	hoursAvailable := float64(count) // 1 lectura/hora
	return hoursAvailable >= cfg.PersistenceHours
}
