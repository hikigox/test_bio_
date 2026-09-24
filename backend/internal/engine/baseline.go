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

	// La acumulación CUSUM empieza *después* de la ventana de referencia. La
	// ventana de referencia es, por definición, el período que se asume previo
	// a cualquier cambio (de ahí que sirva para estimar sigma); buscar dentro
	// de ella "el punto de cambio" es incoherente y, en la práctica, hace que
	// una bajada ordinaria del propio ruido con el que se calibró sigma cruce
	// h = 5σ y se reporte como cambio días antes del salto real.
	var cusumHigh, cusumLow float64
	changeIdx := -1
	for i := refDays; i < len(totals); i++ {
		res := totals[i] - median
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

	// Extensión del cambio: días consecutivos, desde el día detectado, cuya
	// desviación respecto a la mediana de referencia mantiene el mismo signo y
	// supera la holgura k del propio CUSUM. Sin esto, un cambio transitorio
	// (una parada de 12 h) se mediría contra todo lo que viene detrás —días que
	// ya volvieron a la normalidad— y su magnitud quedaría diluida hasta
	// parecer insignificante, igual que le pasa a un salto real si se mide
	// contra los días previos.
	//
	// No se extiende hacia atrás: con h = 5σ y saltos de esta magnitud el CUSUM
	// dispara en el propio día del cambio, y retroceder mientras la desviación
	// supere k absorbe con facilidad días de ruido ordinario del baseline.
	start := changeIdx
	end := changeIdx // último día incluido
	delta := totals[changeIdx] - median
	if abs(delta) > k {
		for end+1 < len(totals) {
			next := totals[end+1] - median
			if (next > 0) != (delta > 0) || abs(next) <= k {
				break
			}
			end++
		}
	}

	at := days[start]
	// Refinamiento horario del inicio: el CUSUM trabaja sobre totales diarios,
	// así que solo localiza el día. Si dentro de ese día hay una hora a partir
	// de la cual el nivel salta en la misma dirección que el cambio, el cambio
	// empieza ahí, y arrastrar las horas normales previas vuelve a diluir la
	// magnitud. Solo se refina el inicio: el final se deja en la frontera del
	// día para no recortar un cambio que sigue vigente al terminar la serie.
	if refined, ok := refineStartHour(readings, days[start], median/24, totals[start]/24, delta > 0); ok {
		at = refined
	}
	endsAt := days[end].Add(24 * time.Hour)

	return ChangePoint{Found: true, At: at, EndsAt: endsAt, Sigma: sigma}
}

// refineStartHourFraction: fracción del salto de nivel por hora que debe
// alcanzar la diferencia de medianas de un corte horario para aceptarlo como
// inicio real del cambio dentro del día. 0.5 (decisión de diseño) exige que el
// corte explique al menos la mitad del salto; por debajo de eso lo más probable
// es que el cambio ya estuviera presente desde el comienzo del día y que el
// mejor corte sea solo el perfil horario normal del medidor.
const refineStartHourFraction = 0.5

// refineStartHour busca, dentro del día day, la hora de corte que maximiza la
// diferencia de medianas entre las lecturas previas y las posteriores, exigiendo
// que la dirección del salto coincida con la del cambio diario (increase) y que
// la diferencia alcance refineStartHourFraction del salto de nivel por hora
// (|nivelCambiado - nivelBaseline| / 24). Devuelve ok=false si ningún corte lo
// cumple, en cuyo caso el cambio se toma desde el inicio del día.
func refineStartHour(readings []Reading, day time.Time, baselinePerHour, changedPerHour float64, increase bool) (time.Time, bool) {
	dayEnd := day.Add(24 * time.Hour)
	var vals []float64
	var stamps []time.Time
	for _, r := range readings {
		if r.Timestamp.Before(day) || !r.Timestamp.Before(dayEnd) {
			continue
		}
		vals = append(vals, r.ConsumptionKWh)
		stamps = append(stamps, r.Timestamp)
	}
	if len(vals) < 4 {
		return time.Time{}, false
	}

	minGap := refineStartHourFraction * math.Abs(changedPerHour-baselinePerHour)
	bestGap, bestIdx := 0.0, -1
	for i := 1; i < len(vals); i++ {
		gap := Median(vals[i:]) - Median(vals[:i])
		if !increase {
			gap = -gap
		}
		if gap > bestGap {
			bestGap, bestIdx = gap, i
		}
	}
	if bestIdx < 0 || bestGap < minGap {
		return time.Time{}, false
	}
	return stamps[bestIdx], true
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
