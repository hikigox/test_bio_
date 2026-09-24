import { test, expect } from "@playwright/test";

test("login, run analysis, and reach M-109's investigation screen", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel(/correo|email/i).fill("demo@energy.local");
  await page.getByLabel(/contraseña|password/i).fill("demo1234");
  await page.getByRole("button", { name: /entrar|iniciar sesión/i }).click();

  // Login redirects to "/" (the dashboard route), not "/dashboard" — see
  // frontend/src/App.tsx's route table.
  await expect(page).toHaveURL("/");
  await expect(page.getByText(/12/).first()).toBeVisible();

  await page.getByRole("button", { name: /run ai analysis|ejecutar análisis/i }).click();
  // RunAnalysisButton (frontend/src/components/RunAnalysisButton.tsx) has no
  // literal "completado"/"completed" copy; on COMPLETED it renders the
  // backend's summary message, e.g. "N anomalías detectadas · M requieren
  // atención prioritaria" (backend/internal/api/analysis_runner.go). That's
  // the real, deterministic signal that the run finished.
  await expect(page.getByText(/anomalías detectadas/i)).toBeVisible({ timeout: 25_000 });

  await page.getByRole("link", { name: /anomalías/i }).click();
  await expect(page).toHaveURL(/\/anomalies/);
  await expect(page.getByText("M-109").first()).toBeVisible();

  await page.getByText("M-109").first().click();
  await expect(page).toHaveURL(/\/anomalies\/\d+/);
  await expect(page.getByText(/acción recomendada|recommended action/i)).toBeVisible();
});
