import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { DateRangeProvider } from "../context/DateRangeContext";
import DashboardPage from "./DashboardPage";
import * as dashboardApi from "../api/dashboard";
import * as anomaliesApi from "../api/anomalies";

describe("DashboardPage", () => {
  it("renders KPIs and the pie chart without crashing when a meter has no anomaly", async () => {
    vi.spyOn(dashboardApi, "getDashboardSummary").mockResolvedValue({
      data_range: { from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" },
      period: { from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" },
      meters: 2, total_consumption_kwh: 3000, anomalies: 1, high_priority: 1, avg_confidence: 0.9,
      last_analysis: { at: "2026-09-14T23:00:00Z", status: "COMPLETED" },
      by_meter: [
        { meter_id: "M-109", consumption_kwh: 2180, share_pct: 72.67, status: "CRITICAL", severity: "HIGH" },
        { meter_id: "M-106", consumption_kwh: 820, share_pct: 27.33, status: "OK", severity: "" },
      ],
    });
    vi.spyOn(anomaliesApi, "getAnomalies").mockResolvedValue({ items: [] });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <DashboardPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/3\.000/)).toBeInTheDocument());
    expect(screen.getByText(/2026-09-14 23:00/)).toBeInTheDocument();
  });

  it("shows 'Sin análisis' when last_analysis is null", async () => {
    vi.spyOn(dashboardApi, "getDashboardSummary").mockResolvedValue({
      data_range: { from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" },
      period: { from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" },
      meters: 0, total_consumption_kwh: 0, anomalies: 0, high_priority: 0, avg_confidence: 0,
      last_analysis: null,
      by_meter: [],
    });
    vi.spyOn(anomaliesApi, "getAnomalies").mockResolvedValue({ items: [] });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <DashboardPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getAllByText("Sin análisis").length).toBeGreaterThan(0));
  });
});
