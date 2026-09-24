import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useDateRange } from "../context/DateRangeContext";
import { getAnomalies } from "../api/anomalies";
import type { AnomalySummary } from "../api/types";
import SeverityBadge from "../components/SeverityBadge";
import EmptyState from "../components/EmptyState";

type Item = AnomalySummary & { meter_id: string };

export default function AnomaliesPage() {
  const { range } = useDateRange();
  const navigate = useNavigate();
  const [items, setItems] = useState<Item[]>([]);
  const [typeFilter, setTypeFilter] = useState("");
  const [severityFilter, setSeverityFilter] = useState("");

  useEffect(() => {
    getAnomalies({ from: range.from, to: range.to, type: typeFilter, severity: severityFilter }).then((r) =>
      setItems(r.items)
    );
  }, [range.from, range.to, typeFilter, severityFilter]);

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

      {sorted.length === 0 ? (
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
                <td className="p-3">{a.type}</td>
                <td className="p-3"><SeverityBadge severity={a.severity} /></td>
                <td className="p-3">{a.confidence >= 0.85 ? "Alta" : a.confidence >= 0.6 ? "Media" : "Baja"}</td>
                <td className="p-3">{a.active_from.slice(0, 10)} – {a.active_to.slice(0, 10)}</td>
                <td className="p-3 text-blue-600">Investigar</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
