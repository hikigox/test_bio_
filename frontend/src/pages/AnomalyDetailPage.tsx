import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getAnomaly, patchAnomaly } from "../api/anomalies";
import type { AnomalyDetail } from "../api/types";
import SeverityBadge from "../components/SeverityBadge";
import ErrorState from "../components/ErrorState";
import { label } from "../lib/labels";
import { toLocalDateTimeStr } from "../lib/format";

export default function AnomalyDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [anomaly, setAnomaly] = useState<AnomalyDetail | null>(null);
  const [error, setError] = useState<string | null>(null);

  function load() {
    if (!id) return;
    getAnomaly(Number(id))
      .then((data) => {
        setAnomaly(data);
        setError(null);
      })
      .catch(() => setError("No se pudo cargar la anomalía."));
  }
  useEffect(load, [id]);

  async function changeStatus(status: AnomalyDetail["status"]) {
    if (!anomaly) return;
    await patchAnomaly(anomaly.id, status);
    load();
  }

  if (error && !anomaly) return <ErrorState message={error} onRetry={load} />;
  if (!anomaly) return <div>Cargando…</div>;

  const decisionPath = anomaly.evidence.decision_path ?? [];
  const dataQualityIssues = anomaly.evidence.data_quality_issues ?? [];
  const signals = anomaly.signals ?? [];
  const variables = anomaly.variables ?? [];
  const events = anomaly.events ?? [];

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-start">
        <div>
          <h2 className="text-xl font-semibold text-slate-900">{anomaly.meter_id} — {label(anomaly.type)}</h2>
          <div className="mt-2 flex gap-2 items-center">
            <SeverityBadge severity={anomaly.severity} />
            <span className="text-xs text-slate-500">Estado: {anomaly.status}</span>
            <span className="text-xs text-slate-500">Confianza: {anomaly.confidence_label}</span>
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
        <h3 className="font-medium text-slate-900 mb-2">Cuándo ocurrió</h3>
        <p className="text-sm text-slate-700">
          {toLocalDateTimeStr(anomaly.active_from)} – {toLocalDateTimeStr(anomaly.active_to)}
        </p>
        <p className="text-xs text-slate-500 mt-1">Duración: {anomaly.duration_hours.toFixed(1)} h</p>
        <p className="text-xs text-slate-500 mt-1">
          Baseline: {anomaly.baseline_kwh.toFixed(1)} kWh · Real: {anomaly.actual_kwh.toFixed(1)} kWh · Variación: {anomaly.variation_pct.toFixed(1)}%
        </p>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Evidencia</h3>
        <p className="text-xs text-slate-500 mb-1">Ruta de decisión: {decisionPath.map(label).join(" → ")}</p>
        {dataQualityIssues.length > 0 && (
          <ul className="text-xs text-slate-500">
            {dataQualityIssues.map((iss, i) => (
              <li key={i}>{label(iss.kind)}: {iss.count}</li>
            ))}
          </ul>
        )}
        <p className="text-xs text-slate-400 mt-2">
          Lecturas afectadas: {anomaly.evidence.affected_readings.count}
        </p>
        {Object.keys(anomaly.evidence.confidence_breakdown ?? {}).length > 0 && (
          <div className="mt-2">
            <p className="text-xs text-slate-500 font-medium">Desglose de confianza</p>
            <ul className="text-xs text-slate-500">
              {Object.entries(anomaly.evidence.confidence_breakdown).map(([k, v]) => (
                <li key={k}>{label(k)}: {v}</li>
              ))}
            </ul>
          </div>
        )}
      </div>

      {variables.length > 0 && (
        <div className="bg-white rounded-lg shadow-sm p-4">
          <h3 className="font-medium text-slate-900 mb-2">Variables comparativa</h3>
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-slate-400 border-b">
                <th className="py-1 pr-2">Variable</th>
                <th className="py-1 pr-2">Baseline</th>
                <th className="py-1 pr-2">Actual</th>
                <th className="py-1 pr-2">Δ %</th>
              </tr>
            </thead>
            <tbody>
              {variables.map((v) => (
                <tr key={v.variable} className={v.changed ? "text-red-600 font-medium" : "text-slate-600"}>
                  <td className="py-1 pr-2">{label(v.variable)}</td>
                  <td className="py-1 pr-2">{v.baseline.toFixed(2)}</td>
                  <td className="py-1 pr-2">{v.actual.toFixed(2)}</td>
                  <td className="py-1 pr-2">{v.delta_pct.toFixed(1)}%</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {signals.length > 0 && (
        <div className="bg-white rounded-lg shadow-sm p-4">
          <div className="flex justify-between items-center mb-2">
            <h3 className="font-medium text-slate-900">Señales</h3>
            <Link to="/glossary/signals" className="text-xs text-blue-600 hover:underline">
              ¿Qué son y cómo se calculan? →
            </Link>
          </div>
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-slate-400 border-b">
                <th className="py-1 pr-2">Señal</th>
                <th className="py-1 pr-2">Observado</th>
                <th className="py-1 pr-2">Umbral</th>
                <th className="py-1 pr-2">Detalle</th>
              </tr>
            </thead>
            <tbody>
              {signals.map((s, i) => {
                // Fuerte = duplica su propio umbral (mismo criterio que
                // dataQualitySeverity/classify.go en el motor).
                const strong = s.threshold > 0 && Math.abs(s.observed) >= 2 * s.threshold;
                return (
                  <tr key={i} className={`border-b last:border-0 ${strong ? "text-red-600 font-medium" : "text-slate-600"}`}>
                    <td className="py-1 pr-2">{label(s.signal)}</td>
                    <td className="py-1 pr-2">{s.observed.toFixed(2)}</td>
                    <td className="py-1 pr-2">{s.threshold.toFixed(2)}</td>
                    <td className="py-1 pr-2 text-slate-500 font-normal">{s.detail}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {events.length > 0 && (
        <div className="bg-white rounded-lg shadow-sm p-4">
          <h3 className="font-medium text-slate-900 mb-2">Eventos relacionados</h3>
          <ul className="text-xs text-slate-600 divide-y">
            {events.map((e) => (
              <li key={e.id} className="py-1">{e.description} ({label(e.relation)}, {e.offset_hours}h)</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
