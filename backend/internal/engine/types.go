// Package engine contains the pure-computation anomaly detection engine:
// robust statistics, baselines, thresholds, signal detectors, and
// classification. It has no database or network dependencies.
package engine

import "time"

// Reading es una lectura horaria de un medidor.
type Reading struct {
	MeterID        string
	Timestamp      time.Time // UTC
	ConsumptionKWh float64
	VoltageV       float64
	CurrentA       float64
	PowerFactor    float64
	Status         string // "OK" u otro valor libre del CSV
}

// ChangePoint describe el resultado de la detección de punto de cambio (CUSUM).
//
// At es el instante en que empieza el cambio y EndsAt el final (exclusivo) del
// tramo en el que el cambio se mantiene. Un cambio que llega hasta el final de
// la serie tiene EndsAt = fin de la última lectura; un cambio transitorio (p.ej.
// una parada de 12 h) tiene EndsAt en el momento en que el consumo vuelve a su
// nivel normal. Ese par [At, EndsAt) es el tramo sobre el que se mide
// variation_pct (02 §1): medir "actual" sobre todo el período diluye el cambio
// con los días que no cambiaron.
type ChangePoint struct {
	Found  bool
	At     time.Time
	EndsAt time.Time
	Sigma  float64
}

// HourlyProfile es el perfil horario de consumo de un medidor sobre una ventana baseline.
type HourlyProfile struct {
	// Mediana de consumo por hora del día (0-23), sobre la ventana baseline.
	MedianByHour [24]float64
	WindowFrom   time.Time
	WindowTo     time.Time
	VoltageV     float64 // mediana en la ventana baseline
	CurrentA     float64
	PowerFactor  float64
}

// Config agrupa los pisos/constantes de 02 §10 (viven en thresholds.go).
type Config struct {
	MinVariationPctFloor float64 // 15
	VariationPctK        float64 // 3
	OutlierZThreshold    float64 // 3.5
	PersistenceHours     float64 // 48
	ElectricalPctFloor   float64 // 15
	ElectricalPctK       float64 // 3
	MinBaselineDays      int     // 3
	DefaultBaselineDays  int     // 7
}

// DefaultConfig devuelve la configuración por defecto según spec 02 §10.
func DefaultConfig() Config {
	return Config{
		MinVariationPctFloor: 15,
		VariationPctK:        3,
		OutlierZThreshold:    3.5,
		PersistenceHours:     48,
		ElectricalPctFloor:   15,
		ElectricalPctK:       3,
		MinBaselineDays:      3,
		DefaultBaselineDays:  7,
	}
}

// ThresholdOrigin indica si los umbrales de un medidor se calcularon con su
// propio ruido histórico (Calculated) o si se usó el ruido de la flota como
// respaldo por falta de datos suficientes (Fallback).
type ThresholdOrigin string

const (
	Calculated ThresholdOrigin = "CALCULATED"
	Fallback   ThresholdOrigin = "FALLBACK"
)

// MeterThresholds son los umbrales adaptativos de un medidor (02 §10):
// umbral = max(piso, k * ruido propio del medidor).
type MeterThresholds struct {
	VariationPct  float64
	OutlierZ      float64
	ElectricalPct float64
	Origin        ThresholdOrigin
}

// SignalType identifica cuál de los 5 detectores de 02 §2-3 disparó una señal.
type SignalType string

const (
	PersistentShift         SignalType = "PERSISTENT_SHIFT"
	Outlier                 SignalType = "OUTLIER"
	HourlyPattern           SignalType = "HOURLY_PATTERN"
	DataQuality             SignalType = "DATA_QUALITY"
	ElectricalInconsistency SignalType = "ELECTRICAL_INCONSISTENCY"
)

// Signal es una señal disparada por un detector, con el valor observado y el
// umbral que se comparó, para alimentar evidence.signals (03-api.md).
type Signal struct {
	Signal    SignalType
	Observed  float64
	Threshold float64
	Detail    string
}

// DataQualityIssue describe un problema de calidad de datos encontrado
// (independiente de si generó señal DATA_QUALITY), para alimentar
// evidence.data_quality_issues.
type DataQualityIssue struct {
	Kind  string
	Count int
}

// Event es un evento operativo reportado para un medidor (nueva línea de
// producción, parada programada, etc.), usado para explicar cambios de consumo.
type Event struct {
	MeterID     string
	Timestamp   time.Time // UTC
	Type        string    // p.ej. OPERATIONAL_CHANGE, SCHEDULED_OUTAGE, DATA_QUALITY, UNKNOWN
	Description string
}

// VariableDelta compara una variable eléctrica antes y después del punto de
// cambio, para alimentar evidence.variables (02 §3).
type VariableDelta struct {
	Variable string // consumption_kwh | voltage_v | current_a | power_factor
	Baseline float64
	Actual   float64
	DeltaPct float64
	Changed  bool
}

// EventRelation indica si un evento cercano explica semánticamente el cambio
// observado (Explains) o si solo coincide en el tiempo (Related).
type EventRelation string

const (
	Explains EventRelation = "EXPLAINS"
	Related  EventRelation = "RELATED"
)

// RelatedEvent es un evento dentro de la ventana ±24h del punto de cambio,
// con su relación con el cambio y su desfase en horas (negativo = anterior).
type RelatedEvent struct {
	Event       Event
	Relation    EventRelation
	OffsetHours float64
}

// AnomalyType es la etiqueta que produce el árbol de clasificación de 02 §5.
// El valor cero ("") significa "sin anomalía" (rama 5: consumo normal).
type AnomalyType string

const (
	RealAnomaly        AnomalyType = "REAL_ANOMALY"
	ExplainableAnomaly AnomalyType = "EXPLAINABLE_ANOMALY"
	TypeDataQuality    AnomalyType = "DATA_QUALITY"
	FalsePositive      AnomalyType = "FALSE_POSITIVE"
)

// Severity es la severidad de una anomalía (02 §5-6). El valor cero ("")
// acompaña a AnomalyType "" cuando no hay anomalía.
type Severity string

const (
	High   Severity = "HIGH"
	Medium Severity = "MEDIUM"
	Low    Severity = "LOW"
)

// ConfidenceBreakdown desglosa los cuatro términos de la confianza de 02 §7
// para que la puntuación sea explicable en la evidencia. Los cuatro suman
// como máximo 1.0 (0.4 + 0.3 + 0.2 + 0.1).
type ConfidenceBreakdown struct {
	SignalStrength    float64 // 0–0.4: magnitud de la variación observada
	ConcordantSignals float64 // 0–0.3: nº de señales y variables coherentes
	EventPresence     float64 // 0 o 0.2: el cuadro de eventos es concluyente
	DataQuality       float64 // 0 o 0.1: el medidor no tiene datos defectuosos
}

// Evidence agrupa la traza de decisión de un análisis (02 §9, evidence_json de
// 03-api.md), para que la clasificación sea auditable.
type Evidence struct {
	DecisionPath          []string
	ConfidenceBreakdown   ConfidenceBreakdown
	DataQualityIssues     []DataQualityIssue
	AffectedReadingsCount int
	AffectedReadingsFirst time.Time
	AffectedReadingsLast  time.Time
}

// MeterResult es el resultado completo del pipeline (02 §1-9) para un medidor
// en un período de análisis: la salida pública de Run que consumirá la fase API.
type MeterResult struct {
	MeterID     string
	Baseline    HourlyProfile
	ChangePoint ChangePoint
	Thresholds  MeterThresholds

	BaselineKWh  float64
	ActualKWh    float64
	VariationPct float64

	Signals   []Signal
	Variables []VariableDelta
	Events    []RelatedEvent

	HasAnomaly bool
	Type       AnomalyType
	Severity   Severity
	// Confidence es el float crudo (02 §7); la etiqueta Baja/Media/Alta
	// (<0.6 / 0.6-0.85 / >0.85) se deriva de este valor en la fase API, que es
	// quien construye la respuesta HTTP — el motor no decide presentación.
	Confidence    float64
	PriorityScore float64

	Reason            string
	RecommendedAction string
	Evidence          Evidence
}

// Run ejecuta el pipeline completo de 02 §1-9 para un medidor: baseline,
// umbrales, detección de señales, correlación de variables, emparejamiento de
// eventos, clasificación y explicación por plantilla.
//
// readings debe venir ordenado por timestamp ascendente: el refinamiento
// horario del punto de cambio y la comprobación de persistencia por bloques
// recorren la serie en orden.
//
// events puede contener eventos de otros medidores (p.ej. si el llamador pasa
// la tabla completa de eventos del período); Run filtra por meterID antes de
// pasarlos a MatchEvents, porque MatchEvents solo compara por ventana de
// tiempo y no conoce el medidor — sin este filtro, dos medidores con eventos
// propios en ventanas de tiempo similares podrían "robarse" eventos entre sí.
func Run(meterID string, readings []Reading, events []Event, fleetCV float64, cfg Config) MeterResult {
	if len(readings) == 0 {
		// Sin lecturas no hay nada que analizar; devolver un resultado vacío en
		// vez de indexar readings[0] más abajo (que entraría en pánico).
		return MeterResult{MeterID: meterID}
	}

	cp := DetectChangePoint(readings, cfg)

	baselineFrom := readings[0].Timestamp
	baselineTo := baselineFrom.Add(time.Duration(cfg.DefaultBaselineDays) * 24 * time.Hour)
	if cp.Found {
		baselineTo = cp.At
	}
	profile := BuildHourlyProfile(readings, baselineFrom, baselineTo)
	th := ComputeThresholds(readings, baselineFrom, baselineTo, fleetCV, cfg)

	signals, issues := DetectSignals(readings, profile, cp, th, cfg)
	variables := CorrelateVariables(readings, profile, cp)

	// 02 §1: variation_pct se mide sobre el tramo posterior al punto de cambio
	// (o sobre todo el período si no hay cambio). Ver ComputeVariation.
	actualKWh, baselineKWh, variationPct := ComputeVariation(readings, profile, cp)

	// MatchEvents solo filtra por ventana de tiempo, no por medidor: hay que
	// acotar a los eventos de este medidor antes de llamarlo.
	meterEvents := make([]Event, 0, len(events))
	for _, e := range events {
		if e.MeterID == meterID {
			meterEvents = append(meterEvents, e)
		}
	}
	relatedEvents := MatchEvents(meterEvents, cp, variationPct)

	result := MeterResult{
		MeterID: meterID, Baseline: profile, ChangePoint: cp, Thresholds: th,
		BaselineKWh: baselineKWh, ActualKWh: actualKWh, VariationPct: round2(variationPct),
		Signals: signals, Variables: variables, Events: relatedEvents,
	}

	typ, sev, confidence, breakdown, path := Classify(signals, issues, relatedEvents, variables, variationPct, th.VariationPct)
	if typ == "" {
		result.HasAnomaly = false
		return result
	}

	// PriorityScore's magnitude term is normally variationPct, but Classify's
	// DATA_QUALITY branch (classify.go) requires consumptionChanged == false,
	// i.e. variationPct ≈ 0 by construction. Passing that through would score
	// every DATA_QUALITY case identically regardless of how severe the
	// underlying inconsistency actually is. Spec 02§6 explicitly allows a
	// type-specific magnitude here ("DATA_QUALITY y FALSE_POSITIVE usan
	// magnitud normalizada propia"), so for DATA_QUALITY we substitute the
	// largest |Observed| among the firing ElectricalInconsistency/DataQuality
	// signals — the same magnitude Classify's own confidence fix
	// (dataQualityStrengthFraction) already uses for its strength term, kept
	// on the same percent scale PriorityScore expects.
	priorityMagnitude := variationPct
	if typ == TypeDataQuality {
		priorityMagnitude = dataQualityMagnitude(signals)
	}

	result.HasAnomaly = true
	result.Type = typ
	result.Severity = sev
	result.Confidence = confidence
	result.PriorityScore = PriorityScore(typ, sev, confidence, priorityMagnitude)
	result.Reason = BuildReason(typ, variationPct, variables)
	result.RecommendedAction = BuildRecommendation(typ)
	result.Evidence = Evidence{
		DecisionPath:          path,
		ConfidenceBreakdown:   breakdown,
		DataQualityIssues:     issues,
		AffectedReadingsCount: len(readings),
		AffectedReadingsFirst: readings[0].Timestamp,
		AffectedReadingsLast:  readings[len(readings)-1].Timestamp,
	}
	return result
}
