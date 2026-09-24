# 04 · Frontend (React + Vite + Tailwind + Recharts)

Debe sentirse como un SaaS de Energy Management: sidebar, barra superior, tema limpio, estados de carga/vacío/error consistentes.

## Filtro de fechas global
- Un solo control en la barra superior, visible en todas las pantallas. Presets: **Todo el período · Últimos 7 días · Últimos 3 días · Personalizado**.
- Límites del selector = `data_range` que devuelve `/dashboard/summary` (primera y última lectura).
- Estado en la URL (`?from=&to=`) y en un contexto React: se conserva al navegar y los enlaces son compartibles. Por defecto, todo el período.

| Pantalla | Qué cambia con el filtro |
|---|---|
| Dashboard | Consumo total, gráfica de pastel y KPIs de anomalías |
| Medidores | Consumo, baseline esperado y variación del rango; la anomalía se marca "fuera del rango" si no se solapa |
| Detalle de medidor | Gráficas del rango y banda de la anomalía |
| Anomalías | Solo las anomalías cuya ventana se solapa con el rango |

- **Regla de solapamiento:** una anomalía se muestra si su ventana activa (`active_from` → `active_to`) intersecta el rango elegido.
- El filtro **no cambia** el tipo, la severidad, la confianza ni el estado del medidor: esos vienen del análisis (ver 03).
- Todas las fechas se muestran en una sola zona horaria (definir en T0 según los CSV) y el tooltip indica UTC.

## Rutas y pantallas
1. **/login**: email + contraseña; redirige al dashboard.
2. **/ (Dashboard)**
   - KPIs: Medidores, Consumo total, Anomalías IA, Alta prioridad, Confianza IA, Último análisis.
   - Botón **Run AI Analysis** (flota completa).
   - **Gráfica de pastel "Consumo por medidor":** los 12 medidores, cada porción = consumo del rango filtrado. Ordenada de mayor a menor; leyenda con kWh y %; tooltip al pasar el cursor; sin etiqueta interna en porciones < 3 %. Los medidores con anomalía llevan un marcador del color de su severidad; clic en una porción o en la leyenda abre el detalle del medidor. Color fijo por medidor en toda la app.
   - Top de anomalías priorizadas (con columna "Cuándo").
3. **/meters**: tabla (medidor, consumo, variación, estado, anomalía). Filtros: todos/normales/alertas/críticas. Búsqueda por meter_id. Orden por consumo/variación/severidad. Anomalía fuera del rango filtrado → badge atenuado "fuera del rango".
4. **/meters/:id**
   - Consumo, baseline esperado, variación, estado.
   - Gráfica de consumo con el perfil de baseline superpuesto y una **banda sombreada** en la ventana de la anomalía, con marcador del punto de cambio.
   - Gráficas de voltaje, corriente y FP; eventos marcados en la línea de tiempo.
   - Lista de anomalías del medidor (vigente e histórico).
   - Botón **Analizar este medidor** (`meter_ids=[id]`).
5. **/anomalies**: tabla Medidor · Tipo · Severidad · Confianza (Alta/Media/Baja) · **Cuándo** (desde–hasta y duración) · Acción, ordenada por prioridad; filtros por tipo y severidad, más el filtro de fechas global.
6. **/anomalies/:id (Investigación)**
   - Qué encontró la IA y **cuándo ocurrió** (inicio, fin, duración, punto de cambio).
   - Variables que cambiaron (baseline vs actual), gráfica comparativa, eventos relacionados.
   - Severidad, confianza y su desglose, acción recomendada, evidencia (señales y ruta de decisión).
   - Botones: Investigar / Descartar / Resolver.

## Run AI Analysis
- En Dashboard y Anomalías (flota) y en el detalle (un medidor). Al pulsar: stepper de 7 etapas (Lecturas → Baseline → Detección → Correlación → Eventos → Explicación → Recomendación) con polling.
- Al terminar: mensaje "4 anomalías detectadas · 2 requieren atención prioritaria" y refresco de datos.

## Convenciones visuales
- Severidad: High rojo · Medium ámbar · Low azul/gris. Estado: OK verde.
- Badges por tipo: Real anomaly, Data quality, Explainable, False positive.
- Números con separador es-ES (1.860 kWh, +47,6 %).
- Responsive básico (tablet+). Sin librerías de UI pesadas.

## Estructura
`src/{api,pages,components,hooks,lib}`; cliente API tipado con los contratos de 03-api.md; contexto `DateRangeContext`; token en memoria/localStorage.
