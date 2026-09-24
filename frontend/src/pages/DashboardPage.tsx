import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { PieChart, Pie, Cell, Tooltip, Legend } from "recharts";
import { useDateRange } from "../context/DateRangeContext";
import { getDashboardSummary } from "../api/dashboard";
import { getAnomalies } from "../api/anomalies";
import type { DashboardSummary, AnomalySummary } from "../api/types";
import { formatNumberEsES, meterColor, severityColor } from "../lib/format";
import RunAnalysisButton from "../components/RunAnalysisButton";

export default function DashboardPage() {
  const { range } = useDateRange();
  const navigate = useNavigate();
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [topAnomalies, setTopAnomalies] = useState<(AnomalySummary & { meter_id: string })[]>([]);

  function load() {
    getDashboardSummary(range.from, range.to).then(setSummary);
    getAnomalies({ from: range.from, to: range.to }).then((r) => setTopAnomalies(r.items.slice(0, 5)));
  }

  useEffect(load, [range.from, range.to]);

  if (!summary) return <div>Cargando…</div>;

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-3 md:grid-cols-6 gap-4">
        <Kpi label="Medidores" value={summary.meters} />
        <Kpi label="Consumo total" value={`${formatNumberEsES(summary.total_consumption_kwh)} kWh`} />
        <Kpi label="Anomalías IA" value={summary.anomalies} />
        <Kpi label="Alta prioridad" value={summary.high_priority} />
        <Kpi label="Confianza IA" value={`${Math.round(summary.avg_confidence * 100)}%`} />
        <Kpi label="Último análisis" value={summary.data_range.to.slice(0, 10)} />
      </div>

      <RunAnalysisButton onComplete={load} />

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Consumo por medidor</h3>
        <PieChart width={420} height={300}>
          <Pie data={summary.by_meter} dataKey="consumption_kwh" nameKey="meter_id" cx="50%" cy="50%" outerRadius={100}
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
            formatter={(v, _name, item) => {
              const sharePct = item?.payload?.share_pct;
              const pct = typeof sharePct === "number" ? ` (${sharePct.toFixed(1)} %)` : "";
              return [`${formatNumberEsES(Number(v))} kWh${pct}`, item?.payload?.meter_id];
            }}
          />
          <Legend onClick={(entry) => navigate(`/meters/${entry.value}`)} />
        </PieChart>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Top de anomalías priorizadas</h3>
        {topAnomalies.length === 0 && <p className="text-sm text-slate-400">Sin análisis</p>}
        <ul className="divide-y">
          {topAnomalies.map((a) => (
            <li key={a.id} className="py-2 flex justify-between cursor-pointer" onClick={() => navigate(`/anomalies/${a.id}`)}>
              <span>{a.meter_id}</span>
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
