import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { PieChart, Pie, Cell, Tooltip, Legend } from "recharts";
import { useDateRange } from "../context/DateRangeContext";
import { getDashboardSummary } from "../api/dashboard";
import { getAnomalies } from "../api/anomalies";
import type { DashboardSummary, AnomalySummary } from "../api/types";
import { formatNumberEsES, meterColor, severityColor } from "../lib/format";
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

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-3 md:grid-cols-6 gap-4">
        <Kpi label="Medidores" value={summary.meters} />
        <Kpi label="Consumo total" value={`${formatNumberEsES(summary.total_consumption_kwh)} kWh`} />
        <Kpi label="Anomalías IA" value={summary.anomalies} />
        <Kpi label="Alta prioridad" value={summary.high_priority} />
        <Kpi label="Confianza IA" value={`${Math.round(summary.avg_confidence * 100)}%`} />
        <Kpi
          label="Último análisis"
          value={summary.last_analysis ? summary.last_analysis.at.slice(0, 16).replace("T", " ") : "Sin análisis"}
        />
      </div>

      <RunAnalysisButton onComplete={load} />

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Consumo por medidor</h3>
        {/* Leyenda vertical a la derecha: con 12 medidores y etiquetas largas
            ("M-101: 14.988 kWh (9.6 %)"), la leyenda horizontal por defecto de
            recharts no reserva su propio espacio y termina superpuesta sobre
            el pastel. layout="vertical" + align="right" la saca a una columna
            aparte del ancho total del chart. */}
        <PieChart width={620} height={320}>
          <Pie data={summary.by_meter} dataKey="consumption_kwh" nameKey="meter_id" cx="35%" cy="50%" outerRadius={110}
            onClick={(entry) => navigate(`/meters/${entry.payload?.meter_id}`)}>
            {summary.by_meter.map((m) => (
              // Marca visual de severidad: contorno para medidores con
              // anomalía vigente ALTA (spec 03: by_meter[].severity).
              <Cell
                key={m.meter_id}
                fill={meterColor(m.meter_id)}
                cursor="pointer"
                stroke={m.severity === "HIGH" ? "#dc2626" : "#fff"}
                strokeWidth={m.severity === "HIGH" ? 3 : 1}
              />
            ))}
          </Pie>
          <Tooltip
            formatter={(v, _name, item) => [
              formatByMeterShare(Number(v), item?.payload?.share_pct),
              item?.payload?.meter_id,
            ]}
          />
          <Legend
            layout="vertical"
            align="right"
            verticalAlign="middle"
            wrapperStyle={{ fontSize: 12, lineHeight: "18px", maxHeight: 300, overflowY: "auto" }}
            onClick={(entry) => navigate(`/meters/${entry.value}`)}
            formatter={(value, entry) => {
              // recharts' Legend payload item carries the original Pie datum
              // in `payload`, but its declared type is a bare `object`.
              const payload = entry?.payload as { consumption_kwh?: number; share_pct?: number } | undefined;
              const consumptionKWh = payload?.consumption_kwh ?? 0;
              const sharePct = payload?.share_pct;
              return `${value}: ${formatByMeterShare(consumptionKWh, sharePct)}`;
            }}
          />
        </PieChart>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Top de anomalías priorizadas</h3>
        {topAnomalies.length === 0 && <p className="text-sm text-slate-400">Sin análisis</p>}
        <ul className="divide-y">
          {topAnomalies.map((a) => (
            <li key={a.id} className="py-2 flex justify-between items-center cursor-pointer" onClick={() => navigate(`/anomalies/${a.id}`)}>
              <span>{a.meter_id}</span>
              <span className="text-xs text-slate-500">{a.active_from.slice(0, 10)} – {a.active_to.slice(0, 10)}</span>
              <span className={`px-2 py-0.5 rounded text-xs ${severityColor(a.severity)}`}>{a.severity}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function Kpi({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="bg-white rounded-lg shadow-sm p-4">
      <p className="text-xs text-slate-400">{label}</p>
      <p className="text-lg font-semibold text-slate-900">{value}</p>
    </div>
  );
}
