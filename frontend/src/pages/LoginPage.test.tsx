import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { AuthProvider } from "../context/AuthContext";
import LoginPage from "./LoginPage";
import * as authApi from "../api/auth";

describe("LoginPage", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("calls login and stores the token on submit", async () => {
    vi.spyOn(authApi, "login").mockResolvedValue("fake-token");

    render(
      <MemoryRouter>
        <AuthProvider>
          <LoginPage />
        </AuthProvider>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "demo@energy.local" } });
    fireEvent.change(screen.getByLabelText(/contraseña/i), { target: { value: "demo1234" } });
    fireEvent.click(screen.getByRole("button", { name: /entrar/i }));

    await waitFor(() => expect(localStorage.getItem("token")).toBe("fake-token"));
  });

  it("shows an error message on invalid credentials", async () => {
    vi.spyOn(authApi, "login").mockRejectedValue(new Error("credenciales inválidas"));

    render(
      <MemoryRouter>
        <AuthProvider>
          <LoginPage />
        </AuthProvider>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "x@x.com" } });
    fireEvent.change(screen.getByLabelText(/contraseña/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /entrar/i }));

    await waitFor(() => expect(screen.getByText(/credenciales inválidas/i)).toBeInTheDocument());
  });
});
