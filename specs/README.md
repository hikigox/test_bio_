# Specs · AI Energy Management Platform

Orden de lectura y qué pegar en cada chat:

| # | Archivo | Contenido | Se usa en |
|---|---|---|---|
| 00 | overview.md | Objetivo, stack (Go), reglas no negociables | Siempre (contexto corto) |
| 01 | data-model.md | Esquema SQLite, relaciones, enums | T0–T2, T7–T8 |
| 02 | anomaly-engine.md | Baseline, señales, clasificación, umbrales | T0–T6 |
| 03 | api.md | Endpoints, contratos, filtro de fechas, alcance del análisis | T7–T9, T10–T14 |
| 04 | frontend.md | Pantallas, pastel, filtro de fechas global | T10–T14 |
| 05 | testing-demo.md | Casos esperados, evaluación, guion de demo | T6, T9, T16 |
| 06 | docker.md | Levantamiento con Docker Compose | T17–T20 |
| — | tasks.md | Checklist de tareas con spec y modelo sugerido | Siempre |

Regla: cada tarea carga solo `00` + las specs de su fila. `expected_results.csv` nunca entra al repo ni a la app.
