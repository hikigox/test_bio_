import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import AnomalyDetailPage from "./AnomalyDetailPage";
import * as anomaliesApi from "../api/anomalies";
import type { AnomalyDetail } from "../api/types";

function baseAnomaly(overrides: Partial<AnomalyDetail> = {}): AnomalyDetail {
  return {
    id: 12, meter_id: "M-109", type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96,
    confidence_label: "Alta", status: "OPEN",
    reason: "Consumo 103.7% por encima del baseline...", recommended_action: "Investigar medidor e instalación",
    active_from: "2026-01-01T00:00:00Z", active_to: "2026-01-01T06:00:00Z", duration_hours: 6,
    change_point_at: null, baseline_kwh: 10, actual_kwh: 20, variation_pct: 103.7,
    explanation_source: "TEMPLATE", signals: [], variables: [], events: [],
    evidence: { decision_path: ["no_data_quality_issue"], confidence_breakdown: {}, data_quality_issues: [], affected_readings: { count: 168, first: "", last: "" } },
    ...overrides,
  };
}

describe("AnomalyDetailPage", () => {
  it("renders reason, evidence, and allows changing status", async () => {
    vi.spyOn(anomaliesApi, "getAnomaly").mockResolvedValue(baseAnomaly());
    const patchSpy = vi.spyOn(anomaliesApi, "patchAnomaly").mockResolvedValue({ status: "INVESTIGATING" });
    const getAnomalySpy = anomaliesApi.getAnomaly as ReturnType<typeof vi.fn>;

    render(
      <MemoryRouter initialEntries={["/anomalies/12"]}>
        <Routes><Route path="/anomalies/:id" element={<AnomalyDetailPage />} /></Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/Consumo 103.7% por encima/)).toBeInTheDocument());
    expect(screen.getByText(/Estado: OPEN/)).toBeInTheDocument();

    getAnomalySpy.mockResolvedValueOnce(baseAnomaly({ status: "INVESTIGATING" }));

    fireEvent.click(screen.getByRole("button", { name: /investigar/i }));
    await waitFor(() => expect(patchSpy).toHaveBeenCalledWith(12, "INVESTIGATING"));
    await waitFor(() => expect(screen.getByText(/Estado: INVESTIGATING/)).toBeInTheDocument());
  });

  it("shows an empty state when the anomaly has no data quality issues", async () => {
    vi.spyOn(anomaliesApi, "getAnomaly").mockResolvedValue(baseAnomaly({
      id: 13, meter_id: "M-200", severity: "LOW", confidence: 0.7, status: "RESOLVED",
      reason: "reason", recommended_action: "action",
      evidence: { decision_path: [], confidence_breakdown: {}, data_quality_issues: [], affected_readings: { count: 0, first: "", last: "" } },
    }));

    render(
      <MemoryRouter initialEntries={["/anomalies/13"]}>
        <Routes><Route path="/anomalies/:id" element={<AnomalyDetailPage />} /></Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/M-200/)).toBeInTheDocument());
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
  });

  // Regression test for the app-crashing bug: the backend serializes
  // evidence.data_quality_issues (and decision_path) as JSON null, not [],
  // whenever there are no issues — confirmed against all 4 real anomalies in
  // the seeded dataset. Rendering must not throw.
  it("renders without throwing when data_quality_issues and decision_path are null", async () => {
    vi.spyOn(anomaliesApi, "getAnomaly").mockResolvedValue(baseAnomaly({
      id: 14, meter_id: "M-300", severity: "MEDIUM", confidence: 0.7, status: "OPEN",
      evidence: { decision_path: null, confidence_breakdown: {}, data_quality_issues: null, affected_readings: { count: 0, first: "", last: "" } },
    }));

    render(
      <MemoryRouter initialEntries={["/anomalies/14"]}>
        <Routes><Route path="/anomalies/:id" element={<AnomalyDetailPage />} /></Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/M-300/)).toBeInTheDocument());
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
  });

  it("shows an error state and allows retry when the fetch fails", async () => {
    const spy = vi.spyOn(anomaliesApi, "getAnomaly").mockRejectedValueOnce(new Error("network error"));

    render(
      <MemoryRouter initialEntries={["/anomalies/15"]}>
        <Routes><Route path="/anomalies/:id" element={<AnomalyDetailPage />} /></Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/no se pudo cargar/i)).toBeInTheDocument());

    spy.mockResolvedValueOnce(baseAnomaly({ id: 15, meter_id: "M-400" }));
    fireEvent.click(screen.getByRole("button", { name: /reintentar/i }));
    await waitFor(() => expect(screen.getByText(/M-400/)).toBeInTheDocument());
  });
});
