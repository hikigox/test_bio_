package engine

import "math"

// strongInconsistencyFactor: una inconsistencia eléctrica se considera "fuerte"
// (02 §5.1) cuando duplica su propio umbral adaptativo. El detector de 02 §2.6
// ya compara la mitad más reciente de la ventana contra la mediana baseline, de
// modo que el hecho de que la señal exista implica que la desviación es
// sostenida ("persistente"); lo que falta discriminar es la fuerza.
const strongInconsistencyFactor = 2.0

// Classify implementa el árbol de decisión de 02 §5, en orden. Devuelve el tipo
// de anomalía, su severidad, la confianza (02 §7) con su desglose, y la ruta de
// decisión recorrida (para auditar por qué se etiquetó así).
//
// Devuelve AnomalyType "" (y Severity "") cuando no hay ninguna evidencia:
// rama 5, consumo normal.
// variationThresholdPct es T_var,i, el umbral adaptativo de "variación
// significativa" del propio medidor (MeterThresholds.VariationPct).
func Classify(signals []Signal, issues []DataQualityIssue, events []RelatedEvent, variables []VariableDelta, variationPct, variationThresholdPct float64) (AnomalyType, Severity, float64, ConfidenceBreakdown, []string) {
	has := func(t SignalType) bool {
		for _, s := range signals {
			if s.Signal == t {
				return true
			}
		}
		return false
	}

	// Rama 5: sin señales ni problemas de datos no hay nada que clasificar.
	if len(signals) == 0 && len(issues) == 0 {
		return "", "", 0, ConfidenceBreakdown{}, []string{"no_signals"}
	}

	path := []string{}

	// "Cambio significativo" es exactamente lo que define 02 §10:
	// variation_pct > T_var,i. NO basta con que haya disparado alguna señal de
	// consumo: un único OUTLIER horario (o un HOURLY_PATTERN) describe horas
	// sueltas fuera del perfil, no un cambio de nivel — un medidor con una hora
	// anómala y variación ~0 % no debe enrutarse a las ramas 2-4 como
	// REAL_ANOMALY. Esas señales siguen en Signals/evidencia para auditoría,
	// pero no deciden esta bifurcación.
	consumptionChanged := abs(variationPct) > variationThresholdPct

	// 1. Consumo estable pero eléctricas inconsistentes o datos defectuosos.
	//    La condición "consumo estable" es parte de la rama: si el consumo sí
	//    cambió, un problema de datos no debe enmascarar la anomalía de consumo
	//    (sigue reportándose en la evidencia y baja la confianza).
	if !consumptionChanged && (has(ElectricalInconsistency) || has(DataQuality) || len(issues) > 0) {
		// En esta rama el consumo es estable por construcción, así que la
		// fuerza de la señal se mide sobre la propia señal que disparó y no
		// sobre variationPct (que aquí es ~0). Ver computeConfidence.
		confidence, breakdown := computeConfidence(signals, events, issues, variables, variationPct, true)
		path = append(path, "consumption_stable", "data_quality_issue")
		return TypeDataQuality, dataQualitySeverity(signals), confidence, breakdown, path
	}
	if !consumptionChanged {
		// Consumo estable y sin problemas de datos: rama 5. Puede haber señales
		// (outliers horarios aislados) que quedan registradas en la evidencia,
		// pero ninguna rama del árbol de 02 §5 aplica, así que no hay anomalía.
		return "", "", 0, ConfidenceBreakdown{}, []string{"consumption_stable", "no_data_quality_issue"}
	}
	path = append(path, "consumption_changed")

	confidence, breakdown := computeConfidence(signals, events, issues, variables, variationPct, false)

	// 2. Cambio significativo + evento que lo explica. MatchEvents (02 §4) ya
	//    comprobó que la dirección del cambio case con el tipo de evento, así
	//    que aquí basta con leer el tipo del evento explicativo.
	if ev := explainingEvent(events); ev != nil {
		path = append(path, "event_explains")
		if ev.Event.Type == "SCHEDULED_OUTAGE" {
			return FalsePositive, Low, confidence, breakdown, path
		}
		return ExplainableAnomaly, Medium, confidence, breakdown, path
	}
	path = append(path, "no_event")

	// 3-4. Cambio significativo sin evento: la severidad depende de si las
	//      variables eléctricas cambiaron de forma coherente con el consumo.
	if countChangedVariables(variables) > 0 {
		path = append(path, "electrical_changes")
		return RealAnomaly, High, confidence, breakdown, path
	}
	path = append(path, "no_electrical_changes")
	return RealAnomaly, Medium, confidence, breakdown, path
}

// explainingEvent devuelve el evento que explica el cambio, dando preferencia a
// una parada programada para que el resultado no dependa del orden de la lista.
func explainingEvent(events []RelatedEvent) *RelatedEvent {
	var first *RelatedEvent
	for i := range events {
		if events[i].Relation != Explains {
			continue
		}
		if events[i].Event.Type == "SCHEDULED_OUTAGE" {
			return &events[i]
		}
		if first == nil {
			first = &events[i]
		}
	}
	return first
}

// dataQualitySeverity aplica "High si la inconsistencia es fuerte y persistente"
// (02 §5.1); cualquier otro problema de datos es Medium.
func dataQualitySeverity(signals []Signal) Severity {
	for _, s := range signals {
		if s.Signal == ElectricalInconsistency && s.Threshold > 0 &&
			abs(s.Observed) >= strongInconsistencyFactor*s.Threshold {
			return High
		}
	}
	return Medium
}

// hasInformativeEvent indica si en la ventana ±24 h hay algún evento que
// realmente aporte información operativa. Un evento de tipo UNKNOWN es el
// registro explícito de que NO se reportó ninguna operación ("No operational
// event reported"): deja el cuadro de eventos tan concluyente como la ausencia
// total de eventos, no ambiguo. Contarlo como evento ambiguo penalizaba la
// confianza justo en el caso que 02 §5 rama 3 describe como "sin evento".
func hasInformativeEvent(events []RelatedEvent) bool {
	for _, e := range events {
		if e.Event.Type != "UNKNOWN" && e.Event.Type != "" {
			return true
		}
	}
	return false
}

func countChangedVariables(variables []VariableDelta) int {
	n := 0
	for _, v := range variables {
		if v.Changed {
			n++
		}
	}
	return n
}

// dataQualityStrengthFraction mide la fuerza de una anomalía de calidad de
// datos con la razón |Observed|/Threshold de la señal que disparó, en lugar de
// con la variación de consumo (que en esa rama es ~0 por construcción). Es la
// alternativa "/z" que nombra 02 §7 para señales que no se miden en porcentaje
// de consumo. Se normaliza con el mismo criterio de "fuerte" que usa la
// severidad (strongInconsistencyFactor): justo en el umbral vale 0.5, al doble
// del umbral o más satura en 1.
func dataQualityStrengthFraction(signals []Signal) float64 {
	best := 0.0
	for _, s := range signals {
		if s.Signal != ElectricalInconsistency && s.Signal != DataQuality {
			continue
		}
		if s.Threshold <= 0 {
			continue
		}
		if ratio := abs(s.Observed) / s.Threshold; ratio > best {
			best = ratio
		}
	}
	return minFloat(best/strongInconsistencyFactor, 1)
}

// dataQualityMagnitude mide la severidad de una anomalía DATA_QUALITY para
// PriorityScore (02 §6, "DATA_QUALITY... usan magnitud normalizada propia"):
// el mayor |Observed| entre las señales ElectricalInconsistency/DataQuality
// que dispararon, en el mismo porcentaje en el que ya vienen esas señales
// (Signal.Observed), para que sea comparable a variationPct en PriorityScore.
// Se elige sobre dataQualityStrengthFraction (que normaliza a [0,1] contra el
// umbral) porque PriorityScore ya hace su propia normalización con /100; pasar
// una fracción ya normalizada la aplastaría dos veces.
func dataQualityMagnitude(signals []Signal) float64 {
	best := 0.0
	for _, s := range signals {
		if s.Signal != ElectricalInconsistency && s.Signal != DataQuality {
			continue
		}
		if v := abs(s.Observed); v > best {
			best = v
		}
	}
	return best
}

// computeConfidence combina los cuatro términos de 02 §7 y redondea a 2
// decimales. El máximo es 1.0 (0.4 + 0.3 + 0.2 + 0.1):
//
//   - fuerza de la señal (0.4): |variación| saturada en 100%, o, cuando se está
//     puntuando la rama DATA_QUALITY (dataQualityCase), la razón
//     observado/umbral de la señal que disparó — 02 §7 admite ambas bases
//     ("variación/z") y la de consumo no aplica a un consumo estable.
//   - señales concordantes (0.3): nº de señales más nº de variables eléctricas
//     que cambiaron de forma coherente, saturado en 3 piezas de evidencia.
//   - evento (0.2): el cuadro de eventos es concluyente, o porque un evento
//     explica el cambio, o porque no hay ningún evento en la ventana. Se pierde
//     cuando solo hay eventos meramente coincidentes en el tiempo (Related),
//     que dejan el diagnóstico ambiguo.
//   - calidad de datos (0.1): el medidor no arrastra lecturas defectuosas.
func computeConfidence(signals []Signal, events []RelatedEvent, issues []DataQualityIssue, variables []VariableDelta, variationPct float64, dataQualityCase bool) (float64, ConfidenceBreakdown) {
	strengthFraction := minFloat(abs(variationPct)/100, 1)
	if dataQualityCase {
		strengthFraction = dataQualityStrengthFraction(signals)
	}
	strength := strengthFraction * 0.4

	concordant := minFloat(float64(len(signals)+countChangedVariables(variables))/3, 1) * 0.3

	eventPresence := 0.0
	if !hasInformativeEvent(events) || explainingEvent(events) != nil {
		eventPresence = 0.2
	}

	dataQuality := 0.1
	if len(issues) > 0 {
		dataQuality = 0.0
	}

	b := ConfidenceBreakdown{
		SignalStrength:    round2(strength),
		ConcordantSignals: round2(concordant),
		EventPresence:     eventPresence,
		DataQuality:       dataQuality,
	}
	return round2(strength + concordant + eventPresence + dataQuality), b
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func severityWeight(s Severity) float64 {
	switch s {
	case High:
		return 3
	case Medium:
		return 2
	default:
		return 1
	}
}

// typeBand separa los tipos en bandas disjuntas de anchura 1 para garantizar el
// invariante duro de 02 §6 ("FALSE_POSITIVE nunca supera a un REAL_ANOMALY" y
// "la lista debe poner primero la anomalía real de mayor impacto"). Un simple
// factor de penalización multiplicativo no basta: un REAL_ANOMALY con poca
// variación y poca confianza puede puntuar por debajo de un FALSE_POSITIVE
// grande y confiable.
func typeBand(t AnomalyType) float64 {
	switch t {
	case RealAnomaly:
		return 2
	case ExplainableAnomaly, TypeDataQuality:
		return 1
	default: // FalsePositive y "" (sin anomalía)
		return 0
	}
}

// PriorityScore ordena las anomalías (02 §6).
//
// Dentro de cada banda de tipo el orden es exactamente el de la fórmula del
// spec, severity_weight (H=3,M=2,L=1) × confidence × min(|variation|/100, 2),
// normalizada a [0, 1) dividiendo por su máximo (3 × 1 × 2 = 6) y escalada por
// 0.99 para que nunca alcance el suelo de la banda superior. A eso se suma la
// banda del tipo, de forma que REAL_ANOMALY ∈ [2, 3), EXPLAINABLE_ANOMALY y
// DATA_QUALITY ∈ [1, 2) y FALSE_POSITIVE ∈ [0, 1).
func PriorityScore(t AnomalyType, sev Severity, confidence, variationPct float64) float64 {
	magnitude := minFloat(abs(variationPct)/100, 2)
	raw := severityWeight(sev) * maxFloat(confidence, 0) * magnitude
	normalized := minFloat(raw/6, 1) * 0.99
	return round2(typeBand(t) + normalized)
}
