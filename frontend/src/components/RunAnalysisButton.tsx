import { useEffect, useRef, useState } from "react";
import { postAnalyze, getAnalysis } from "../api/analysis";
import type { AnalyzeScope } from "../api/analysis";
import { usePolling } from "../hooks/usePolling";
import type { AnalysisStatus } from "../api/types";

const STAGES = ["READINGS", "BASELINE", "DETECTION", "CORRELATION", "EVENTS", "EXPLANATION", "RECOMMENDATION"];

export default function RunAnalysisButton({ scope, onComplete }: { scope?: AnalyzeScope; onComplete: (status: AnalysisStatus) => void }) {
  const [analysisId, setAnalysisId] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const { data: status } = usePolling<AnalysisStatus | null>(
    async () => (analysisId ? getAnalysis(analysisId) : null),
    (data) => data?.status === "COMPLETED" || data?.status === "FAILED",
    1000,
    analysisId ?? "idle"
  );

  // Notifica al padre una sola vez por análisis completado: llamarlo en cada
  // render (como haría comprobar la condición directamente en el cuerpo del
  // componente) dispara un bucle infinito, porque onComplete normalmente
  // dispara un refetch en el padre que vuelve a renderizar este componente,
  // que a su vez lo volvería a invocar mientras `status` siga COMPLETED.
  const notifiedForRef = useRef<number | null>(null);
  useEffect(() => {
    if (status?.status === "COMPLETED" && analysisId && notifiedForRef.current !== analysisId) {
      notifiedForRef.current = analysisId;
      onComplete(status);
    }
  }, [status, analysisId, onComplete]);

  async function start() {
    setError(null);
    try {
      const res = await postAnalyze(scope);
      setAnalysisId(res.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "error iniciando el análisis");
    }
  }

  const running = analysisId !== null && status?.status !== "COMPLETED" && status?.status !== "FAILED";

  return (
    <div>
      <button onClick={start} disabled={running} className="bg-blue-600 text-white rounded px-4 py-2 disabled:opacity-50">
        {running ? "Analizando…" : "Run AI Analysis"}
      </button>
      {error && <p className="text-sm text-red-600 mt-2">{error}</p>}
      {running && status && (
        <ol className="mt-3 flex gap-2 text-xs text-slate-500">
          {STAGES.map((stage) => (
            <li key={stage} className={stage === status.stage ? "font-semibold text-blue-600" : ""}>{stage}</li>
          ))}
        </ol>
      )}
      {status?.status === "COMPLETED" && <p className="text-sm text-green-700 mt-2">{status.summary.message}</p>}
    </div>
  );
}
