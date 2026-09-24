# Cierre y Demo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cerrar el MVP con documentación real (README), un ensayo verificado del guion de demo de 7 pasos con cualquier papercut encontrado corregido, y un smoke test automatizado del flujo principal que cierre el hueco de verificación de browser real que quedó abierto al final de la Fase 3 (frontend).

**Architecture:** No se agrega ninguna pieza de arquitectura nueva — este plan es de cierre: documentación, verificación manual del flujo end-to-end contra los servidores de desarrollo reales (no Docker; eso es la Fase 5), y un smoke test Playwright opcional-mandatado por spec 05 ("opcional: Playwright/Vitest") que ejercita el guion real contra la app real.

**Tech Stack:** Markdown (README), Playwright (smoke test, nueva dependencia dev-only del frontend), los mismos backend Go + frontend React/Vite ya construidos.

**Spec:** specs/05-testing-demo.md (además specs/00-overview.md para arquitectura/decisiones a documentar, specs/02-anomaly-engine.md para los umbrales reales a documentar en README — los valores exactos ya implementados se listan en este plan para que el ejecutor no tenga que releer el motor completo)

## Estado real del repo (contexto para el ejecutor — no volver a scaffoldear esto)

- `README.md` es solo un stub: `# test_bio`.
- `Makefile` ya existe y ya implementa `up`/`down`/`reset`/`logs`/`test`/`eval` (ver Fase 5 para verificación de que funcionan de punta a punta con Docker — este plan documenta su uso, no los reescribe).
- El usuario demo y la carga de CSVs ya son automáticos: `backend/cmd/api/main.go`'s `seedIfEmpty()` carga `readings.csv`/`events.csv` y crea el usuario demo (`DEMO_EMAIL`/`DEMO_PASSWORD`, default `demo@energy.local`/`demo1234`) solo si la tabla `readings` está vacía. No hay comando `make seed` separado ni falta ninguno — no crear uno.
- `cmd/eval` (backend/cmd/eval/main.go, 268 líneas + tests) ya está implementado y probado — no es parte de este plan.
- El motor de explicación es 100% por plantillas: `explanation_source` se graba siempre como `'TEMPLATE'` (`backend/internal/api/analysis_runner.go:361`, hardcodeado). Las variables de entorno `LLM_EXPLAIN`/`ANTHROPIC_API_KEY` existen en `docker-compose.yml` pero **`backend/cmd/api/main.go` no las lee en ningún lado** — no hay integración LLM implementada. Documentar esto como decisión/límite conocido en el README (Task 1), no implementarla — está fuera de alcance del MVP (spec 00: "LLM opcional solo para redactar la explicación; nunca decide").

## Global Constraints

- El README debe explicar cómo arrancar la app con Docker (`make up`, ver Fase 5) y en modo desarrollo local sin Docker (backend: `go run ./cmd/api` desde `backend/`; frontend: `npm run dev` desde `frontend/`) — spec 05 pide "arranque" sin especificar cuál; documentar ambos porque ambos existen y funcionan hoy.
- Todo comando documentado en el README debe ejecutarse tal cual está escrito contra este repo — no placeholders de rutas ni de credenciales (usar los valores default reales: `demo@energy.local` / `demo1234`).
- El smoke test (Task 3) corre contra los servidores de desarrollo reales (`npm run dev` + `go run ./cmd/api`), no contra Docker — Docker se verifica aparte en la Fase 5.
- No introducir cambios de comportamiento en backend/frontend salvo los papercuts triviales que aparezcan durante el ensayo de demo (Task 2) — cualquier hallazgo que no sea trivial (una palabra, un `null`-guard de una línea) se documenta como límite conocido en el README en vez de arreglarse aquí.

## Review Focus

- Un comando de arranque copiado literalmente del README que no corre tal cual (typo, ruta relativa incorrecta, variable de entorno faltante) — el ejecutor debe correr cada comando documentado, no solo escribirlo.
- El guion de demo de 7 pasos ejecutado contra la app real puede revelar el mismo tipo de gap que ya apareció 2 veces en la Fase 3 (spec dice X, código no lo tiene) — cualquier paso que no produzca lo que el guion espera debe documentarse explícitamente en el reporte de la Task 2, no silenciarse.
- El smoke test Playwright debe usar selectores estables (texto visible, `data-testid` si hace falta agregarlo) y no timings arbitrarios — un `waitFor` con timeout corto en un flujo async (login → análisis) es la causa más común de flakiness en este tipo de test.
- El README documenta valores default de credenciales demo — si en algún punto se cambian los defaults en `main.go` sin actualizar el README, el README queda mintiendo; Task 1 debe citar la fuente (`backend/cmd/api/main.go:27-28`) en un comentario para que el próximo cambio sea encontrable.
- El smoke test no debe depender de datos que cambien entre corridas (ej. IDs de anomalía autoincrementales cuya "M-109 es #1" depende de que la BD esté recién sembrada) — usar aserciones sobre texto/estructura visible, no sobre IDs numéricos específicos salvo que se resetee la BD antes del test.

---

### Task 1: README.md completo

**Files:**
- Modify: `README.md` (reemplaza el stub `# test_bio`)

**Interfaces:**
- Consumes: nada de código — este task es documentación pura, pero cita comandos y rutas reales del repo que deben verificarse ejecutándolos.
- Produces: nada que otro task consuma directamente, salvo que Task 2's demo rehearsal debe seguir las instrucciones de arranque de este README tal cual quedan escritas (para validar que son correctas).

- [ ] **Step 1: Verificar los comandos de arranque local antes de documentarlos**

Desde la raíz del repo:

```bash
cd backend && go run ./cmd/api &
sleep 2
curl -s http://localhost:8080/healthz
kill %1
```

Expected: `{"status":"ok"}`. Si falla, el problema es del entorno (Go no instalado, puerto ocupado) — resolverlo antes de continuar, no lo documentes como "funciona" si no corrió.

```bash
cd frontend && npm install && npm run dev &
sleep 3
curl -s http://localhost:5173 | head -5
kill %1
```

Expected: HTML del `index.html` de Vite (contiene `<div id="root">`).

- [ ] **Step 2: Escribir el README**

Reemplazar todo el contenido de `README.md` con:

```markdown
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
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: write real README (arranque, arquitectura, decisiones del motor, límites)"
```

---

### Task 2: Ensayo del guion de demo (7 pasos, spec 05)

**Files:**
- Create: `.superpowers/sdd/2026-09-24-cierre-demo/demo-rehearsal-report.md` (si se ejecuta vía subagent-driven-development; si se ejecuta inline, el reporte puede ir directo en la respuesta al usuario)
- Modify: cualquier archivo de `frontend/src/` o `backend/` **solo** si aparece un papercut trivial (ver Global Constraints — una palabra, un null-guard de una línea)

**Interfaces:**
- Consumes: el README de Task 1 (las instrucciones de arranque deben ser las que se usan acá, verificando que son correctas en la práctica).
- Produces: un reporte pass/fail por cada uno de los 7 pasos del guion, que Task 3 (smoke test) usa como referencia de qué automatizar.

- [ ] **Step 1: Levantar ambos servidores en modo desarrollo**

```bash
cd backend && go run ./cmd/api &
cd frontend && npm run dev &
```

Esperar a que ambos respondan:
```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:5173 | head -1
```

- [ ] **Step 2: Ejecutar el guion de demo de spec 05, paso a paso, en un navegador real**

Para cada paso, anotar en el reporte si lo observado coincide con lo esperado:

1. **Login → Dashboard**: entrar a `http://localhost:5173`, login con
   `demo@energy.local` / `demo1234`. Esperado: Dashboard muestra 12
   medidores y KPI "Último análisis" = "Sin análisis" (recién sembrada la
   BD — si ya se corrió un análisis antes en esta BD, esperar en cambio la
   fecha del último análisis real, no "Sin análisis"; anotar cuál caso
   aplicó).
2. **Meters**: ir a la pantalla de Medidores, localizar `M-109`, confirmar
   que su variación mostrada es `+103,7%` (formato es-ES), probar al menos
   un filtro.
3. **Detalle M-109**: entrar al detalle, confirmar que la gráfica "Consumo
   vs. baseline" muestra la línea de baseline superpuesta (agregada en la
   Fase 3, fix wave FIX 3) y que las gráficas de corriente/factor de
   potencia están presentes.
4. **Run AI Analysis**: disparar el análisis desde el Dashboard (sin
   `meter_ids`, alcance completo), observar el stepper de etapas, esperar a
   que termine. Esperado final: "4 anomalías · 2 prioritarias".
5. **Anomalías**: ir a la pantalla de Anomalías, confirmar que `M-109`
   aparece primero (prioridad más alta), y contrastar contra `M-106`
   (`FALSE_POSITIVE`, no debe verse escalado) y `M-112` (`DATA_QUALITY`).
6. **Investigación M-109**: abrir el detalle de la anomalía de `M-109`,
   confirmar que se ven evidencia, variables comparativas y acción
   recomendada (agregado en la Fase 3, fix wave FIX 2).
7. **Cierre**: confirmar que el ciclo completo (Datos → Acción) es
   narrable con lo que la UI muestra — sin pasos faltantes evidentes.

- [ ] **Step 3: Documentar cualquier gap encontrado**

Si algún paso no produce lo esperado: si es un papercut trivial (copy,
formato, un `null` sin guardar), corregirlo directamente y commitear con
un mensaje que describa el fix. Si es más grande, NO intentar arreglarlo en
este task — documentarlo en el reporte como límite conocido (y considerar
agregarlo al README's "Límites conocidos" si aplica).

- [ ] **Step 4: Apagar los servidores y confirmar árbol limpio**

```bash
kill %1 %2 2>/dev/null
git status --porcelain
```

- [ ] **Step 5: Commit (si hubo fixes)**

```bash
git add -A
git commit -m "fix: address papercuts found during demo script rehearsal"
```

(Si no hubo fixes, no hay nada que commitear en este step — el reporte de
hallazgos es el único artefacto.)

---

### Task 3: Smoke test Playwright del flujo principal

**Files:**
- Create: `frontend/playwright.config.ts`
- Create: `frontend/e2e/demo-flow.spec.ts`
- Modify: `frontend/package.json` (agrega `@playwright/test` a devDependencies, agrega script `test:e2e`)
- Modify: `.gitignore` en `frontend/` (si no existe ya, ignorar `frontend/test-results/` y `frontend/playwright-report/`)

**Interfaces:**
- Consumes: el guion de 7 pasos verificado en Task 2 — este test automatiza el subconjunto que es determinista (login, navegación, disparar análisis, ver resultado), no cada aserción textual del ensayo manual.
- Produces: nada que otro task consuma — este es el cierre de la Fase 4/spec 05's "opcional: Playwright/Vitest" y del hueco que la revisión final de la Fase 3 dejó marcado como no aceptable de ignorar silenciosamente.

- [ ] **Step 1: Instalar Playwright**

```bash
cd frontend
npm install -D @playwright/test
npx playwright install --with-deps chromium
```

- [ ] **Step 2: Escribir el config**

```typescript
// frontend/playwright.config.ts
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL: "http://localhost:5173",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "cd ../backend && go run ./cmd/api",
      url: "http://localhost:8080/healthz",
      reuseExistingServer: true,
      timeout: 20_000,
    },
    {
      command: "npm run dev",
      url: "http://localhost:5173",
      reuseExistingServer: true,
      timeout: 20_000,
    },
  ],
});
```

- [ ] **Step 3: Escribir el test del flujo principal**

```typescript
// frontend/e2e/demo-flow.spec.ts
import { test, expect } from "@playwright/test";

test("login, run analysis, and reach M-109's investigation screen", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel(/correo|email/i).fill("demo@energy.local");
  await page.getByLabel(/contraseña|password/i).fill("demo1234");
  await page.getByRole("button", { name: /entrar|iniciar sesión/i }).click();

  await expect(page).toHaveURL(/\/dashboard/);
  await expect(page.getByText(/12/).first()).toBeVisible();

  await page.getByRole("button", { name: /run ai analysis|ejecutar análisis/i }).click();
  await expect(page.getByText(/completado|completed/i)).toBeVisible({ timeout: 25_000 });

  await page.getByRole("link", { name: /anomalías/i }).click();
  await expect(page).toHaveURL(/\/anomalies/);
  await expect(page.getByText("M-109").first()).toBeVisible();

  await page.getByText("M-109").first().click();
  await expect(page).toHaveURL(/\/anomalies\/\d+/);
  await expect(page.getByText(/acción recomendada|recommended action/i)).toBeVisible();
});
```

Nota: los `getByLabel`/`getByRole` de arriba son selectores por texto visible
(no `data-testid`) siguiendo el copy real de la UI ya implementada — si al
correr el test algún selector no matchea el texto/label exacto que la UI
usa, ajustarlo al texto real observado en `frontend/src/pages/LoginPage.tsx`
y `DashboardPage.tsx`, no inventar texto nuevo en la UI para que el test
pase.

- [ ] **Step 4: Agregar el script npm**

En `frontend/package.json`, dentro de `"scripts"`:

```json
"test:e2e": "playwright test"
```

- [ ] **Step 5: Correr el test y verificar que pasa**

```bash
cd frontend && npm run test:e2e
```

Expected: 1 passed. Si falla por timeout en el análisis (25s puede no
alcanzar dependiendo de la máquina), subir el timeout del segundo
`expect` — no bajar la calidad de la aserción quitándola.

- [ ] **Step 6: Ignorar artefactos de Playwright en git**

Agregar a `frontend/.gitignore` (crear si hace falta, o revisar si ya
cubre esto):
```
test-results/
playwright-report/
```

- [ ] **Step 7: Commit**

```bash
git add frontend/playwright.config.ts frontend/e2e/demo-flow.spec.ts frontend/package.json frontend/package-lock.json frontend/.gitignore
git commit -m "test(frontend): add Playwright smoke test of the demo's main flow"
```
