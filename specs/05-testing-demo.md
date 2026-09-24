# 05 · Testing, evaluación y demo

## Casos esperados (criterios de aceptación)
| Medidor | Caso | Resultado esperado |
|---|---|---|
| M-109 | +103,7%, sin evento, cambios eléctricos | REAL_ANOMALY · HIGH · confianza ≥ 0.9 · **#1 en prioridad** |
| M-112 | Consumo estable, eléctricas inconsistentes | DATA_QUALITY · HIGH |
| M-104 | Aumento por nueva línea productiva | EXPLAINABLE_ANOMALY · MEDIUM |
| M-106 | Cambio por parada programada | FALSE_POSITIVE · LOW (no escalar) |
| Resto | Sin señales | Sin anomalía |
Resultado global: 4 anomalías detectadas, 2 de atención prioritaria.

## Pruebas
- **Unitarias (engine)**: baseline, punto de cambio, z-score robusto, consistencia eléctrica, árbol de clasificación, orden de prioridad. Table-driven.
- **Integración (API)**: cada endpoint con BD en memoria; POST /ai/analyze → polling → COMPLETED.
- **Frontend**: smoke test del flujo principal (opcional: Playwright/Vitest).
- **Sin datos de evaluación en tests unitarios**: usar fixtures sintéticos; los casos reales se validan con `cmd/eval`.

## cmd/eval
- Único lugar que lee `expected_results.csv` (ruta por flag `--expected`, fuera del repo público).
- Corre el motor y compara: detecta M-109, prioriza M-109, M-106 no es real, M-112 es data quality, evidencia presente, acción coherente.
- Imprime tabla de puntaje (rúbrica: 30/25/15/10/10/10).

## Guion de demo (5–10 min)
1. Login → Dashboard (12 medidores, "Sin análisis").
2. Meters: mostrar M-109 (+103,7%) y filtros.
3. Detalle M-109: gráfica vs baseline, corriente/FP.
4. **Run AI Analysis**: etapas → "4 anomalías · 2 prioritarias".
5. Anomalías: M-109 primero; contraste con M-106 (no escalar) y M-112 (calidad de datos).
6. Investigación M-109: evidencia, variables, acción recomendada.
7. Cierre: ciclo Datos → Acción y cómo se evitan falsos positivos.

## Documentación
README con: arranque (`make run`), arquitectura, decisiones del motor, umbrales, límites conocidos.
