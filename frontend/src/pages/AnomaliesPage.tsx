import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useDateRange } from "../context/DateRangeContext";
import { getAnomalies } from "../api/anomalies";
import type { AnomalySummary } from "../api/types";
import SeverityBadge from "../components/SeverityBadge";
import EmptyState from "../components/EmptyState";
import ErrorState from "../components/ErrorState";
import { label } from "../lib/labels";
import { toLocalDateStr } from "../lib/format";

type Item = AnomalySummary & { meter_id: string };

export default function AnomaliesPage() {
  const { range } = useDateRange();
  const navigate = useNavigate();
  // El dashboard enlaza aquí con ?severity=HIGH (tarjeta "Alta prioridad"), así
  // que los filtros viven en la URL en vez de solo en estado local: así el
  // link es compartible/navegable con atrás-adelante, no solo un clic directo.
  const [searchParams, setSearchParams] = useSearchParams();
  const [items, setItems] = useState<Item[]>([]);
  const [loading, setLoading] = useState(true);
  const typeFilter = searchParams.get("type") ?? "";
  const severityFilter = searchParams.get("severity") ?? "";
  const [error, setError] = useState<string | null>(null);

  function setTypeFilter(value: string) {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      value ? next.set("type", value) : next.delete("type");
      return next;
    });
  }

  function setSeverityFilter(value: string) {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      value ? next.set("severity", value) : next.delete("severity");
      return next;
    });
  }

  function load() {
    setLoading(true);
    setError(null);
    getAnomalies({ from: range.from, to: range.to, type: typeFilter, severity: severityFilter })
      .then((r) => setItems(r.items))
      .catch(() => setError("No se pudieron cargar las anomalías."))
      .finally(() => setLoading(false));
  }

  useEffect(load, [range.from, range.to, typeFilter, severityFilter]);

  const sorted = useMemo(() => [...items].sort((a, b) => a.priority_rank - b.priority_rank), [items]);

  return (
    <div className="space-y-4">
      <div className="flex gap-3">
        <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)} className="border rounded px-2 py-1.5 text-sm">
          <option value="">Todos los tipos</option>
          <option value="REAL_ANOMALY">Real anomaly</option>
          <option value="DATA_QUALITY">Data quality</option>
          <option value="EXPLAINABLE_ANOMALY">Explainable</option>
          <option value="FALSE_POSITIVE">False positive</option>
        </select>
        <select value={severityFilter} onChange={(e) => setSeverityFilter(e.target.value)} className="border rounded px-2 py-1.5 text-sm">
          <option value="">Toda severidad</option>
          <option value="HIGH">High</option>
          <option value="MEDIUM">Medium</option>
          <option value="LOW">Low</option>
        </select>
      </div>

      {loading ? (
        <div>Cargando…</div>
      ) : error && sorted.length === 0 ? (
        <ErrorState message={error} onRetry={load} />
      ) : sorted.length === 0 ? (
        <EmptyState message="Sin anomalías en este rango" />
      ) : (
        <table className="w-full bg-white rounded-lg shadow-sm text-sm">
          <thead>
            <tr className="text-left text-slate-400 border-b">
              <th className="p-3">Medidor</th>
              <th className="p-3">Tipo</th>
              <th className="p-3">Severidad</th>
              <th className="p-3">Confianza</th>
              <th className="p-3">Cuándo</th>
              <th className="p-3">Acción</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((a) => (
              <tr key={a.id} className="border-b hover:bg-slate-50 cursor-pointer" onClick={() => navigate(`/anomalies/${a.id}`)}>
                <td className="p-3 font-medium">{a.meter_id}</td>
                <td className="p-3">{label(a.type)}</td>
                <td className="p-3"><SeverityBadge severity={a.severity} /></td>
                <td className="p-3">{a.confidence >= 0.85 ? "Alta" : a.confidence >= 0.6 ? "Media" : "Baja"}</td>
                <td className="p-3">{toLocalDateStr(a.active_from)} – {toLocalDateStr(a.active_to)}</td>
                <td className="p-3 text-blue-600">Investigar</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
