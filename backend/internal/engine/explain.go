package engine

import (
	"fmt"
	"strings"
)

// BuildReason redacta la explicación por plantilla (02 §8), citando evidencia numérica real.
func BuildReason(t AnomalyType, variationPct float64, variables []VariableDelta) string {
	var changed []string
	for _, v := range variables {
		if v.Changed {
			changed = append(changed, fmt.Sprintf("%s %+.1f%%", v.Variable, v.DeltaPct))
		}
	}
	changedStr := "sin cambios eléctricos coherentes"
	if len(changed) > 0 {
		changedStr = strings.Join(changed, ", ")
	}

	switch t {
	case RealAnomaly:
		return fmt.Sprintf("Consumo %.1f%% por encima del baseline sin evento conocido; %s.", variationPct, changedStr)
	case TypeDataQuality:
		return fmt.Sprintf("Inconsistencia eléctrica o de calidad de datos detectada (variación de consumo %.1f%%); %s.", variationPct, changedStr)
	case ExplainableAnomaly:
		return fmt.Sprintf("Consumo %.1f%% respecto al baseline, explicado por un evento operativo registrado; %s.", variationPct, changedStr)
	case FalsePositive:
		return fmt.Sprintf("Variación de %.1f%% explicada por una parada programada; no se escala como anomalía real.", variationPct)
	default:
		return ""
	}
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
