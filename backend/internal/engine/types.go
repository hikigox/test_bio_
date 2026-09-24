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
type ChangePoint struct {
	Found bool
	At    time.Time
	Sigma float64
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
