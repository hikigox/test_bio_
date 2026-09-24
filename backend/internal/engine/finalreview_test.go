package engine

import (
	"math"
	"testing"
	"time"
)

// Fixtures sintéticos para la ronda de correcciones de la revisión final
// (C1, C2, C3, I3). Ningún test lee el dataset real: spec 05 exige fixtures
// sintéticos en las pruebas unitarias.

// noisyHourly construye lecturas horarias con un perfil diario y un jitter
// determinista de ±3 %, para que los totales diarios tengan un ruido realista
// (con datos perfectamente deterministas sigma degenera al piso mínimo y
// cualquier variación ínfima cruza h = 5σ).
func noisyHourly(start time.Time, hours int, level func(h int) float64) []Reading {
	return hourlyReadings(start, hours, func(h int) float64 {
		return level(h) * (1 + 0.03*math.Sin(float64(h)*1.7))
	})
}

// --- C1: consistencia eléctrica (02 §10) ---

// electricalFixture: consumo perfectamente estable y razón r = kWh/(V·I·PF)
// estable en el baseline, con deviations lecturas posteriores en las que V cae
// y por tanto r salta. Las desviaciones se reparten (intermitentes), que es el
// caso que describe el dataset real ("abnormal electrical jumps").
func electricalFixture(start time.Time, hours, deviations int) []Reading {
	readings := hourlyReadings(start, hours, func(h int) float64 { return 10 })
	// Se colocan en la segunda mitad, espaciadas, empezando lejos del baseline.
	step := (hours / 2) / maxInt(deviations, 1)
	for i := 0; i < deviations; i++ {
		idx := hours/2 + i*step
		if idx >= hours {
			break
		}
		readings[idx].VoltageV = 140 // 220 -> 140: r sube ~+57 %
	}
	return readings
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestElectricalInconsistencyFiresOnIntermittentDeviations es el test de C1.
// El detector anterior comparaba dos medianas agregadas (mitad reciente vs.
// serie completa), que es robusta justo a lo que debe detectar: 8 % de lecturas
// desviadas no mueve una mediana calculada sobre 168 lecturas mayoritariamente
// normales, así que la señal nunca disparaba.
func TestElectricalInconsistencyFiresOnIntermittentDeviations(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := electricalFixture(start, 14*24, 28) // 28/336 = 8.3 % de lecturas

	profile := BuildHourlyProfile(readings, start, start.Add(7*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())

	// Control: la comparación de medianas agregadas que había antes no lo ve.
	var ratios []float64
	for _, r := range readings {
		if v, ok := electricalRatio(r); ok {
			ratios = append(ratios, v)
		}
	}
	oldDelta := (Median(ratios[len(ratios)/2:]) - Median(ratios)) / Median(ratios) * 100
	if abs(oldDelta) > th.ElectricalPct {
		t.Fatalf("fixture inválido: la comparación de medianas agregadas ya lo detectaba (%.2f%%)", oldDelta)
	}

	sig, ok := electricalInconsistency(readings, profile, th)
	if !ok {
		t.Fatal("expected ELECTRICAL_INCONSISTENCY for intermittent ratio jumps")
	}
	if sig.Observed <= th.ElectricalPct {
		t.Fatalf("Observed must be the typical deviation magnitude above the threshold, got %.2f (thr %.2f)",
			sig.Observed, th.ElectricalPct)
	}
}

// Por debajo de la tasa del 2 % (fila "Calidad de datos" de 02 §10) una
// desviación puntual no es una inconsistencia del medidor.
func TestElectricalInconsistencyIgnoresIsolatedDeviations(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := electricalFixture(start, 14*24, 3) // 3/336 = 0.9 %, por debajo del 2 %

	profile := BuildHourlyProfile(readings, start, start.Add(7*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())

	if sig, ok := electricalInconsistency(readings, profile, th); ok {
		t.Fatalf("0.9%% of deviating readings is below the 2%% rate, must not fire: %+v", sig)
	}
}

// El umbral T_elec,i se calcula con el ruido propio de la RAZÓN (02 §10), no
// con el del consumo: un medidor cuya razón es naturalmente ruidosa no debe
// disparar por su propio ruido.
func TestElectricalThresholdAdaptsToRatioNoise(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := start.Add(7 * 24 * time.Hour)

	steady := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	noisy := hourlyReadings(start, 14*24, func(h int) float64 { return 10 })
	for i := range noisy {
		// El consumo diario sigue siendo idéntico; solo la razón es ruidosa.
		noisy[i].VoltageV = 220 * (1 + 0.12*math.Sin(float64(i)*0.9))
	}

	thSteady := ComputeThresholds(steady, start, to, 0.1, DefaultConfig())
	thNoisy := ComputeThresholds(noisy, start, to, 0.1, DefaultConfig())

	if thSteady.ElectricalPct != DefaultConfig().ElectricalPctFloor {
		t.Fatalf("a steady ratio must sit on the 15%% floor, got %v", thSteady.ElectricalPct)
	}
	if thNoisy.ElectricalPct <= thSteady.ElectricalPct {
		t.Fatalf("a noisy ratio must raise T_elec,i above the floor: noisy=%v steady=%v",
			thNoisy.ElectricalPct, thSteady.ElectricalPct)
	}
	if thNoisy.VariationPct != thSteady.VariationPct {
		t.Fatalf("consumption threshold must not move with ratio noise: %v vs %v",
			thNoisy.VariationPct, thSteady.VariationPct)
	}
}

// --- C2: "cambio significativo" = variation_pct > T_var,i ---

// Una sola hora anómala, con variación de consumo ~0, no es un "cambio
// significativo" (02 §10) y no puede enrutar el medidor a las ramas 2-4.
func TestClassifyIsolatedOutlierIsNotAConsumptionChange(t *testing.T) {
	signals := []Signal{{Signal: Outlier, Observed: 3.65, Threshold: 3.5}}
	typ, sev, _, _, path := Classify(signals, nil, nil, nil, 0.6, 15)
	if typ != "" || sev != "" {
		t.Fatalf("one outlier hour with ~0%% variation must not be an anomaly, got %v/%v (path=%v)", typ, sev, path)
	}
}

// Idem para HOURLY_PATTERN: describe horas fuera del perfil, no un cambio de nivel.
func TestClassifyHourlyPatternAloneIsNotAConsumptionChange(t *testing.T) {
	signals := []Signal{
		{Signal: Outlier, Observed: 4.1, Threshold: 3.5},
		{Signal: HourlyPattern, Observed: 5, Threshold: 3},
	}
	typ, _, _, _, _ := Classify(signals, nil, nil, nil, -0.1, 15)
	if typ != "" {
		t.Fatalf("hourly-pattern signals with ~0%% variation must not be an anomaly, got %v", typ)
	}
}

// Las señales aisladas siguen registrándose en la evidencia aunque no clasifiquen.
func TestRunKeepsIsolatedOutlierInSignalsWithoutAnomaly(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := noisyHourly(start, 14*24, func(h int) float64 { return 10 + float64(h%24)*0.1 })
	readings[200].ConsumptionKWh *= 2.5 // una única hora fuera del perfil

	result := Run("M-TEST-OUTLIER", readings, nil, 0.1, DefaultConfig())
	if result.HasAnomaly {
		t.Fatalf("a single anomalous hour must not become an anomaly, got %v/%v var=%v",
			result.Type, result.Severity, result.VariationPct)
	}
	found := false
	for _, s := range result.Signals {
		if s.Signal == Outlier {
			found = true
		}
	}
	if !found {
		t.Fatalf("the outlier signal must remain in the evidence for audit, got %+v", result.Signals)
	}
}

// --- C3: CUSUM y el tramo de variation_pct ---

// El CUSUM no debe buscar el punto de cambio dentro de la ventana con la que
// estimó sigma: un bajón ordinario dentro de esa ventana cruzaba h = 5σ y se
// reportaba como cambio, días antes del salto real.
func TestDetectChangePointDoesNotScanItsOwnReferenceWindow(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(10 * 24 * time.Hour)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		day := h / 24
		base := 10.0
		if day == 5 || day == 6 {
			base = 9.2 // bajón ordinario DENTRO de la ventana de referencia
		}
		if day >= 10 {
			base = 21 // salto real
		}
		return base
	})

	cp := DetectChangePoint(readings, DefaultConfig())
	if !cp.Found {
		t.Fatal("expected the real change point to be found")
	}
	if cp.At.Before(shiftAt) {
		t.Fatalf("change point %v falls before the real shift %v: the reference window was scanned", cp.At, shiftAt)
	}
}

// variation_pct se mide sobre el tramo del cambio, no sobre todo el período:
// mezclarlo con los días previos divide la magnitud real aproximadamente entre dos.
func TestRunVariationIsMeasuredOverTheChangeSpan(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := hourlyReadings(start, 14*24, func(h int) float64 {
		if h >= 7*24 {
			return 20.37 // +103.7 % sobre 10
		}
		return 10
	})

	result := Run("M-TEST-SPAN", readings, nil, 0.1, DefaultConfig())
	if !result.ChangePoint.Found {
		t.Fatal("expected a change point")
	}
	if result.VariationPct < 95 || result.VariationPct > 112 {
		t.Fatalf("variation must reflect the post-change level (~+103.7%%), got %v", result.VariationPct)
	}
	if result.Confidence < 0.9 {
		t.Fatalf("a ~+104%% unexplained shift must reach confidence >= 0.9, got %v", result.Confidence)
	}
	// Y las cifras publicadas deben cuadrar con la variación reportada.
	want := (result.ActualKWh - result.BaselineKWh) / result.BaselineKWh * 100
	if math.Abs(want-result.VariationPct) > 0.01 {
		t.Fatalf("ActualKWh/BaselineKWh (%v) must match VariationPct (%v)", want, result.VariationPct)
	}
}

// Un cambio transitorio (parada de 12 h) se mide sobre su propio tramo: medirlo
// contra los días que ya volvieron a la normalidad lo diluye hasta hacerlo
// parecer insignificante.
func TestRunVariationOfATransientChangeIsNotDiluted(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	outageDay := 7
	readings := noisyHourly(start, 14*24, func(h int) float64 {
		if h/24 == outageDay && h%24 < 12 {
			return 1.5 // parada de 12 h
		}
		return 10
	})

	result := Run("M-TEST-TRANSIENT", readings, nil, 0.1, DefaultConfig())
	if !result.ChangePoint.Found {
		t.Fatal("expected a change point for the outage")
	}
	if result.VariationPct > -15 {
		t.Fatalf("a 12h outage must still read as a significant drop, got %v%%", result.VariationPct)
	}
}

// --- I3: persistencia ---

// Un punto de cambio espurio (sin cambio de nivel detrás) no es persistente por
// el mero hecho de que existan lecturas después de él.
func TestIsPersistentRejectsSpuriousChangePointWithoutLevelChange(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	readings := noisyHourly(start, 14*24, func(h int) float64 { return 10 })
	profile := BuildHourlyProfile(readings, start, start.Add(7*24*time.Hour))
	th := ComputeThresholds(readings, start, start.Add(7*24*time.Hour), 0.1, DefaultConfig())

	// "Punto de cambio" inventado en el día 7, con 7 días de datos detrás: la
	// comprobación anterior (>= 48 lecturas después) lo daba por persistente.
	spurious := ChangePoint{Found: true, At: start.Add(7 * 24 * time.Hour), EndsAt: start.Add(14 * 24 * time.Hour)}
	if isPersistent(readings, profile, spurious, th, DefaultConfig()) {
		t.Fatal("a change point with no sustained level change must not count as persistent")
	}
}

// Un cambio real y sostenido sí es persistente.
func TestIsPersistentAcceptsSustainedLevelChange(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := noisyHourly(start, 14*24, func(h int) float64 {
		if h >= 7*24 {
			return 20.5
		}
		return 10
	})
	profile := BuildHourlyProfile(readings, start, shiftAt)
	th := ComputeThresholds(readings, start, shiftAt, 0.1, DefaultConfig())

	cp := ChangePoint{Found: true, At: shiftAt, EndsAt: start.Add(14 * 24 * time.Hour)}
	if !isPersistent(readings, profile, cp, th, DefaultConfig()) {
		t.Fatal("a sustained +105% shift over 7 days must count as persistent")
	}
}

// Un cambio que dura menos de PersistenceHours no es persistente.
func TestIsPersistentRejectsTooShortChange(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	shiftAt := start.Add(7 * 24 * time.Hour)
	readings := noisyHourly(start, 14*24, func(h int) float64 {
		if h >= 7*24 && h < 7*24+12 {
			return 20.5
		}
		return 10
	})
	profile := BuildHourlyProfile(readings, start, shiftAt)
	th := ComputeThresholds(readings, start, shiftAt, 0.1, DefaultConfig())

	cp := ChangePoint{Found: true, At: shiftAt, EndsAt: shiftAt.Add(12 * time.Hour)}
	if isPersistent(readings, profile, cp, th, DefaultConfig()) {
		t.Fatal("a 12h change is shorter than the 48h persistence window")
	}
}
