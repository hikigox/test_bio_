import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { getAnomaly, patchAnomaly } from "../api/anomalies";
import type { AnomalyDetail } from "../api/types";
import SeverityBadge from "../components/SeverityBadge";

export default function AnomalyDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [anomaly, setAnomaly] = useState<AnomalyDetail | null>(null);

  function load() {
    if (id) getAnomaly(Number(id)).then(setAnomaly);
  }
  useEffect(load, [id]);

  async function changeStatus(status: AnomalyDetail["status"]) {
    if (!anomaly) return;
    await patchAnomaly(anomaly.id, status);
    load();
  }

  if (!anomaly) return <div>Cargando…</div>;

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-start">
        <div>
          <h2 className="text-xl font-semibold text-slate-900">{anomaly.meter_id} — {anomaly.type}</h2>
          <div className="mt-2 flex gap-2 items-center">
            <SeverityBadge severity={anomaly.severity} />
            <span className="text-xs text-slate-500">Estado: {anomaly.status}</span>
          </div>
        </div>
        <div className="flex gap-2">
          <button onClick={() => changeStatus("INVESTIGATING")} disabled={anomaly.status === "INVESTIGATING"} className="border rounded px-3 py-1.5 text-sm disabled:opacity-50">Investigar</button>
          <button onClick={() => changeStatus("DISMISSED")} disabled={anomaly.status === "DISMISSED"} className="border rounded px-3 py-1.5 text-sm disabled:opacity-50">Descartar</button>
          <button onClick={() => changeStatus("RESOLVED")} disabled={anomaly.status === "RESOLVED"} className="border rounded px-3 py-1.5 text-sm disabled:opacity-50">Resolver</button>
        </div>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Qué encontró la IA</h3>
        <p className="text-sm text-slate-700">{anomaly.reason}</p>
        <p className="text-sm text-slate-500 mt-2">Acción recomendada: {anomaly.recommended_action}</p>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Evidencia</h3>
        <p className="text-xs text-slate-500 mb-1">Ruta de decisión: {anomaly.evidence.decision_path.join(" → ")}</p>
        {anomaly.evidence.data_quality_issues.length > 0 && (
          <ul className="text-xs text-slate-500">
            {anomaly.evidence.data_quality_issues.map((iss, i) => (
              <li key={i}>{iss.kind}: {iss.count}</li>
            ))}
          </ul>
        )}
        <p className="text-xs text-slate-400 mt-2">
          Lecturas afectadas: {anomaly.evidence.affected_readings.count}
        </p>
      </div>
    </div>
  );
}
