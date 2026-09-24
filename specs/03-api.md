# 03 · API (Go, JSON, prefijo /api)

Auth simple: `POST /auth/login {email,password}` → `{token}`; header `Authorization: Bearer <token>` en el resto. Usuario demo sembrado.
Errores: `{"error":"mensaje"}` con código HTTP correcto. CORS habilitado para el frontend. Fechas en ISO-8601 UTC.

## Endpoints
| Método | Ruta | Descripción |
|---|---|---|
| GET | /meters?status=&q=&sort=&order=&from=&to= | Lista con consumo del período, baseline esperado, variación, estado y anomalía vigente |
| GET | /meters/:meterId?from=&to= | Detalle + baseline + variación del período |
| GET | /meters/:meterId/readings?from=&to=&interval= | Serie (consumo, V, I, PF) |
| GET | /meters/:meterId/events?from=&to= | Eventos del medidor |
| GET | /meters/:meterId/anomalies?scope=current\|history&from=&to= | Anomalías del medidor (vigente o todas las de análisis pasados) |
| GET | /anomalies?meter_id=&type=&severity=&status=&analysis_id=&from=&to= | Vigentes por defecto, ordenadas por prioridad; `from`/`to` filtran por solapamiento con la ventana activa |
| GET | /anomalies/:id | Detalle con evidencia |
| PATCH | /anomalies/:id | Cambiar status (INVESTIGATING/RESOLVED/DISMISSED) |
| POST | /ai/analyze | Lanza análisis (alcance opcional) → 202 `{id,status}` |
| GET | /ai/analysis/:id | Estado, etapa y resultado |
| GET | /ai/analysis?limit= | Historial de análisis |
| GET | /dashboard/summary?from=&to= | KPIs, consumo por medidor (pastel) y rango de datos |

`sort`: consumption | variation | severity. `status`: all | normal | alert | critical.

## Alcance del análisis
`POST /ai/analyze` acepta un cuerpo opcional:
```json
{"meter_ids":["M-109"],"from":"2026-01-01T00:00:00Z","to":"2026-01-14T23:00:00Z",
 "baseline_from":"2026-01-01T00:00:00Z","baseline_to":"2026-01-07T23:00:00Z"}
```
- **Sin cuerpo (por defecto): todos los medidores y todo el período.** Es el caso del botón Run AI Analysis del Dashboard.
- `meter_ids`: analiza solo esos medidores (por ejemplo, desde el detalle de M-109). El motor sigue leyendo la flota completa como referencia (ruido de respaldo, ver 02 §10), pero solo escribe resultados para los medidores pedidos.
- `from`/`to`: período que se analiza.
- `baseline_from`/`baseline_to` (opcional): ventana de referencia manual. Si se omite, el motor la deduce (02 §1). Validación: ≥ 3 días y dentro de los datos; si no, 400.
- Por qué es de flota por defecto: la **priorización compara medidores entre sí** y el respaldo de umbrales usa a toda la flota.
- Solo un análisis RUNNING a la vez (409 si hay otro).

## Vigencia y prioridad
- Para cada medidor, la anomalía **vigente** es la del último análisis COMPLETED que lo incluyó.
- `priority_rank` no se guarda: se calcula al consultar, ordenando por `priority_score` todas las anomalías vigentes (no depende de los filtros). Así un análisis de un solo medidor queda bien posicionado frente al resto.

## Cuándo ocurrió una anomalía y filtro de fechas
- Cada anomalía tiene una **ventana activa** derivada al consultar: `active_from = change_point_at ?? period_from`, `active_to = period_to`.
- Con `from`/`to`, `/anomalies` y `/meters/:id/anomalies` devuelven solo las anomalías cuya ventana activa se **solapa** con el rango (`active_from <= to` y `active_to >= from`).
- En `/meters`, la anomalía siempre se devuelve, con `in_range: true|false` para que la UI la atenúe si queda fuera del rango.
- El filtro no altera tipo, severidad, confianza ni el status del medidor.

## Baseline y filtro de fechas
El baseline **no se recalcula con el filtro de fechas**, pero **sí se aplica a él**:
1. El baseline se calcula en el análisis, sobre su ventana de referencia (normal, anterior al cambio), y se guarda como perfil horario (`meter_baselines.hourly_profile_json`).
2. Al consultar con `from`/`to`, la API calcula `consumption_kwh` real del rango y `baseline_kwh` esperado del **mismo rango**: suma del perfil sobre los timestamps de las lecturas existentes en ese rango (así las lecturas faltantes no distorsionan). `variation_pct = (real − esperado) / esperado`.
3. Sin filtro, el rango es todo el período analizado.
- Por qué no se recalcula con el filtro: si el rango cae dentro del período anómalo (ej. M-109 solo los últimos 5 días), el "baseline" saldría del propio comportamiento anómalo y la variación sería ~0 %, ocultando el problema.
- Para cambiar la referencia hay que lanzar un análisis con `baseline_from`/`baseline_to`.
- La clasificación (tipo, severidad, confianza) es del análisis y **no cambia con el filtro**. Si el rango consultado difiere del período analizado, la respuesta lo indica (`analysis_period`) para que la UI muestre el aviso.

## De dónde sale la anomalía y el estado en /meters
- Cada fila de `/meters` es `meters` + `meter_baselines` + `anomalies` del análisis vigente de ese medidor (LEFT JOIN; sin anomalía → `anomaly: null`).
- `status` lo escribe el motor al terminar el análisis, según la anomalía vigente:

| Anomalía vigente | status |
|---|---|
| REAL_ANOMALY · HIGH | CRITICAL |
| REAL_ANOMALY · MEDIUM/LOW, EXPLAINABLE_ANOMALY, DATA_QUALITY | ALERT |
| FALSE_POSITIVE, o sin anomalía | OK |
| Nunca analizado | UNKNOWN |

- Ejemplo esperado: M-109 CRITICAL · M-112 ALERT · M-104 ALERT · M-106 OK (no escalar).
- El status **no cambia con el filtro de fechas**.
- `sort=severity`: HIGH > MEDIUM > LOW > sin anomalía; desempate por `priority_score`.
- Filtros: normal = OK, alert = ALERT, critical = CRITICAL.

## Contratos clave
`GET /meters`:
```json
{"period":{"from":"","to":""},
 "items":[{"meter_id":"M-109","name":"","consumption_kwh":2180,"baseline_kwh":1070,
   "variation_pct":103.7,"status":"CRITICAL","analysis_id":3,
   "baseline_ref":{"window_from":"","window_to":"","method":"HOURLY_MEDIAN"},
   "analysis_period":{"from":"","to":""},
   "anomaly":{"id":12,"type":"REAL_ANOMALY","severity":"HIGH","confidence":0.96,"priority_rank":1,"in_range":true,"active_from":"","active_to":""}}]}
```
Medidor sin análisis: `baseline_kwh`, `variation_pct`, `analysis_id` y `anomaly` en `null`; `status: "UNKNOWN"`.

`GET /meters/:meterId`: igual que un item de la lista, más `baseline{hourly_profile,voltage,current,power_factor,change_point_at}`, los valores actuales de V/I/PF del período y `latest_anomaly`.

`GET /anomalies/:id`:
```json
{"id":12,"analysis_id":3,"meter_id":"M-109","type":"REAL_ANOMALY","severity":"HIGH",
 "confidence":0.96,"confidence_label":"HIGH","priority_rank":1,"priority_score":5.8,
 "status":"OPEN","detected_at":"","period_from":"","period_to":"","change_point_at":"",
 "active_from":"","active_to":"","duration_hours":0,
 "baseline_kwh":1070,"actual_kwh":2180,"variation_pct":103.7,
 "reason":"","recommended_action":"","explanation_source":"TEMPLATE",
 "signals":[{"signal":"PERSISTENT_SHIFT","observed":103.7,"threshold":25,"detail":""}],
 "variables":[{"variable":"current_a","baseline":0,"actual":0,"delta_pct":0,"changed":true}],
 "events":[{"id":0,"type":"","description":"","relation":"RELATED","offset_hours":0}],
 "evidence":{"decision_path":[],"confidence_breakdown":{},"data_quality_issues":[],"affected_readings":{}}}
```
`GET /ai/analysis/:id`:
```json
{"id":3,"status":"RUNNING","stage":"CORRELATION","progress":0.55,
 "scope":{"meter_ids":null,"from":"","to":"","baseline_from":null,"baseline_to":null},
 "started_at":"","finished_at":null,"duration_ms":null,"engine_version":"",
 "readings_analyzed":4032,"meters_analyzed":12,
 "summary":{"anomalies":4,"priority":2,"avg_confidence":0.9,"message":"4 anomalías detectadas · 2 requieren atención prioritaria"},
 "stage_log":[{"stage":"READINGS","ms":120}]}
```
`GET /dashboard/summary`:
```json
{"data_range":{"from":"","to":""},"period":{"from":"","to":""},
 "meters":12,"total_consumption_kwh":0,"anomalies":4,"high_priority":2,
 "avg_confidence":0.9,"last_analysis":{"at":"","status":"COMPLETED"},
 "by_meter":[{"meter_id":"M-109","consumption_kwh":2180,"share_pct":18.4,"status":"CRITICAL","severity":"HIGH"}]}
```
- `by_meter`: los 12 medidores, consumo del rango, ordenados de mayor a menor (alimenta el pastel); `share_pct` sobre el total del rango.
- Con `from`/`to`: cambian `total_consumption_kwh` y `by_meter`, y `anomalies`, `high_priority` y `avg_confidence` cuentan solo las anomalías vigentes que se solapan con el rango.
- `data_range` = primera y última lectura; define los límites del selector de fechas.

## Notas
- Análisis en goroutine; el frontend hace polling cada ~1 s.
- Si aún no hay análisis: `/anomalies` devuelve `[]` y el dashboard indica "Sin análisis".
