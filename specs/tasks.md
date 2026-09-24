# Tasks (un chat por bloque; pegar solo la spec indicada)

Modelo sugerido: S = Sonnet · O = Opus (solo puntual) · H = Haiku

## Fase 0 · Datos
- [ ] T0 Inspeccionar CSV (columnas, intervalo, calidad) y ajustar 01 y 02 — S — specs: 01, 02

## Fase 1 · Motor
- [ ] T1 Scaffold Go + carga de CSV a SQLite (idempotente) + esquema completo — S — 01
- [ ] T2 Baseline (perfil horario guardado) y punto de cambio — S/O — 02 §1
- [ ] T3 Señales: outliers, horario, calidad, consistencia eléctrica — S — 02 §2–3
- [ ] T4 Eventos + clasificación + severidad/confianza/prioridad — O — 02 §4–7
- [ ] T5 Explicación/recomendación por plantillas — S — 02 §8
- [ ] T6 Tests unitarios del motor + cmd/eval — S — 05

## Fase 2 · API
- [ ] T7 Endpoints de lectura con filtro de fechas: baseline por rango, ventana activa, `by_meter` — S — 03
- [ ] T8 POST /ai/analyze con alcance (meter_ids, fechas, baseline manual) + polling + login — S — 03
- [ ] T9 Tests de integración — H/S — 05

## Fase 3 · Frontend
- [ ] T10 Scaffold, login, layout, cliente API, filtro de fechas global (DateRangeContext) — S — 04
- [ ] T11 Dashboard: KPIs, pastel por medidor, Run AI Analysis (stepper) — S — 04
- [ ] T12 Meters (filtros, búsqueda, orden) — S — 04
- [ ] T13 Detalle de medidor: gráficas, banda de anomalía, "Analizar este medidor" — S — 04
- [ ] T14 Anomalías (columna Cuándo) + Investigación — S — 04

## Fase 4 · Cierre
- [ ] T15 README, Makefile, seed de usuario — H — 05
- [ ] T16 Ensayo de demo y ajustes — S — 05

## Fase 5 · Docker (transversal, puede hacerse tras T1)
- [ ] T17 Dockerfile backend (multi-stage, sin CGO, no root) + `/healthz` — H/S — 06
- [ ] T18 Dockerfile frontend + nginx con proxy `/api` — H — 06
- [ ] T19 docker-compose.yml, `.env.example`, `.dockerignore`, Makefile — H — 06
- [ ] T20 Verificar arranque desde clon limpio y que expected_results.csv no entra en la imagen — S — 06
