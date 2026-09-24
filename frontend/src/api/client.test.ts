import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiFetch, ApiError, setUnauthorizedHandler } from "./client";

describe("apiFetch", () => {
  beforeEach(() => {
    localStorage.clear();
    setUnauthorizedHandler(() => {});
    vi.stubGlobal("fetch", vi.fn());
  });

  it("adds Authorization header when a token is stored", async () => {
    localStorage.setItem("token", "abc123");
    (fetch as any).mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));

    await apiFetch("/api/meters");

    const [, opts] = (fetch as any).mock.calls[0];
    expect(opts.headers.Authorization).toBe("Bearer abc123");
  });

  it("does not add an Authorization header when no token is stored", async () => {
    (fetch as any).mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));

    await apiFetch("/api/meters");

    const [, opts] = (fetch as any).mock.calls[0];
    expect(opts.headers.Authorization).toBeUndefined();
  });

  it("throws ApiError with the server message on non-2xx", async () => {
    (fetch as any).mockResolvedValue(new Response(JSON.stringify({ error: "credenciales inválidas" }), { status: 401 }));

    await expect(apiFetch("/api/meters")).rejects.toThrow(ApiError);
  });

  it("calls the unauthorized handler on 401", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    (fetch as any).mockResolvedValue(new Response(JSON.stringify({ error: "token inválido" }), { status: 401 }));

    await expect(apiFetch("/api/meters")).rejects.toThrow();
    expect(handler).toHaveBeenCalled();
  });

  it("does not call the unauthorized handler on non-401 errors", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    (fetch as any).mockResolvedValue(new Response(JSON.stringify({ error: "error interno" }), { status: 500 }));

    await expect(apiFetch("/api/meters")).rejects.toThrow(ApiError);
    expect(handler).not.toHaveBeenCalled();
  });

  it("resolves with the parsed JSON body on success", async () => {
    (fetch as any).mockResolvedValue(new Response(JSON.stringify({ token: "xyz" }), { status: 200 }));

    const result = await apiFetch<{ token: string }>("/api/auth/login", { method: "POST" });

    expect(result).toEqual({ token: "xyz" });
  });
});
