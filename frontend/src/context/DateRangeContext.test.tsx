import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { DateRangeProvider, useDateRange } from "./DateRangeContext";

function Probe() {
  const { range, setPreset } = useDateRange();
  return (
    <div>
      <span data-testid="from">{range.from}</span>
      <button onClick={() => setPreset("last_7_days")}>7 días</button>
    </div>
  );
}

describe("DateRangeContext", () => {
  it("defaults to the full period when no query params are present", () => {
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <Probe />
        </DateRangeProvider>
      </MemoryRouter>
    );
    expect(screen.getByTestId("from").textContent).toBe("2026-09-01T00:00:00Z");
  });

  it("updates the range when a preset is selected", () => {
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <DateRangeProvider dataRange={{ from: "2026-09-01T00:00:00Z", to: "2026-09-14T23:00:00Z" }}>
          <Probe />
        </DateRangeProvider>
      </MemoryRouter>
    );
    fireEvent.click(screen.getByText("7 días"));
    expect(screen.getByTestId("from").textContent).not.toBe("2026-09-01T00:00:00Z");
  });
});
