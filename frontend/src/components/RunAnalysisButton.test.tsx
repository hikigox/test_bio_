import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import RunAnalysisButton from "./RunAnalysisButton";
import * as analysisApi from "../api/analysis";

describe("RunAnalysisButton", () => {
  it("shows a clear message when the server returns 409", async () => {
    vi.spyOn(analysisApi, "postAnalyze").mockRejectedValue(new Error("Ya hay un análisis en curso. Espera a que termine."));

    render(<RunAnalysisButton onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /run ai analysis/i }));

    await waitFor(() => expect(screen.getByText(/ya hay un análisis en curso/i)).toBeInTheDocument());
  });
});
