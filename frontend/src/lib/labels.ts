// Diccionario centralizado de etiquetas técnicas del motor (backend/internal/engine)
// a su significado en español para mostrar en la UI. Cualquier código nuevo que
// aparezca en evidence/signals/variables/events debe añadirse aquí; `label()`
// devuelve el código sin traducir si no lo encuentra, para no ocultar datos.
const LABELS: Record<string, string> = {
  // AnomalyType (engine/types.go) — clasificación final de 02 §5.
  REAL_ANOMALY: "Anomalía real",
  EXPLAINABLE_ANOMALY: "Anomalía explicable por un evento",
  DATA_QUALITY: "Problema de calidad de datos",
  FALSE_POSITIVE: "Falso positivo",

  // SignalType — detectores que pueden disparar una señal.
  PERSISTENT_SHIFT: "Cambio de nivel sostenido",
  OUTLIER: "Lectura atípica puntual",
  HOURLY_PATTERN: "Patrón horario alterado",
  ELECTRICAL_INCONSISTENCY: "Inconsistencia eléctrica",

  // Event.Type — eventos operativos reportados.
  OPERATIONAL_CHANGE: "Cambio operativo",
  SCHEDULED_OUTAGE: "Parada programada",
  UNKNOWN: "Sin evento reportado",

  // EventRelation — cómo se relaciona un evento con el cambio detectado.
  EXPLAINS: "Explica el cambio",
  RELATED: "Coincide en el tiempo",

  // Variables eléctricas (VariableDelta.Variable).
  consumption_kwh: "Consumo (kWh)",
  voltage_v: "Voltaje (V)",
  current_a: "Corriente (A)",
  power_factor: "Factor de potencia",

  // Ruta de decisión (classify.go) — pasos del árbol de clasificación.
  no_signals: "Sin señales detectadas",
  consumption_stable: "Consumo estable",
  consumption_changed: "Consumo cambió de forma significativa",
  data_quality_issue: "Con problema de calidad de datos",
  no_data_quality_issue: "Sin problema de calidad de datos",
  event_explains: "Un evento explica el cambio",
  no_event: "Sin evento que lo explique",
  electrical_changes: "Con cambios eléctricos coherentes",
  no_electrical_changes: "Sin cambios eléctricos coherentes",

  // ConfidenceBreakdown (02 §7) — términos del desglose de confianza.
  signal_strength: "Fuerza de la señal",
  concordant_signals: "Señales concordantes",
  event_presence: "Cuadro de eventos concluyente",
  data_quality: "Calidad de datos",

  // DataQualityIssue.Kind — problemas de calidad de datos detectados.
  OUT_OF_RANGE: "Lecturas fuera de rango",
  NULLS: "Lecturas nulas/faltantes",
};

/** Traduce un código técnico del motor a su significado en español. Si no se
 * conoce el código, devuelve el original en vez de ocultarlo. */
export function label(code: string): string {
  return LABELS[code] ?? code;
}
