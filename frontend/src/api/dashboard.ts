import { apiFetch } from "./client";
import type { DashboardSummary } from "./types";

export function getDashboardSummary(from?: string, to?: string): Promise<DashboardSummary> {
  const query = from && to ? `?from=${from}&to=${to}` : "";
  return apiFetch<DashboardSummary>(`/api/dashboard/summary${query}`);
}
