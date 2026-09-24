import { apiFetch } from "./client";
import { MeterListItem } from "./types";

export function getMeters(params: { from?: string; to?: string; status?: string; q?: string; sort?: string; order?: string } = {}) {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v) as [string, string][]);
  return apiFetch<{ items: MeterListItem[] }>(`/api/meters?${q}`);
}

export function getMeter(meterId: string, from?: string, to?: string) {
  const q = from && to ? `?from=${from}&to=${to}` : "";
  return apiFetch<MeterListItem>(`/api/meters/${meterId}${q}`);
}

export function getMeterReadings(meterId: string, from: string, to: string) {
  return apiFetch<{ items: { timestamp: string; consumption_kwh: number; voltage_v: number; current_a: number; power_factor: number }[] }>(
    `/api/meters/${meterId}/readings?from=${from}&to=${to}`
  );
}

export function getMeterEvents(meterId: string) {
  return apiFetch<{ items: { timestamp: string; type: string; description: string }[] }>(`/api/meters/${meterId}/events`);
}
