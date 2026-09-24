package engine

import (
	"math"
	"time"
)

// BuildHourlyProfile calcula la mediana de consumo por hora del día (0-23)
// sobre la ventana [from, to), y las medianas de V/I/PF en esa misma ventana.
func BuildHourlyProfile(readings []Reading, from, to time.Time) HourlyProfile {
	byHour := make([][]float64, 24)
	var voltages, currents, pfs []float64
	for _, r := range readings {
		if r.Timestamp.Before(from) || !r.Timestamp.Before(to) {
			continue
		}
		h := r.Timestamp.Hour()
		byHour[h] = append(byHour[h], r.ConsumptionKWh)
		voltages = append(voltages, r.VoltageV)
		currents = append(currents, r.CurrentA)
		pfs = append(pfs, r.PowerFactor)
	}
	var profile HourlyProfile
	profile.WindowFrom, profile.WindowTo = from, to
	for h := 0; h < 24; h++ {
		profile.MedianByHour[h] = Median(byHour[h])
	}
	profile.VoltageV = Median(voltages)
	profile.CurrentA = Median(currents)
	profile.PowerFactor = Median(pfs)
	return profile
}

// dailyTotals agrupa consumo por día calendario (UTC) para el CUSUM.
func dailyTotals(readings []Reading) (days []time.Time, totals []float64) {
	sums := map[string]float64{}
	order := map[string]time.Time{}
	for _, r := range readings {
		key := r.Timestamp.Format("2006-01-02")
		sums[key] += r.ConsumptionKWh
		if _, ok := order[key]; !ok {
			order[key] = time.Date(r.Timestamp.Year(), r.Timestamp.Month(), r.Timestamp.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	for key, day := range order {
		days = append(days, day)
		totals = append(totals, sums[key])
	}
	// ordenar por fecha
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j].Before(days[j-1]); j-- {
			days[j], days[j-1] = days[j-1], days[j]
			totals[j], totals[j-1] = totals[j-1], totals[j]
		}
	}
	return days, totals
}

// DetectChangePoint aplica CUSUM sobre los totales diarios de consumo (02 §1, §10).
// k = 0.5*sigma, h = 5*sigma. sigma se estima con MADNorm de los residuos de una
// ventana de referencia (los primeros cfg.DefaultBaselineDays días, o menos si no
// hay suficientes datos) respecto a su propia mediana.
//
// Nota de implementación: estimar sigma con la mediana/MAD de *toda* la serie
// (incluyendo el período posterior al cambio) es autodestructivo cuando hay un
// salto de nivel real: el salto infla el MAD global, lo que infla sigma y por
// tanto el umbral h = 5*sigma hasta volverlo inalcanzable para saltos de
// duración corta. Por eso sigma se calcula solo sobre una ventana de
// referencia inicial, que se asume representativa del comportamiento normal
// del medidor antes de cualquier cambio.
func DetectChangePoint(readings []Reading, cfg Config) ChangePoint {
	days, totals := dailyTotals(readings)
	if len(days) < 2*cfg.MinBaselineDays {
		return ChangePoint{Found: false}
	}

	refDays := cfg.DefaultBaselineDays
	if refDays < cfg.MinBaselineDays {
		refDays = cfg.MinBaselineDays
	}
	if refDays > len(totals)/2 {
		refDays = len(totals) / 2
	}
	if refDays < 1 {
		return ChangePoint{Found: false}
	}

	reference := totals[:refDays]
	median := Median(reference)
	refResiduals := make([]float64, len(reference))
	for i, v := range reference {
		refResiduals[i] = v - median
	}
	sigma := MADNorm(refResiduals)
	if sigma == 0 {
		// Ventana de referencia sin varianza (datos deterministas o medidor
		// perfectamente estable): usamos un piso mínimo relativo a la mediana
		// para evitar un umbral h=0 que dispararía en el primer residuo no nulo.
		floor := math.Abs(median) * 0.01
		if floor <= 0 {
			floor = 1e-6
		}
		sigma = 1.4826 * floor
	}
	k := 0.5 * sigma
	h := 5 * sigma

	var cusumHigh, cusumLow float64
	changeIdx := -1
	for i, v := range totals {
		res := v - median
		cusumHigh = maxFloat(0, cusumHigh+res-k)
		cusumLow = minFloat(0, cusumLow+res+k)
		if cusumHigh > h || -cusumLow > h {
			changeIdx = i
			break
		}
	}
	if changeIdx == -1 {
		return ChangePoint{Found: false}
	}
	return ChangePoint{Found: true, At: days[changeIdx], Sigma: sigma}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
