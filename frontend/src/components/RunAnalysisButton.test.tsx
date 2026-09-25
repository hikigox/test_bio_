import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import RunAnalysisButton from "./RunAnalysisButton";
import * as analysisApi from "../api/analysis";
import { ApiError } from "../api/client";

describe("RunAnalysisButton", () => {
  it("shows a clear message when the server returns 409", async () => {
    vi.spyOn(analysisApi, "postAnalyze").mockRejectedValue(new Error("Ya hay un análisis en curso. Espera a que termine."));

    render(<RunAnalysisButton onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /run ai analysis/i }));

    await waitFor(() => expect(screen.getByText(/ya hay un análisis en curso/i)).toBeInTheDocument());
  });

  // Regresión: si el análisis desaparece mientras se está sondeando (p.ej. el
  // backend se reinició con otra base de datos y GET /ai/analysis/:id
  // devuelve 404), el botón se quedaba mostrando "Analizando…" para siempre
  // porque nada volvía a liberar analysisId. Ahora debe mostrar un error y
  // volver a habilitar el botón.
  it("recovers when the analysis disappears mid-poll (404) instead of hanging on 'Analizando…'", async () => {
    vi.spyOn(analysisApi, "postAnalyze").mockResolvedValue({ id: 22, status: "RUNNING" });
    vi.spyOn(analysisApi, "getAnalysis").mockRejectedValue(new ApiError(404, "análisis no encontrado"));

    render(<RunAnalysisButton onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /run ai analysis/i }));

    await waitFor(() => expect(screen.getByText(/se perdió el análisis en curso/i)).toBeInTheDocument());
    expect(screen.getByRole("button", { name: /run ai analysis/i })).not.toBeDisabled();
  });
});
