# 02 · Motor de anomalías

Paquete `internal/engine`. Determinista, testeable, sin dependencias de red.

## Pipeline (mapea a los estados de Run AI Analysis)
Lecturas → Baseline → Detección → Correlación → Eventos → Explicación → Recomendación

## 1. Baseline
- Por medidor: perfil por hora del día (mediana) sobre el período previo al cambio.
- Detección de punto de cambio: CUSUM o comparación de medianas móviles diarias; si no hay cambio, baseline = primeros 7 días.
- `baseline_kwh` = perfil extrapolado a los 14 días; `variation_pct = (actual - baseline) / baseline * 100`.
- Robusto: usar mediana/MAD, no media/desviación.

## 2. Detección (señales por medidor)
| Señal | Método |
|---|---|
| Cambio persistente | variation_pct > umbral y sostenido ≥ N días |
| Spikes / outliers | z-score robusto (MAD) > 3.5 por hora |
| Patrón horario | desviación del perfil horario vs baseline |
| Calidad de datos | nulos, duplicados, huecos, valores fuera de rango, saltos imposibles |
| Consistencia eléctrica | potencia implícita (V·I·PF) vs kWh: la razón debe ser estable en baseline; si se rompe → inconsistente |

Los umbrales **no son constantes fijas**: se calculan por medidor según su propio ruido (ver "Cálculo de umbrales" al final de esta sección).

## 3. Correlación
Para cada medidor con señal: ¿cambiaron voltaje, corriente o factor de potencia junto con el consumo? Registrar deltas en evidencia.

## 4. Eventos
Buscar eventos en `events` del medidor dentro de ±24 h del punto de cambio.
- Evento de aumento de carga (ej. nueva línea) + aumento → explica.
- Evento de parada/mantenimiento + caída → explica.

## 5. Clasificación (árbol, en orden)
1. Consumo estable pero eléctricas inconsistentes o datos defectuosos → **DATA_QUALITY**, High si la inconsistencia es fuerte y persistente.
2. Cambio significativo + evento que lo explica:
   - aumento por nueva carga → **EXPLAINABLE_ANOMALY**, Medium
   - cambio por parada programada → **FALSE_POSITIVE**, Low
3. Cambio significativo sin evento + cambios eléctricos coherentes → **REAL_ANOMALY**, High.
4. Cambio significativo sin evento y sin cambios eléctricos → REAL_ANOMALY, Medium.
5. Sin señales → normal (sin anomalía).

## 6. Severidad y prioridad
- `priority_score = severity_weight (H=3,M=2,L=1) × confidence × min(|variation|/100, 2)`; DATA_QUALITY y FALSE_POSITIVE usan magnitud normalizada propia.
- Orden final: score descendente. La lista debe poner primero la anomalía real de mayor impacto.
- FALSE_POSITIVE nunca supera a un REAL_ANOMALY.

## 7. Confianza (0–1)
Combinación explicable: fuerza de la señal (variación/z) + número de señales concordantes + presencia/ausencia de evento + calidad de datos del medidor. Documentar la fórmula en código; redondear a 2 decimales. Etiqueta: <0.6 Baja · 0.6–0.85 Media · >0.85 Alta.

## 8. Explicación y recomendación
- Plantillas por tipo, rellenadas con la evidencia (cifras reales). Ej. REAL_ANOMALY: "Consumo {x}% por encima del baseline sin evento conocido; {V/I/PF} cambiaron {…}."
- Acciones: REAL → "Investigar medidor e instalación" · DATA_QUALITY → "Validar sensor/cableado" · EXPLAINABLE → "Validar operación" · FALSE_POSITIVE → "No escalar".
- Opcional (flag `LLM_EXPLAIN=1`): un LLM reescribe `reason` solo con los números de la evidencia; si falla, se usa la plantilla.

## 9. Salida (por anomalía)
```json
{"meter_id":"","anomaly":true,"type":"","severity":"","confidence":0.0,
 "reason":"","recommended_action":"","evidence":{}}
```

## 10. Cálculo de umbrales

**Principio:** `umbral = max(piso, k × ruido propio del medidor)`.
- El **ruido propio** evita falsos positivos en medidores naturalmente variables.
- El **piso** evita alertar por cambios irrelevantes en medidores muy estables.
- Todo se estima con estadística robusta (mediana y MAD, `MAD_norm = 1.4826 × MAD`) sobre la ventana baseline del propio medidor.

| Umbral | Cálculo | Piso / constante | Origen |
|---|---|---|---|
| Ventana baseline | Lecturas previas al punto de cambio; mínimo 3 días. Si no hay cambio, primeros 7 días | 3 días mín. | Regla de diseño |
| Ruido diario `cv_i` | `MAD_norm(consumo diario en baseline) / mediana` | — | Datos |
| **Variación significativa** `T_var,i` | `max(15 %, 3 × cv_i × 100)` | 15 % | Datos + piso |
| **Outlier horario** | Residuo = lectura − perfil horario del baseline. `z_mod = 0.6745 × (r − mediana_r) / MAD_r`; outlier si `|z_mod| > 3.5` | 3.5 | Literatura (Iglewicz-Hoaglin) |
| **Punto de cambio (CUSUM)** | `σ = MAD_norm(residuos baseline)`; `k = 0.5σ`, `h = 5σ` | 0.5σ / 5σ | Convención estándar de CUSUM |
| **Persistencia** | El cambio se mantiene ≥ 48 h continuas (`N = 48 h / intervalo`) | 48 h | Regla de diseño |
| **Consistencia eléctrica** `T_elec,i` | `r_t = kWh_t / (V·I·PF)`; `T_elec,i = max(15 %, 3 × cv(r)_i × 100)`. Se compara `r_t` contra la mediana de `r` en baseline | 15 % | Datos + piso |
| **Calidad de datos** | Límites físicos fijos (PF ∈ [0,1], V > 0, I ≥ 0, kWh ≥ 0) + rango plausible P0.5–P99.5 de la flota con margen ±10 %; nulos/duplicados/huecos > 2 % | ver columna | Física + datos |

Notas:
- **Consistencia eléctrica:** comparar `r_t` contra el baseline del mismo medidor hace que la constante de fase (monofásico o trifásico, √3) se cancele; no hace falta conocerla.
- **Ventana corta:** si la ventana baseline tiene < 3 días, `cv_i` se sustituye por la mediana de `cv` de la flota.
- **Trazabilidad:** los umbrales efectivos por medidor y su origen (`CALCULATED` o `FALLBACK`) se guardan en `analyses.config_json`.

### Reglas de calibración
1. **Nunca se ajustan con `expected_results.csv`.** Los umbrales salen solo de `readings.csv` y de estas fórmulas; `cmd/eval` solo reporta.
2. **Prueba de sensibilidad (sin ground truth):** variar cada umbral ±30–50 %. Si la clasificación de los medidores cambia, el caso está en el borde y se revisa el método, no el número.
3. **Pisos y constantes** viven en `config.go` con comentario de su origen; cambiarlos requiere actualizar esta sección.
4. Tests unitarios con datos sintéticos: un medidor estable, uno ruidoso y uno con cambio, para verificar que el umbral se adapta.

