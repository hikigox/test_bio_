import { apiFetch } from "./client";
import type { AnomalySummary, AnomalyDetail } from "./types";

export function getAnomalies(params: { from?: string; to?: string; type?: string; severity?: string } = {}) {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v) as [string, string][]);
  return apiFetch<{ items: (AnomalySummary & { meter_id: string })[] }>(`/api/anomalies?${q}`);
}

export function getAnomaly(id: number): Promise<AnomalyDetail> {
  return apiFetch<AnomalyDetail>(`/api/anomalies/${id}`);
}

export function patchAnomaly(id: number, status: AnomalyDetail["status"]): Promise<{ status: string }> {
  return apiFetch(`/api/anomalies/${id}`, { method: "PATCH", body: JSON.stringify({ status }) });
}
