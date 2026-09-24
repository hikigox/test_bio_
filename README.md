# AI Energy Management — MVP

Plataforma de gestión energética con IA: convierte lecturas de medidores en
decisiones operativas. Ciclo: **Datos → Análisis → Anomalía → Explicación →
Priorización → Acción**.

## Arranque

### Con Docker (recomendado)

```bash
cp .env.example .env   # ajustar si hace falta; los defaults funcionan para la demo
make up
```

Login en `http://localhost:3000` con el usuario demo:
- Email: `demo@energy.local`
- Password: `demo1234`

(Ver [docs de Docker](specs/06-docker.md) para detalle de servicios, variables
de entorno y `make reset`/`make logs`/`make eval`.)

### Modo desarrollo (sin Docker)

Backend (desde `backend/`):
```bash
go run ./cmd/api
```
Escucha en `:8080` por defecto (`PORT`). Carga `../data/readings.csv` y
`../data/events.csv` automáticamente en el primer arranque (BD vacía) y crea
el usuario demo — ver `backend/cmd/api/main.go` (`seedIfEmpty`).

Frontend (desde `frontend/`, en otra terminal):
```bash
npm install
npm run dev
```
Sirve en `http://localhost:5173` y llama al backend en `:8080` directamente
(sin proxy nginx — eso solo existe en la imagen Docker de producción).

## Arquitectura

- **Backend** (`backend/`, Go, sin CGO): API REST (`chi` router) +
  **motor de anomalías determinista** (estadística + reglas + eventos, sin
  ML) + SQLite (`modernc.org/sqlite`) como única fuente de estado.
  - `cmd/api`: servidor HTTP.
  - `cmd/eval`: script de evaluación aparte que compara contra
    `expected_results.csv` (nunca se carga en la app — ver Reglas no
    negociables en `specs/00-overview.md`).
  - `internal/engine`: motor puro (sin I/O) — baseline, detección de
    señales, clasificación, explicación.
  - `internal/api`: HTTP handlers, auth JWT, orquestación del análisis
    asíncrono (goroutine + polling).
  - `internal/store`: acceso a SQLite, carga de CSVs.
- **Frontend** (`frontend/`, React + Vite + TypeScript + Tailwind +
  Recharts): SPA con Dashboard, Medidores, Anomalías, Detalle de medidor y
  de anomalía. Filtro de fechas global (`DateRangeContext`, respaldado en la
  URL). Cliente API tipado con manejo de 401 → logout automático.

## Decisiones del motor de anomalías

- **Baseline**: perfil horario (24 valores, promedio por hora del día) sobre
  una ventana de referencia. El punto de cambio (`change_point_at`) se
  detecta con **CUSUM** sobre residuales normalizados por
  **MAD** (`MADNorm = 1.4826 * MAD`, estimador robusto de σ frente a
  outliers), con holgura `k = 0.5σ` y arranque de acumulación después de la
  ventana de referencia (`backend/internal/engine/baseline.go`).
- **Detección de señales** (`internal/engine/signals.go`):
  outliers por z-score robusto (umbral `3.5`), calidad de datos (lecturas
  faltantes/erróneas > `2%`), inconsistencia eléctrica (voltaje/corriente/FP
  fuera de rango esperado), patrón horario persistente.
- **Clasificación** (`internal/engine/classify.go`): árbol de reglas sobre
  las señales + eventos correlacionados (ventana de correlación: `24h`,
  `internal/engine/events.go`) decide `REAL_ANOMALY` /
  `EXPLAINABLE_ANOMALY` / `DATA_QUALITY` / `FALSE_POSITIVE`. Un cambio
  explicado por un evento conocido no se escala como anomalía real (spec
  00, regla no negociable #3). Variables con variación > `10%` se marcan
  `changed` (`internal/engine/correlation.go`).
- **Confianza y prioridad**: `ConfidenceBreakdown` combina señal, eventos,
  concordancia y calidad de datos en un score 0–1; la prioridad ordena por
  ese score combinado con severidad.
- **Explicación**: 100% por plantillas (`explanation_source` siempre
  `"TEMPLATE"`, `backend/internal/api/analysis_runner.go:361`). El motor
  **no** contiene lógica hardcodeada por `meter_id` (spec 00, regla #4).

## Límites conocidos

- **Sin integración LLM**: `LLM_EXPLAIN`/`ANTHROPIC_API_KEY` existen como
  variables de entorno en `docker-compose.yml` pero el backend no las lee —
  la explicación es siempre por plantillas. La spec original marca esto como
  opcional ("LLM opcional solo para redactar la explicación; nunca decide ni
  inventa cifras") — quedó fuera de alcance del MVP, no es un bug.
- **Sin smoke test end-to-end en CI**: el smoke test Playwright (ver
  `frontend/e2e/`) existe pero no corre automáticamente en ningún pipeline —
  se ejecuta manualmente (`npm run test:e2e` desde `frontend/`, requiere los
  servidores de desarrollo levantados).
- **Bundle de frontend sin code-splitting**: ~675kB minificado en un solo
  chunk (`npm run build` avisa sobre esto). No afecta la demo; sería el
  primer punto de optimización si esto pasara a producción real.
- **Motor sin ML**: decisión deliberada (ver `specs/00-overview.md`,
  "¿Go o Python?") — estadística clásica (MAD/CUSUM/z-score) + reglas,
  suficiente para 12 medidores / 4032 lecturas. Reconsiderar solo si se
  necesitara un modelo tipo Isolation Forest, como sidecar aparte.

## Tests

```bash
cd backend && go test ./...
cd frontend && npx tsc -b --force && npx vitest run
```

`make test` corre los tests de backend dentro de un contenedor Docker
(sin necesidad de tener Go instalado localmente).
