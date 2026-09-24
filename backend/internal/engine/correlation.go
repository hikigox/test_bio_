package engine

// variableChangedPctThreshold es el umbral fijo (%) a partir del cual una
// variable eléctrica se marca como "changed" en la vista de Investigación.
// Es informativo: no decide la clasificación (eso lo hace signals.go).
const variableChangedPctThreshold = 10.0

// CorrelateVariables compara V/I/PF antes y después del punto de cambio (02 §3)
// o, si no hay punto de cambio, entre la ventana baseline y el resto del período.
func CorrelateVariables(readings []Reading, profile HourlyProfile, cp ChangePoint) []VariableDelta {
	splitAt := profile.WindowTo
	if cp.Found {
		splitAt = cp.At
	}

	var beforeV, beforeI, beforePF, afterV, afterI, afterPF []float64
	for _, r := range readings {
		if r.Timestamp.Before(splitAt) {
			beforeV = append(beforeV, r.VoltageV)
			beforeI = append(beforeI, r.CurrentA)
			beforePF = append(beforePF, r.PowerFactor)
		} else {
			afterV = append(afterV, r.VoltageV)
			afterI = append(afterI, r.CurrentA)
			afterPF = append(afterPF, r.PowerFactor)
		}
	}

	return []VariableDelta{
		buildDelta("voltage_v", beforeV, afterV),
		buildDelta("current_a", beforeI, afterI),
		buildDelta("power_factor", beforePF, afterPF),
	}
}

// buildDelta calcula el delta porcentual entre las medianas de ambos lados del
// corte. Si alguno de los lados está vacío no hay comparación posible y el
// delta se devuelve neutro: tomar Median(nil)=0 como valor real produciría un
// -100% espurio marcado como "changed" en medidores cuya serie completa cabe
// en la ventana baseline.
func buildDelta(name string, before, after []float64) VariableDelta {
	baseline, actual := Median(before), Median(after)
	if len(before) == 0 || len(after) == 0 {
		return VariableDelta{Variable: name, Baseline: baseline, Actual: actual}
	}
	deltaPct := 0.0
	if baseline != 0 {
		deltaPct = (actual - baseline) / baseline * 100
	}
	return VariableDelta{
		Variable: name,
		Baseline: baseline,
		Actual:   actual,
		DeltaPct: deltaPct,
		Changed:  abs(deltaPct) > variableChangedPctThreshold,
	}
}
