import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { BarChart, Bar, Cell, Tooltip, XAxis, YAxis, CartesianGrid, LabelList } from "recharts";
import { useDateRange } from "../context/DateRangeContext";
import { getDashboardSummary } from "../api/dashboard";
import { getAnomalies } from "../api/anomalies";
import type { DashboardSummary, AnomalySummary } from "../api/types";
import { formatNumberEsES, meterColor, severityColor, toLocalDateStr, toLocalDateTimeStr } from "../lib/format";
import RunAnalysisButton from "../components/RunAnalysisButton";
import ErrorState from "../components/ErrorState";

// Shared by the pie's Tooltip and Legend so both show the same "kWh (%)"
// wording for a by_meter entry.
function formatByMeterShare(consumptionKWh: number, sharePct: number | undefined) {
  const pct = typeof sharePct === "number" ? ` (${sharePct.toFixed(1)} %)` : "";
  return `${formatNumberEsES(consumptionKWh)} kWh${pct}`;
}

export default function DashboardPage() {
  const { range } = useDateRange();
  const navigate = useNavigate();
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [topAnomalies, setTopAnomalies] = useState<(AnomalySummary & { meter_id: string })[]>([]);
  const [error, setError] = useState<string | null>(null);

  function load() {
    Promise.all([
      getDashboardSummary(range.from, range.to).then(setSummary),
      getAnomalies({ from: range.from, to: range.to }).then((r) => setTopAnomalies(r.items.slice(0, 5))),
    ])
      .then(() => setError(null))
      .catch(() => setError("No se pudo cargar el dashboard."));
  }

  useEffect(load, [range.from, range.to]);

  if (error && !summary) return <ErrorState message={error} onRetry={load} />;
  if (!summary) return <div>Cargando…</div>;

  // Ordenado de mayor a menor consumo (spec 04) por las dudas: no confiar en
  // que el backend ya lo entregue así deja el gráfico correcto aunque cambie.
  const byMeter = [...summary.by_meter].sort((a, b) => b.consumption_kwh - a.consumption_kwh);

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-3 md:grid-cols-6 gap-4">
        <Kpi label="Medidores" value={summary.meters} />
        <Kpi label="Consumo total" value={`${formatNumberEsES(summary.total_consumption_kwh)} kWh`} />
        <Kpi label="Anomalías IA" value={summary.anomalies} onClick={() => navigate("/anomalies")} />
        <Kpi label="Alta prioridad" value={summary.high_priority} onClick={() => navigate("/anomalies?severity=HIGH")} />
        <Kpi label="Confianza IA" value={`${Math.round(summary.avg_confidence * 100)}%`} />
        <Kpi
          label="Último análisis"
          value={summary.last_analysis ? toLocalDateTimeStr(summary.last_analysis.at) : "Sin análisis"}
        />
      </div>

      <RunAnalysisButton onComplete={load} />

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Consumo por medidor</h3>
        {/* Barras en vez de pastel: con 12 medidores, comparar longitudes es
            más legible que comparar ángulos de porciones, y el eje X ya
            etiqueta cada barra sin necesitar una leyenda aparte. */}
        <BarChart  width={900} height={320} data={byMeter}  margin={{ top: 24, right: 12, left: 12, bottom: 8 } } className="justify-self-center">
          <CartesianGrid strokeDasharray="3 3" vertical={false} />
          <XAxis dataKey="meter_id" tick={{ fontSize: 12 }} />
          <YAxis tick={{ fontSize: 12 }} width={40} />
          <Tooltip
            formatter={(v, _name, item) => [
              formatByMeterShare(Number(v), item?.payload?.share_pct),
              item?.payload?.meter_id,
            ]}
          />
          <Bar dataKey="consumption_kwh" onClick={(entry) => navigate(`/meters/${entry.payload?.meter_id}`)} radius={[4, 4, 0, 0]}>
            {byMeter.map((m) => (
              // Marca visual de severidad: contorno para medidores con
              // anomalía vigente ALTA (spec 03: by_meter[].severity).
              <Cell
                key={m.meter_id}
                fill={meterColor(m.meter_id)}
                cursor="pointer"
                stroke={m.severity === "HIGH" ? "#dc2626" : "none"}
                strokeWidth={m.severity === "HIGH" ? 3 : 0}
              />
            ))}
            <LabelList
              dataKey="share_pct"
              position="top"
              formatter={(v: unknown) => (typeof v === "number" ? `${v.toFixed(1)} %` : "")}
              style={{ fontSize: 11, fill: "#64748b" }}
            />
          </Bar>
        </BarChart>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Top de anomalías priorizadas</h3>
        {topAnomalies.length === 0 && <p className="text-sm text-slate-400">Sin análisis</p>}
        <ul className="divide-y">
          {topAnomalies.map((a) => (
            // Grid con columnas de ancho fijo en vez de flex justify-between:
            // con flex, el ancho variable del chip de severidad (HIGH/MEDIUM/LOW
            // no miden lo mismo) corre la columna del medio fila a fila. Con
            // grid, el límite de cada columna es fijo sin importar el contenido.
            <li
              key={a.id}
              className="py-2 grid grid-cols-[4rem_1fr_4.5rem] items-center gap-2 cursor-pointer"
              onClick={() => navigate(`/anomalies/${a.id}`)}
            >
              <span className="truncate">{a.meter_id}</span>
              <span className="text-xs text-slate-500 text-center">{toLocalDateStr(a.active_from)} – {toLocalDateStr(a.active_to)}</span>
              <span className={`justify-self-end px-2 py-0.5 rounded text-xs ${severityColor(a.severity)}`}>{a.severity}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function Kpi({ label, value, onClick }: { label: string; value: string | number; onClick?: () => void }) {
  const clickable = onClick != null;
  return (
    <div
      className={`bg-white rounded-lg shadow-sm p-4 ${clickable ? "cursor-pointer hover:shadow-md hover:ring-1 hover:ring-blue-200 transition" : ""}`}
      onClick={onClick}
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      onKeyDown={clickable ? (e) => e.key === "Enter" && onClick!() : undefined}
    >
      <p className="text-xs text-slate-400">{label}</p>
      <p className={`text-lg font-semibold ${clickable ? "text-blue-600" : "text-slate-900"}`}>{value}</p>
    </div>
  );
}
