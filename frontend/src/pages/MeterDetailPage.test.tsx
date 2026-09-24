import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { DateRangeProvider } from "../context/DateRangeContext";
import MeterDetailPage from "./MeterDetailPage";
import * as metersApi from "../api/meters";
import * as analysisApi from "../api/analysis";

const baseMeter = {
  meter_id: "M-109",
  name: "M-109",
  consumption_kwh: 2180,
  baseline_kwh: 1070,
  variation_pct: 103.7,
  status: "CRITICAL" as const,
  analysis_id: 3,
  baseline: {
    hourly_profile: [1, 2, 3],
    voltage: 220,
    current: 10,
    power_factor: 0.95,
    change_point_at: null,
  },
  current_voltage: 221.5,
  current_current: 15.2,
  current_power_factor: 0.9,
};

const readings = {
  items: [
    { timestamp: "2026-09-01T00:00:00Z", consumption_kwh: 10, voltage_v: 220, current_a: 10, power_factor: 0.95 },
    { timestamp: "2026-09-02T00:00:00Z", consumption_kwh: 12, voltage_v: 221, current_a: 11, power_factor: 0.94 },
  ],
};

function renderPage() {
  render(
    <MemoryRouter initialEntries={["/meters/M-109"]}>
      <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
        <Routes>
          <Route path="/meters/:meterId" element={<MeterDetailPage />} />
        </Routes>
      </DateRangeProvider>
    </MemoryRouter>
  );
}

describe("MeterDetailPage", () => {
  it("renders meter info and readings chart, with anomaly band", async () => {
    vi.spyOn(metersApi, "getMeter").mockResolvedValue({
      ...baseMeter,
      anomaly: { id: 12, type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, priority_rank: 1, in_range: true, active_from: "2026-09-08T00:00:00Z", active_to: "2026-09-14T23:00:00Z" },
      latest_anomaly: { id: 12, type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, priority_rank: 1, in_range: true, active_from: "2026-09-08T00:00:00Z", active_to: "2026-09-14T23:00:00Z" },
    });
    vi.spyOn(metersApi, "getMeterReadings").mockResolvedValue(readings);
    vi.spyOn(metersApi, "getMeterEvents").mockResolvedValue({ items: [] });
    vi.spyOn(metersApi, "getMeterAnomalies").mockResolvedValue({ items: [] });

    renderPage();

    await waitFor(() => expect(screen.getByText("M-109")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: /analizar este medidor/i })).toBeInTheDocument();
    expect(screen.getByText("Sin eventos registrados")).toBeInTheDocument();
  });

  it("renders without crashing when the meter has NO anomaly (no anomaly band)", async () => {
    vi.spyOn(metersApi, "getMeter").mockResolvedValue({
      ...baseMeter,
      status: "OK",
      anomaly: null,
      latest_anomaly: null,
    });
    vi.spyOn(metersApi, "getMeterReadings").mockResolvedValue(readings);
    vi.spyOn(metersApi, "getMeterEvents").mockResolvedValue({
      items: [{ timestamp: "2026-09-03T00:00:00Z", type: "MAINTENANCE", description: "Revisión programada" }],
    });
    vi.spyOn(metersApi, "getMeterAnomalies").mockResolvedValue({ items: [] });

    renderPage();

    await waitFor(() => expect(screen.getByText("M-109")).toBeInTheDocument());
    expect(screen.getByText(/Revisión programada/)).toBeInTheDocument();
  });

  it("scopes the analysis to just this meter", async () => {
    vi.spyOn(metersApi, "getMeter").mockResolvedValue({ ...baseMeter, anomaly: null, latest_anomaly: null });
    vi.spyOn(metersApi, "getMeterReadings").mockResolvedValue(readings);
    vi.spyOn(metersApi, "getMeterEvents").mockResolvedValue({ items: [] });
    vi.spyOn(metersApi, "getMeterAnomalies").mockResolvedValue({ items: [] });
    const postAnalyze = vi.spyOn(analysisApi, "postAnalyze").mockResolvedValue({ id: 99 } as any);
    vi.spyOn(analysisApi, "getAnalysis").mockResolvedValue({
      id: 99, status: "PENDING", stage: "READINGS", progress: 0,
      summary: { anomalies: 0, priority: 0, avg_confidence: 0, message: "" },
    } as any);

    renderPage();

    await waitFor(() => expect(screen.getByText("M-109")).toBeInTheDocument());
    const button = screen.getByRole("button", { name: /analizar este medidor/i });
    button.click();

    await waitFor(() => expect(postAnalyze).toHaveBeenCalledWith({ meter_ids: ["M-109"] }));
  });

  it("lists the meter's current anomalies and switches to history on toggle", async () => {
    vi.spyOn(metersApi, "getMeter").mockResolvedValue({ ...baseMeter, anomaly: null, latest_anomaly: null });
    vi.spyOn(metersApi, "getMeterReadings").mockResolvedValue(readings);
    vi.spyOn(metersApi, "getMeterEvents").mockResolvedValue({ items: [] });
    const getMeterAnomalies = vi.spyOn(metersApi, "getMeterAnomalies").mockImplementation((_meterId, scope = "current") =>
      Promise.resolve({
        items:
          scope === "current"
            ? [{ id: 12, type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, status: "OPEN" }]
            : [
                { id: 12, type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, status: "OPEN" },
                { id: 5, type: "EXPLAINABLE_ANOMALY", severity: "LOW", confidence: 0.7, status: "RESOLVED" },
              ],
      })
    );

    renderPage();

    await waitFor(() => expect(getMeterAnomalies).toHaveBeenCalledWith("M-109", "current"));
    await waitFor(() => expect(screen.getByText("REAL_ANOMALY")).toBeInTheDocument());
    expect(screen.queryByText("EXPLAINABLE_ANOMALY")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /histórico/i }));

    await waitFor(() => expect(getMeterAnomalies).toHaveBeenCalledWith("M-109", "history"));
    await waitFor(() => expect(screen.getByText("EXPLAINABLE_ANOMALY")).toBeInTheDocument());
  });
});
