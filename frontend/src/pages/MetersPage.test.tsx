import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { DateRangeProvider } from "../context/DateRangeContext";
import MetersPage from "./MetersPage";
import * as metersApi from "../api/meters";

const ITEMS = [
  { meter_id: "M-109", name: "M-109", consumption_kwh: 2180, baseline_kwh: 1070, variation_pct: 103.7,
    status: "CRITICAL" as const, analysis_id: 3,
    anomaly: { id: 12, type: "REAL_ANOMALY" as const, severity: "HIGH" as const, confidence: 0.96, priority_rank: 1, in_range: true, active_from: "", active_to: "" } },
  { meter_id: "M-999", name: "M-999", consumption_kwh: null, baseline_kwh: null, variation_pct: null,
    status: "UNKNOWN" as const, analysis_id: null, anomaly: null },
];

describe("MetersPage", () => {
  it("renders a never-analyzed meter without crashing on null fields", async () => {
    vi.spyOn(metersApi, "getMeters").mockResolvedValue({ items: ITEMS });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <MetersPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("M-999")).toBeInTheDocument());
    expect(screen.getByText("M-109")).toBeInTheDocument();
  });

  it("filters by search query on meter_id", async () => {
    vi.spyOn(metersApi, "getMeters").mockResolvedValue({ items: ITEMS });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <MetersPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("M-109")).toBeInTheDocument());
    fireEvent.change(screen.getByPlaceholderText(/buscar/i), { target: { value: "M-999" } });
    expect(screen.queryByText("M-109")).not.toBeInTheDocument();
    expect(screen.getByText("M-999")).toBeInTheDocument();
  });

  it("filters by status", async () => {
    vi.spyOn(metersApi, "getMeters").mockResolvedValue({ items: ITEMS });

    render(
      <MemoryRouter>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <MetersPage />
        </DateRangeProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("M-109")).toBeInTheDocument());
    fireEvent.change(screen.getByDisplayValue(/todos/i), { target: { value: "critical" } });
    expect(screen.getByText("M-109")).toBeInTheDocument();
    expect(screen.queryByText("M-999")).not.toBeInTheDocument();
  });
});
