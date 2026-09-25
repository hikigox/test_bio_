package engine

import (
	"fmt"
	"strings"
)

// variableLabel traduce el identificador interno de una variable (el mismo
// que usa VariableDelta.Variable y evidence.variables[].variable en la API)
// a su nombre en español para la explicación en lenguaje natural. Solo se usa
// aquí, en el texto: el identificador crudo se mantiene en evidence.variables
// porque el frontend ya lo traduce por su cuenta (lib/labels.ts) y otros
// consumidores de la API pueden depender de ese valor estable.
var variableLabels = map[string]string{
	"consumption_kwh": "consumo",
	"voltage_v":       "voltaje",
	"current_a":       "corriente",
	"power_factor":    "factor de potencia",
}

func variableLabel(v string) string {
	if label, ok := variableLabels[v]; ok {
		return label
	}
	return v
}

// BuildReason redacta la explicación por plantilla (02 §8), citando evidencia numérica real.
func BuildReason(t AnomalyType, variationPct float64, variables []VariableDelta) string {
	var changed []string
	for _, v := range variables {
		if v.Changed {
			changed = append(changed, fmt.Sprintf("%s %+.1f%%", variableLabel(v.Variable), v.DeltaPct))
		}
	}
	changedStr := "sin cambios eléctricos coherentes"
	if len(changed) > 0 {
		changedStr = strings.Join(changed, ", ")
	}

	// Las ramas 3/4 de 02 §5 no restringen el signo de la variación: una caída
	// inexplicada es tan REAL_ANOMALY como una subida. La dirección se redacta
	// a partir del signo y la cifra se imprime siempre en valor absoluto, para
	// no producir textos que se contradicen ("-0.1% por encima del baseline").
	dir := direction(variationPct)
	mag := abs(variationPct)

	switch t {
	case RealAnomaly:
		return fmt.Sprintf("Consumo %.1f%% %s del baseline sin evento conocido; %s.", mag, dir, changedStr)
	case TypeDataQuality:
		return fmt.Sprintf("Inconsistencia eléctrica o de calidad de datos detectada (consumo %.1f%% %s del baseline); %s.", mag, dir, changedStr)
	case ExplainableAnomaly:
		return fmt.Sprintf("Consumo %.1f%% %s del baseline, explicado por un evento operativo registrado; %s.", mag, dir, changedStr)
	case FalsePositive:
		return fmt.Sprintf("Variación de %.1f%% %s del baseline explicada por una parada programada; no se escala como anomalía real.", mag, dir)
	default:
		return ""
	}
}

// direction traduce el signo de la variación al texto de dirección. Una
// variación exactamente 0 se redacta como "por encima" por convención (no
// aparece en la práctica: sin variación no hay anomalía de consumo).
func direction(variationPct float64) string {
	if variationPct < 0 {
		return "por debajo"
	}
	return "por encima"
}

// BuildRecommendation retorna la acción recomendada por tipo (02 §8).
func BuildRecommendation(t AnomalyType) string {
	switch t {
	case RealAnomaly:
		return "Investigar medidor e instalación"
	case TypeDataQuality:
		return "Validar sensor/cableado"
	case ExplainableAnomaly:
		return "Validar operación"
	case FalsePositive:
		return "No escalar"
	default:
		return ""
	}
}
