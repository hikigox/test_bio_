import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { DateRangeProvider } from "../context/DateRangeContext";
import AnomaliesPage from "./AnomaliesPage";
import * as anomaliesApi from "../api/anomalies";

describe("AnomaliesPage", () => {
  it("shows an empty state when no anomaly overlaps the selected range", async () => {
    vi.spyOn(anomaliesApi, "getAnomalies").mockResolvedValue({ items: [] });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-08-01T00:00:00Z", to: "2026-08-31T00:00:00Z" }}>
          <AnomaliesPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/sin anomalías/i)).toBeInTheDocument());
  });

  it("renders the anomalies table when items are present", async () => {
    vi.spyOn(anomaliesApi, "getAnomalies").mockResolvedValue({
      items: [{ id: 12, meter_id: "M-109", type: "REAL_ANOMALY", severity: "HIGH", confidence: 0.96, priority_rank: 1, in_range: true, active_from: "2026-09-08T00:00:00Z", active_to: "2026-09-14T23:00:00Z" }],
    });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <AnomaliesPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("M-109")).toBeInTheDocument());
  });
});
