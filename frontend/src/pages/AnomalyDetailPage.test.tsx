import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import AnomalyDetailPage from "./AnomalyDetailPage";
import * as anomaliesApi from "../api/anomalies";

describe("AnomalyDetailPage", () => {
  it("renders reason, evidence, and allows changing status", async () => {
    vi.spyOn(anomaliesApi, "getAnomaly").mockResolvedValue({
      id: 12, meter_id: "M-109", type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, status: "OPEN",
      reason: "Consumo 103.7% por encima del baseline...", recommended_action: "Investigar medidor e instalación",
      evidence: { decision_path: ["no_data_quality_issue"], confidence_breakdown: {}, data_quality_issues: [], affected_readings: { count: 168, first: "", last: "" } },
    });
    const patchSpy = vi.spyOn(anomaliesApi, "patchAnomaly").mockResolvedValue({ status: "INVESTIGATING" });
    const getAnomalySpy = anomaliesApi.getAnomaly as ReturnType<typeof vi.fn>;

    render(
      <MemoryRouter initialEntries={["/anomalies/12"]}>
        <Routes><Route path="/anomalies/:id" element={<AnomalyDetailPage />} /></Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/103.7/)).toBeInTheDocument());
    expect(screen.getByText(/Estado: OPEN/)).toBeInTheDocument();

    getAnomalySpy.mockResolvedValueOnce({
      id: 12, meter_id: "M-109", type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, status: "INVESTIGATING",
      reason: "Consumo 103.7% por encima del baseline...", recommended_action: "Investigar medidor e instalación",
      evidence: { decision_path: ["no_data_quality_issue"], confidence_breakdown: {}, data_quality_issues: [], affected_readings: { count: 168, first: "", last: "" } },
    });

    fireEvent.click(screen.getByRole("button", { name: /investigar/i }));
    await waitFor(() => expect(patchSpy).toHaveBeenCalledWith(12, "INVESTIGATING"));
    await waitFor(() => expect(screen.getByText(/Estado: INVESTIGATING/)).toBeInTheDocument());
  });

  it("shows an empty state when the anomaly has no data quality issues", async () => {
    vi.spyOn(anomaliesApi, "getAnomaly").mockResolvedValue({
      id: 13, meter_id: "M-200", type: "REAL_ANOMALY", severity: "LOW", confidence: 0.7, status: "RESOLVED",
      reason: "reason", recommended_action: "action",
      evidence: { decision_path: [], confidence_breakdown: {}, data_quality_issues: [], affected_readings: { count: 0, first: "", last: "" } },
    });

    render(
      <MemoryRouter initialEntries={["/anomalies/13"]}>
        <Routes><Route path="/anomalies/:id" element={<AnomalyDetailPage />} /></Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/M-200/)).toBeInTheDocument());
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
  });
});
