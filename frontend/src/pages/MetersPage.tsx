import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useDateRange } from "../context/DateRangeContext";
import { getMeters } from "../api/meters";
import { MeterListItem } from "../api/types";
import { formatNumberEsES, formatPercent } from "../lib/format";
import StatusBadge from "../components/StatusBadge";
import SeverityBadge from "../components/SeverityBadge";

type StatusFilter = "all" | "normal" | "alert" | "critical";
type SortField = "consumption" | "variation" | "severity";

// Búsqueda, filtro de estado y orden se resuelven en el cliente: GET /meters
// ya trae toda la flota para el rango seleccionado (sin paginación), así que
// no hay ganancia real en repetir la petición por cada tecleo o cambio de
// filtro — y hacerlo introduciría latencia visible en la búsqueda tipo
// "as you type". El backend sí soporta status/q/sort/order (usados aquí solo
// para la petición inicial con from/to), y getMeters() expone esos params
// para quien los necesite (p.ej. una futura vista paginada).
export default function MetersPage() {
  const { range } = useDateRange();
  const navigate = useNavigate();
  const [items, setItems] = useState<MeterListItem[]>([]);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortField>("consumption");

  useEffect(() => {
    getMeters({ from: range.from, to: range.to }).then((r) => setItems(r.items));
  }, [range.from, range.to]);

  const filtered = useMemo(() => {
    let out = items;
    if (statusFilter !== "all") {
      const map: Record<StatusFilter, string> = { all: "", normal: "OK", alert: "ALERT", critical: "CRITICAL" };
      out = out.filter((m) => m.status === map[statusFilter]);
    }
    if (query) {
      out = out.filter((m) => m.meter_id.toLowerCase().includes(query.toLowerCase()));
    }
    const severityRank: Record<string, number> = { HIGH: 3, MEDIUM: 2, LOW: 1 };
    return [...out].sort((a, b) => {
      if (sort === "consumption") return (b.consumption_kwh ?? 0) - (a.consumption_kwh ?? 0);
      if (sort === "variation") return Math.abs(b.variation_pct ?? 0) - Math.abs(a.variation_pct ?? 0);
      return (severityRank[b.anomaly?.severity ?? ""] ?? 0) - (severityRank[a.anomaly?.severity ?? ""] ?? 0);
    });
  }, [items, statusFilter, query, sort]);

  return (
    <div className="space-y-4">
      <div className="flex gap-3">
        <input placeholder="Buscar medidor…" value={query} onChange={(e) => setQuery(e.target.value)}
          className="border rounded px-3 py-1.5 text-sm" />
        <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value as StatusFilter)} className="border rounded px-2 py-1.5 text-sm">
          <option value="all">Todos</option>
          <option value="normal">Normales</option>
          <option value="alert">Alertas</option>
          <option value="critical">Críticas</option>
        </select>
        <select value={sort} onChange={(e) => setSort(e.target.value as SortField)} className="border rounded px-2 py-1.5 text-sm">
          <option value="consumption">Ordenar por consumo</option>
          <option value="variation">Ordenar por variación</option>
          <option value="severity">Ordenar por severidad</option>
        </select>
      </div>

      <table className="w-full bg-white rounded-lg shadow-sm text-sm">
        <thead>
          <tr className="text-left text-slate-400 border-b">
            <th className="p-3">Medidor</th><th className="p-3">Consumo</th><th className="p-3">Variación</th>
            <th className="p-3">Estado</th><th className="p-3">Anomalía</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map((m) => (
            <tr key={m.meter_id} className="border-b hover:bg-slate-50 cursor-pointer" onClick={() => navigate(`/meters/${m.meter_id}`)}>
              <td className="p-3 font-medium">{m.meter_id}</td>
              <td className="p-3">{m.consumption_kwh != null ? `${formatNumberEsES(m.consumption_kwh)} kWh` : "—"}</td>
              <td className="p-3">{m.variation_pct != null ? formatPercent(m.variation_pct) : "—"}</td>
              <td className="p-3"><StatusBadge status={m.status} /></td>
              <td className="p-3">
                {m.anomaly ? (
                  <span className={`inline-flex items-center gap-2 ${m.anomaly.in_range ? "" : "opacity-40"}`}>
                    <SeverityBadge severity={m.anomaly.severity} />
                    {m.anomaly.type}{!m.anomaly.in_range && " (fuera del rango)"}
                  </span>
                ) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
