# 06 · Docker (levantamiento con un comando)

Objetivo: `docker compose up --build` deja la app funcionando, con datos cargados y usuario demo, sin instalar Go ni Node.

## Servicios
| Servicio | Imagen | Puerto | Notas |
|---|---|---|---|
| backend | Go multi-stage → alpine | 8080 | SQLite en volumen; carga CSV al iniciar si la BD está vacía |
| frontend | Node build → nginx | 3000 | Sirve el SPA y hace proxy de `/api` al backend |

## Reglas
1. Backend y frontend se comunican por la red interna de Compose; el navegador solo habla con `:3000`.
2. `./data` se monta en el backend como **solo lectura** (`/data:ro`). Solo `readings.csv` y `events.csv`.
3. `expected_results.csv` NO va en `./data` ni en la imagen. `.dockerignore` lo excluye. `cmd/eval` se ejecuta fuera de la app, con el archivo montado a mano.
4. BD SQLite en volumen nombrado (`dbdata`): persiste entre reinicios; `make reset` la borra.
5. Configuración por variables de entorno (`.env.example`): `PORT`, `DB_PATH`, `DATA_DIR`, `JWT_SECRET`, `DEMO_EMAIL`, `DEMO_PASSWORD`, `LLM_EXPLAIN` (0 por defecto), `ANTHROPIC_API_KEY` (opcional).
6. Healthcheck en backend (`GET /healthz`); el frontend espera `service_healthy`.
7. Imagen final del backend sin CGO, usuario no root.

## Comandos (Makefile)
- `make up` → `docker compose up --build -d`
- `make down` → detener
- `make reset` → `docker compose down -v` (borra la BD)
- `make logs` → logs
- `make test` → `go test ./...` en contenedor
- `make eval EXPECTED=/ruta/expected_results.csv` → corre `cmd/eval`

## Criterios de aceptación
- Clon limpio + `docker compose up --build` → login funcional en `http://localhost:3000` en menos de 2 minutos.
- Reiniciar no duplica lecturas (carga idempotente).
- La imagen del backend no contiene `expected_results.csv` (verificable con `docker run ... ls`).
