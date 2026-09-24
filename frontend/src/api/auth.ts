import { apiFetch } from "./client";

export async function login(email: string, password: string): Promise<string> {
  const res = await apiFetch<{ token: string }>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
  return res.token;
}
