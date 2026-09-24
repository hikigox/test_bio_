import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { LineChart, Line, XAxis, YAxis, Tooltip, ReferenceArea, CartesianGrid } from "recharts";
import { useDateRange } from "../context/DateRangeContext";
import { getMeter, getMeterReadings, getMeterEvents } from "../api/meters";
import type { MeterDetail } from "../api/types";
import { formatNumberEsES, formatPercent } from "../lib/format";
import StatusBadge from "../components/StatusBadge";
import RunAnalysisButton from "../components/RunAnalysisButton";

interface ReadingPoint {
  timestamp: string;
  consumption_kwh: number;
  voltage_v: number;
  current_a: number;
  power_factor: number;
}

interface MeterEvent {
  timestamp: string;
  type: string;
  description: string;
}

export default function MeterDetailPage() {
  const { meterId } = useParams<{ meterId: string }>();
  const { range } = useDateRange();
  const [meter, setMeter] = useState<MeterDetail | null>(null);
  const [readings, setReadings] = useState<ReadingPoint[]>([]);
  const [events, setEvents] = useState<MeterEvent[]>([]);

  function load() {
    if (!meterId) return;
    getMeter(meterId, range.from, range.to).then(setMeter);
    getMeterReadings(meterId, range.from, range.to).then((r) => setReadings(r.items));
    getMeterEvents(meterId).then((r) => setEvents(r.items));
  }

  useEffect(load, [meterId, range.from, range.to]);

  if (!meter) return <div>Cargando…</div>;

  // La banda de anomalía usa `anomaly` (la vigente dentro del rango
  // seleccionado), no `latest_anomaly` (la del último análisis sin filtrar
  // por rango): si el rango elegido no la incluye, `anomaly` viene null y no
  // debe pintarse ninguna banda aunque exista una anomalía histórica.
  const anomaly = meter.anomaly;

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-start">
        <div>
          <h2 className="text-xl font-semibold text-slate-900">{meter.meter_id}</h2>
          <div className="flex gap-4 mt-2 text-sm text-slate-500">
            <span>{meter.consumption_kwh != null ? `${formatNumberEsES(meter.consumption_kwh)} kWh` : "—"}</span>
            <span>{meter.variation_pct != null ? formatPercent(meter.variation_pct) : "—"}</span>
            <StatusBadge status={meter.status} />
          </div>
        </div>
        <RunAnalysisButton scope={{ meter_ids: [meter.meter_id] }} onComplete={load} label="Analizar este medidor" />
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Consumo vs. baseline</h3>
        <LineChart width={700} height={280} data={readings}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="timestamp" tickFormatter={(t) => t.slice(5, 10)} />
          <YAxis />
          <Tooltip labelFormatter={(t) => `${t} UTC`} />
          {anomaly && (
            <ReferenceArea x1={anomaly.active_from} x2={anomaly.active_to} fill="#fca5a5" fillOpacity={0.2} />
          )}
          <Line type="monotone" dataKey="consumption_kwh" stroke="#2563eb" dot={false} name="Consumo (kWh)" />
        </LineChart>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <ElectricalChart title="Voltaje (V)" readings={readings} dataKey="voltage_v" color="#059669" anomaly={anomaly} />
        <ElectricalChart title="Corriente (A)" readings={readings} dataKey="current_a" color="#d97706" anomaly={anomaly} />
        <ElectricalChart title="Factor de potencia" readings={readings} dataKey="power_factor" color="#7c3aed" anomaly={anomaly} />
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Eventos</h3>
        {events.length === 0 && <p className="text-sm text-slate-400">Sin eventos registrados</p>}
        <ul className="text-sm divide-y">
          {events.map((e, i) => (
            <li key={i} className="py-2">{e.timestamp.slice(0, 10)} · {e.type} — {e.description}</li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function ElectricalChart({
  title,
  readings,
  dataKey,
  color,
  anomaly,
}: {
  title: string;
  readings: ReadingPoint[];
  dataKey: "voltage_v" | "current_a" | "power_factor";
  color: string;
  anomaly: MeterDetail["anomaly"];
}) {
  return (
    <div className="bg-white rounded-lg shadow-sm p-4">
      <h3 className="font-medium text-slate-900 mb-2">{title}</h3>
      <LineChart width={220} height={180} data={readings}>
        <CartesianGrid strokeDasharray="3 3" />
        <XAxis dataKey="timestamp" tickFormatter={(t) => t.slice(5, 10)} />
        <YAxis />
        <Tooltip labelFormatter={(t) => `${t} UTC`} />
        {anomaly && (
          <ReferenceArea x1={anomaly.active_from} x2={anomaly.active_to} fill="#fca5a5" fillOpacity={0.2} />
        )}
        <Line type="monotone" dataKey={dataKey} stroke={color} dot={false} name={title} />
      </LineChart>
    </div>
  );
}
