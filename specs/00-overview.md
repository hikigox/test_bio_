# 00 · Overview

## Objetivo
MVP de AI Energy Management: convertir lecturas de medidores en una decisión operativa.
Ciclo: DATOS → ANÁLISIS → ANOMALÍA → EXPLICACIÓN → PRIORIZACIÓN → ACCIÓN.

## Alcance
- 12 medidores, 14 días, 4.032 lecturas (intervalo esperado: 1 h... verificar en CSV).
- Variables: consumption_kwh, voltage_v, current_a, power_factor.
- Demo de 5–10 min: Login → Dashboard → M-109 → Run AI Analysis → Anomalía → Explicación → Acción.

## Stack (decisión)
- Backend: **Go** (net/http o chi) + **SQLite** (modernc.org/sqlite, sin CGO).
- Frontend: React + Vite + Tailwind + Recharts.
- Motor de anomalías: **híbrido determinista** (estadística + reglas + eventos). LLM opcional solo para redactar la explicación; nunca decide ni inventa cifras.
- Despliegue: **Docker Compose** (ver 06-docker.md).

### ¿Go o Python? Decisión: Go
- **Requisito de la prueba:** el enunciado pide "usar como lenguaje go". Cambiar a Python arriesga incumplirlo.
- **Suficiencia técnica:** el motor solo necesita mediana/MAD, z-score, CUSUM y reglas sobre 4.032 filas. Go lo resuelve con la stdlib y algo de código propio; no hace falta pandas ni scikit-learn.
- **Ventajas:** un binario único, imagen Docker pequeña, arranque rápido y concurrencia simple para el análisis asíncrono.
- **Coste asumido:** menos librerías de estadística que Python (se implementan ~100 líneas propias, con tests).
- **Cuándo reconsiderar:** solo si se quisiera Isolation Forest u otro modelo ML. Aun así, sería un servicio Python opcional aparte (sidecar en Docker), sin tocar el resto. Fuera de alcance del MVP.

## Reglas no negociables
1. `expected_results.csv` NO se carga en la app ni en la BD. Solo lo lee `cmd/eval` (script aparte).
2. Toda anomalía debe traer evidencia numérica que soporte su `reason`.
3. Un cambio explicado por un evento conocido no se escala como anomalía real.
4. El motor no contiene lógica hardcodeada por `meter_id`: las reglas son generales.

## Preguntas que la plataforma responde
Qué pasa con los medidores · qué se sale de lo esperado · real, explicable o calidad de datos · cuál investigar primero · por qué · qué acción.

## Estructura del repo
```
/specs  /backend (cmd/api, cmd/eval, internal/{store,engine,api})  /frontend  /data (CSVs)
docker-compose.yml  Makefile  README.md
```
La estructura se mantiene; se añaden los archivos de contenedores (ver 06-docker.md).

## Criterio de éxito
Un evaluador entiende en minutos qué requiere atención y por qué. Ver criterios medibles en 05-testing-demo.md.
