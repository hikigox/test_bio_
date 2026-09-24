package engine

import "strconv"

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

	// 2. Consumo real vs. baseline extrapolado sobre el mismo tramo -> variation_pct.
	_, _, variationPct := ComputeVariation(readings, profile, cp)
	absVariation := abs(variationPct)

	// 3. Cambio persistente: variación por encima del umbral y sostenida (aprox.: hay change point con confianza).
	if absVariation > th.VariationPct && cp.Found {
		if isPersistent(readings, profile, cp, th, cfg) {
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

	// 6. Consistencia eléctrica (02 §10): r_t = kWh_t/(V·I·PF) por lectura,
	//    comparado contra la MEDIANA de r en la VENTANA BASELINE.
	if sig, ok := electricalInconsistency(readings, profile, th); ok {
		signals = append(signals, sig)
	}

	return signals, issues
}

// dataQualityRatePct es el "> 2 %" de la fila "Calidad de datos" de la tabla de
// calibración de 02 §10: la fracción de lecturas defectuosas a partir de la cual
// el problema deja de ser anecdótico y se reporta como señal. Se reutiliza aquí
// para la consistencia eléctrica, que es precisamente una comprobación de
// calidad de datos por lectura.
const dataQualityRatePct = 2.0

// electricalInconsistency implementa 02 §10 "Consistencia eléctrica":
//
//	r_t = kWh_t / (V_t · I_t · PF_t)
//	ref = mediana de r_t sobre la VENTANA BASELINE [WindowFrom, WindowTo)
//	desviación_t = |r_t - ref| / ref * 100
//	dispara si  #{t : desviación_t > T_elec,i} / #lecturas * 100 > dataQualityRatePct
//
// Se cuenta la FRACCIÓN de lecturas desviadas en vez de comparar dos medianas
// agregadas: una inconsistencia intermitente (el caso real de 02 §2, "saltos
// eléctricos anómalos") queda diluida en cualquier mediana calculada sobre una
// ventana mayoritariamente normal, que es justo lo que debe detectar.
//
// Observed es la MEDIANA de las desviaciones que superaron el umbral, no la
// fracción ni el máximo: es el tamaño típico de la inconsistencia, está en la
// misma unidad (%) que Threshold — de modo que dataQualitySeverity
// (|Observed| >= 2·Threshold => High) y dataQualityMagnitude siguen leyéndose
// sin cambios — y, al ser una mediana, no la fija una sola lectura extrema.
func electricalInconsistency(readings []Reading, profile HourlyProfile, th MeterThresholds) (Signal, bool) {
	var baselineRatios []float64
	for _, r := range readings {
		if r.Timestamp.Before(profile.WindowFrom) || !r.Timestamp.Before(profile.WindowTo) {
			continue
		}
		if v, ok := electricalRatio(r); ok {
			baselineRatios = append(baselineRatios, v)
		}
	}
	ref := Median(baselineRatios)
	if ref == 0 {
		return Signal{}, false
	}

	total, deviating := 0, 0
	var deviations []float64
	for _, r := range readings {
		v, ok := electricalRatio(r)
		if !ok {
			continue // ya contado como OUT_OF_RANGE
		}
		total++
		d := abs(v-ref) / ref * 100
		if d > th.ElectricalPct {
			deviating++
			deviations = append(deviations, d)
		}
	}
	if total == 0 {
		return Signal{}, false
	}
	ratePct := float64(deviating) / float64(total) * 100
	if ratePct <= dataQualityRatePct {
		return Signal{}, false
	}
	return Signal{
		Signal:    ElectricalInconsistency,
		Observed:  Median(deviations),
		Threshold: th.ElectricalPct,
		Detail: "la razón kWh/(V·I·PF) se desvía de su mediana baseline en " +
			formatPct(ratePct) + " de las lecturas (umbral " + formatPct(dataQualityRatePct) + ")",
	}, true
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

// electricalRatio devuelve r_t = kWh_t/(V·I·PF) de una lectura. ok=false si el
// denominador no es positivo (división por cero; ya contado como OUT_OF_RANGE).
func electricalRatio(r Reading) (float64, bool) {
	denom := r.VoltageV * r.CurrentA * r.PowerFactor
	if denom <= 0 {
		return 0, false
	}
	return r.ConsumptionKWh / denom, true
}

// ChangeWindowReadings devuelve el tramo de lecturas sobre el que se mide la
// variación: [cp.At, cp.EndsAt) si hubo punto de cambio, o todas las lecturas
// si no lo hubo.
//
// 02 §1 define variation_pct = (actual - baseline)/baseline. Medir "actual"
// sobre el período completo cuando SÍ hay punto de cambio mezcla los días
// previos —que por definición no cambiaron— con los posteriores y divide la
// magnitud real entre dos: un salto de +104 % que ocurre a mitad del período se
// reportaría como ~+19 %. Simétricamente, medir hasta el final de la serie
// diluye un cambio transitorio (una parada de 12 h queda en −5 %).
func ChangeWindowReadings(readings []Reading, cp ChangePoint) []Reading {
	if !cp.Found {
		return readings
	}
	out := make([]Reading, 0, len(readings))
	for _, r := range readings {
		if r.Timestamp.Before(cp.At) {
			continue
		}
		if !cp.EndsAt.IsZero() && !r.Timestamp.Before(cp.EndsAt) {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return readings
	}
	return out
}

// ComputeVariation calcula (actual, baseline, variation_pct) de 02 §1 sobre el
// tramo del cambio (o sobre todo el período si no hay punto de cambio).
func ComputeVariation(readings []Reading, profile HourlyProfile, cp ChangePoint) (actualKWh, baselineKWh, variationPct float64) {
	window := ChangeWindowReadings(readings, cp)
	actualKWh = sumKWh(window)
	baselineKWh = extrapolateBaseline(profile, window)
	if baselineKWh != 0 {
		variationPct = (actualKWh - baselineKWh) / baselineKWh * 100
	}
	return actualKWh, baselineKWh, variationPct
}

// isPersistent verifica que el cambio de NIVEL se sostenga tras el punto de
// cambio, no solo que existan lecturas después de él (02 §10, "Persistencia:
// el cambio se mantiene >= 48 h continuas").
//
// Criterio: se parten las lecturas posteriores al punto de cambio en bloques
// consecutivos y disjuntos de cfg.PersistenceHours lecturas (1 lectura/hora).
// Debe haber al menos un bloque completo, y en CADA bloque completo la
// variación del bloque frente al baseline extrapolado sobre ese mismo bloque
// debe (a) tener el mismo signo que la variación global posterior al cambio y
// (b) alcanzar al menos persistenceMagnitudeFraction del umbral T_var,i.
//
// El punto (b) con una fracción del umbral (y no el umbral entero) tolera que
// el nivel fluctúe alrededor del salto sin exigir que cada ventana por separado
// vuelva a superar el umbral completo; el punto (a) es lo que descarta un punto
// de cambio espurio, tras el cual la variación es ~0 y cambia de signo.
func isPersistent(readings []Reading, profile HourlyProfile, cp ChangePoint, th MeterThresholds, cfg Config) bool {
	if !cp.Found {
		return false
	}
	window := ChangeWindowReadings(readings, cp)
	block := int(cfg.PersistenceHours) // 1 lectura/hora
	if block <= 0 || len(window) < block {
		return false
	}

	_, _, overall := ComputeVariation(readings, profile, cp)
	if overall == 0 {
		return false
	}
	minMagnitude := persistenceMagnitudeFraction * th.VariationPct

	for start := 0; start+block <= len(window); start += block {
		chunk := window[start : start+block]
		actual := sumKWh(chunk)
		baseline := extrapolateBaseline(profile, chunk)
		if baseline == 0 {
			return false
		}
		v := (actual - baseline) / baseline * 100
		if (v > 0) != (overall > 0) {
			return false
		}
		if abs(v) < minMagnitude {
			return false
		}
	}
	return true
}

// persistenceMagnitudeFraction: fracción del umbral T_var,i que cada bloque de
// PersistenceHours debe mantener para considerar el cambio sostenido. 0.5 es
// una decisión de diseño: exigir el umbral completo en cada bloque haría el
// criterio más estricto que la propia detección de cambio, y exigir solo el
// signo lo dejaría casi sin fuerza.
const persistenceMagnitudeFraction = 0.5

// formatPct formatea un porcentaje con un decimal para los textos de Detail.
func formatPct(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64) + "%"
}
