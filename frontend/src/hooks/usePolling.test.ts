import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { usePolling } from "./usePolling";

describe("usePolling", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("polls repeatedly until the predicate says done", async () => {
    let calls = 0;
    const fetcher = vi.fn(async () => {
      calls++;
      return { status: calls >= 3 ? "COMPLETED" : "RUNNING" };
    });

    const { result } = renderHook(() =>
      usePolling(fetcher, (data) => data?.status === "COMPLETED", 100)
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(result.current.data?.status).toBe("COMPLETED");
    expect(calls).toBe(3);
  });

  it("does not start a second polling loop when re-rendered with the same id", async () => {
    const fetcher = vi.fn(async () => ({ status: "RUNNING" }));
    const { rerender } = renderHook(({ id }) => usePolling(fetcher, () => false, 100, id), {
      initialProps: { id: 1 },
    });
    rerender({ id: 1 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    // con id estable, solo un intervalo activo: ~2-3 llamadas, no el doble
    expect(fetcher.mock.calls.length).toBeLessThan(5);
  });
});
