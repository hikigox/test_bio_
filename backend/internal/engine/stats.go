// Package engine contains the pure-computation anomaly detection engine:
// robust statistics, baselines, thresholds, signal detectors, and
// classification. It has no database or network dependencies.
package engine

import "sort"

// Median calcula la mediana. No modifica el slice de entrada.
func Median(values []float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := n / 2
	if n%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// MAD (Median Absolute Deviation) sin normalizar.
func MAD(values []float64) float64 {
	m := Median(values)
	deviations := make([]float64, len(values))
	for i, v := range values {
		d := v - m
		if d < 0 {
			d = -d
		}
		deviations[i] = d
	}
	return Median(deviations)
}

// MADNorm = 1.4826 * MAD, estimador robusto de la desviación estándar (02 §10).
func MADNorm(values []float64) float64 {
	return 1.4826 * MAD(values)
}

// RobustZ = 0.6745 * (value - median(sample)) / MAD(sample) (02 §10, fórmula de Iglewicz-Hoaglin).
// Si MAD(sample) == 0 (muestra constante), usa un piso mínimo para evitar división por cero
// sin perder la señal de que value se desvía de una muestra sin ruido.
func RobustZ(value float64, sample []float64) float64 {
	m := Median(sample)
	mad := MAD(sample)
	if mad == 0 {
		if value == m {
			return 0
		}
		// piso: 1% del valor absoluto de la mediana, o 1e-6 si la mediana también es 0.
		floor := m * 0.01
		if floor <= 0 {
			floor = 1e-6
		}
		mad = floor
	}
	return 0.6745 * (value - m) / mad
}
