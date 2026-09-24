import { apiFetch, ApiError } from "./client";
import type { AnalysisStatus } from "./types";

export interface AnalyzeScope {
  meter_ids?: string[];
  from?: string;
  to?: string;
  baseline_from?: string;
  baseline_to?: string;
}

export async function postAnalyze(scope?: AnalyzeScope): Promise<{ id: number; status: string }> {
  try {
    return await apiFetch("/api/ai/analyze", {
      method: "POST",
      body: scope ? JSON.stringify(scope) : undefined,
    });
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      throw new Error("Ya hay un análisis en curso. Espera a que termine.");
    }
    throw err;
  }
}

export function getAnalysis(id: number): Promise<AnalysisStatus> {
  return apiFetch<AnalysisStatus>(`/api/ai/analysis/${id}`);
}
