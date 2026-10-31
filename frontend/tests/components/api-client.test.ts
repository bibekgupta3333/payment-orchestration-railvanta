import { describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { apiRequest, ApiError, shouldRetryRead } from "@/lib/api/client";

describe("API boundary", () => {
  it("uses the same-origin path and validates a successful response", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ status: "ready" }), { status: 200 }));
    vi.stubGlobal("fetch", fetcher);
    await expect(apiRequest("/v1/probe", z.object({ status: z.string() }))).resolves.toEqual({ status: "ready" });
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher).toHaveBeenCalledWith("/v1/probe", expect.objectContaining({ credentials: "same-origin", cache: "no-store" }));
  });
  it("rejects invalid payloads instead of trusting TypeScript types", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"amount_minor": 25}', { status: 200 })));
    await expect(apiRequest("/v1/payments", z.object({ amount_minor: z.string() }))).rejects.toThrow();
  });
  it("parses safe errors and never automatically retries a request", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: "unauthorized", message: "Sign in", request_id: "request-1", field_errors: [] }), { status: 401 }));
    vi.stubGlobal("fetch", fetcher);
    await expect(apiRequest("/v1/payments", z.unknown(), { method: "POST" })).rejects.toBeInstanceOf(ApiError);
    expect(fetcher).toHaveBeenCalledOnce();
    expect(shouldRetryRead(0, new ApiError(401, { code: "unauthorized", message: "Sign in", request_id: "request-1", field_errors: [] }))).toBe(false);
    expect(shouldRetryRead(2, new Error("network"))).toBe(false);
  });
  it("rejects paths that escape the API boundary", async () => {
    const fetcher = vi.fn(); vi.stubGlobal("fetch", fetcher);
    await expect(apiRequest("/v1/../other", z.unknown())).rejects.toThrow("Invalid API path");
    expect(fetcher).not.toHaveBeenCalled();
  });
});
