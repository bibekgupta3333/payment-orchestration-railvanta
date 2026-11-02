"use client";

import type { ApiResult } from "@/lib/api/client";
import {
  sandboxSamplesSchema,
  type SandboxSamples,
} from "@/lib/api/sample-schemas";
import { useScopedRead } from "@/lib/hooks/payments";

/** Poll cadence while any sample run is still running. */
export const samplesPollMs = 3_000;

export function samplesRefetchInterval(
  data: ApiResult<SandboxSamples> | undefined,
): number | false {
  return data?.data.items.some((item) => item.status === "running")
    ? samplesPollMs
    : false;
}

/**
 * Server-side sample runs. The key is scoped under ["railvanta", ws, env], so
 * the Load sample data card's invalidation of ["railvanta"] refreshes it.
 */
export function useSandboxSamples() {
  return useScopedRead(
    ["sandbox", "samples"],
    "/v1/sandbox/samples",
    sandboxSamplesSchema,
    { refetchInterval: samplesRefetchInterval },
  );
}
