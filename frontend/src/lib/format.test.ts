import { describe, it, expect } from "vitest";
import { formatNumberEsES, formatPercent, severityColor, meterColor } from "./format";

describe("formatNumberEsES", () => {
  it("formats with dot as thousands separator", () => {
    expect(formatNumberEsES(1860)).toBe("1.860");
  });
});

describe("formatPercent", () => {
  it("formats with comma as decimal separator and explicit sign", () => {
    expect(formatPercent(47.6)).toBe("+47,6 %");
    expect(formatPercent(-12.3)).toBe("-12,3 %");
  });
});

describe("severityColor", () => {
  it("maps HIGH to red, MEDIUM to amber, LOW to gray/blue", () => {
    expect(severityColor("HIGH")).toContain("red");
    expect(severityColor("MEDIUM")).toContain("amber");
    expect(severityColor("LOW")).toMatch(/blue|gray|slate/);
  });
});

describe("meterColor", () => {
  it("returns the same color for the same meter_id across calls", () => {
    const c1 = meterColor("M-109");
    const c2 = meterColor("M-109");
    expect(c1).toBe(c2);
  });

  it("returns different colors for different meters (statistically, within the palette)", () => {
    const colors = new Set(["M-101", "M-102", "M-103", "M-104"].map(meterColor));
    expect(colors.size).toBeGreaterThan(1);
  });
});
