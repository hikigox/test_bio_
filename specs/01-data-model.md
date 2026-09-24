# 01 · Modelo de datos

## Entradas (CSV, ver `/data`)
- `readings.csv`: meter_id, timestamp, consumption_kwh, voltage_v, current_a, power_factor (+ status si existe).
- `events.csv`: meter_id, timestamp, type, description.
> Tarea 0: inspeccionar los CSV reales y ajustar nombres de columnas, intervalo y zona horaria.

## Relaciones
```
analyses 1 ──< meter_baselines   (1 fila por medidor y análisis: los 12, tengan o no anomalía)
analyses 1 ──< anomalies         (0..1 por medidor y análisis)
anomalies 1 ──< anomaly_signals    (qué detectores se dispararon)
anomalies 1 ──< anomaly_variables  (qué variables cambiaron)
anomalies >──< events (anomaly_events)  (eventos relacionados)
meters 1 ──< readings, events
```
**Identificadores:** `analyses.id` y `anomalies.id` son secuencias independientes. Un análisis genera varias anomalías; cada anomalía guarda `analysis_id` como FK.

**Regla de vigencia:** para cada medidor, la anomalía y el baseline **vigentes** son los del último análisis COMPLETED que lo incluyó (un análisis puede cubrir todos los medidores o solo algunos). Los análisis anteriores se conservan como histórico. El `status` de triaje (OPEN/INVESTIGATING/…) vive en cada fila de anomalía y no se hereda entre análisis (limitación conocida del MVP).

## Esquema SQLite

### Datos base
```sql
meters(id INTEGER PK, meter_id TEXT UNIQUE, name TEXT, location TEXT, status TEXT, created_at TEXT);
readings(id INTEGER PK, meter_id TEXT, timestamp TEXT, consumption_kwh REAL,
         voltage_v REAL, current_a REAL, power_factor REAL, status TEXT,
         UNIQUE(meter_id, timestamp));
events(id INTEGER PK, meter_id TEXT, timestamp TEXT, type TEXT, description TEXT);
users(id INTEGER PK, email TEXT UNIQUE, password_hash TEXT);
```

### Análisis (la corrida completa y su contexto)
```sql
analyses(
  id INTEGER PK,
  status TEXT, stage TEXT, progress REAL,          -- estado en vivo
  trigger TEXT, triggered_by INTEGER,              -- MANUAL | SCHEDULED, user id
  started_at TEXT, finished_at TEXT, duration_ms INTEGER,
  engine_version TEXT,                             -- reproducibilidad
  config_json TEXT,                                -- umbrales usados en esta corrida
  scope_meter_ids_json TEXT NULL,                  -- NULL = todos los medidores
  data_from TEXT, data_to TEXT,                    -- período analizado
  baseline_from TEXT NULL, baseline_to TEXT NULL,  -- ventana de referencia manual (NULL = automática)
  readings_analyzed INTEGER, meters_analyzed INTEGER,
  anomalies_count INTEGER, high_priority_count INTEGER, avg_confidence REAL,
  summary_message TEXT,                            -- "4 anomalías detectadas · 2 requieren atención prioritaria"
  stage_log_json TEXT,                             -- [{stage, started_at, finished_at, ms}]
  error TEXT
);
```

### Baseline por análisis y medidor
Se guarda para **todos** los medidores; alimenta la lista, el detalle y las gráficas.
```sql
meter_baselines(
  id INTEGER PK, analysis_id INTEGER FK, meter_id TEXT,
  method TEXT,                                     -- p.ej. HOURLY_MEDIAN
  window_from TEXT, window_to TEXT,                -- ventana usada como "normal"
  change_point_at TEXT NULL,                       -- si se detectó cambio
  baseline_kwh REAL, actual_kwh REAL, variation_pct REAL,
  hourly_profile_json TEXT,                        -- 24 valores (mediana por hora)
  baseline_voltage_v REAL, baseline_current_a REAL, baseline_power_factor REAL,
  actual_voltage_v REAL,   actual_current_a REAL,   actual_power_factor REAL,
  readings_count INTEGER, data_quality_score REAL, -- 0..1
  UNIQUE(analysis_id, meter_id)
);
```

### Anomalías (resultado explícito y consultable)
```sql
anomalies(
  id INTEGER PK, analysis_id INTEGER FK, meter_id TEXT,
  detected_at TEXT, period_from TEXT, period_to TEXT, change_point_at TEXT NULL,
  type TEXT, severity TEXT,
  confidence REAL, confidence_label TEXT,          -- LOW | MEDIUM | HIGH
  priority_score REAL,                             -- el rank se calcula al consultar (ver 03)
  baseline_kwh REAL, actual_kwh REAL, variation_pct REAL,   -- copia para consultar sin joins
  reason TEXT, recommended_action TEXT,
  explanation_source TEXT,                         -- TEMPLATE | LLM
  status TEXT, created_at TEXT, updated_at TEXT,
  evidence_json TEXT,                              -- ver abajo
  UNIQUE(analysis_id, meter_id)
);

anomaly_signals(          -- por qué se disparó
  id INTEGER PK, anomaly_id INTEGER FK,
  signal TEXT,            -- PERSISTENT_SHIFT | SPIKE | OUTLIER | HOURLY_PATTERN | DATA_QUALITY | ELECTRICAL_INCONSISTENCY
  observed REAL, threshold REAL, detail TEXT
);

anomaly_variables(        -- variables que cambiaron (pantalla Investigación)
  id INTEGER PK, anomaly_id INTEGER FK,
  variable TEXT,          -- consumption_kwh | voltage_v | current_a | power_factor
  baseline REAL, actual REAL, delta_pct REAL, changed INTEGER
);

anomaly_events(           -- eventos relacionados
  anomaly_id INTEGER FK, event_id INTEGER FK,
  relation TEXT,          -- EXPLAINS | RELATED
  offset_hours REAL       -- distancia al punto de cambio
);
```

### `evidence_json` (solo lo que no requiere consulta directa)
```json
{"decision_path":["no_data_quality_issue","significant_change","no_event","electrical_changes"],
 "confidence_breakdown":{"signal_strength":0.4,"concordant_signals":0.3,"event_presence":0.2,"data_quality":0.1},
 "data_quality_issues":[{"kind":"NULLS","count":0}],
 "affected_readings":{"count":168,"first":"","last":""}}
```

## Enums
- `meters.status`: UNKNOWN | OK | ALERT | CRITICAL. Lo escribe el motor al cerrar el análisis según la anomalía vigente (tabla de mapeo en 03-api.md).
- `anomalies.type`: REAL_ANOMALY | EXPLAINABLE_ANOMALY | DATA_QUALITY | FALSE_POSITIVE.
- `severity`: LOW | MEDIUM | HIGH.
- `anomalies.status`: OPEN | INVESTIGATING | RESOLVED | DISMISSED.
- `analyses.status`: PENDING | RUNNING | COMPLETED | FAILED.
- `analyses.stage`: READINGS | BASELINE | DETECTION | CORRELATION | EVENTS | EXPLANATION | RECOMMENDATION.

## Reglas de carga y escritura
- Carga idempotente (UNIQUE meter_id+timestamp); timestamps en UTC ISO-8601.
- Lecturas inválidas se cargan y se marcan en `status`; no se descartan (alimentan calidad de datos).
- Un análisis escribe baselines y anomalías en **una transacción** al terminar; si falla, queda FAILED sin datos parciales visibles.
- Los medidores sin anomalía tienen baseline pero no fila en `anomalies`.
