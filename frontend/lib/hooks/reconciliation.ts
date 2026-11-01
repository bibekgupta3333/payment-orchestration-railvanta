"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiRequestWithMeta, newRequestId } from "@/lib/api/client";
import {
  discrepancyPageSchema,
  discrepancySchema,
  reconciliationRunPageSchema,
  reconciliationRunSchema,
  type DiscrepancyStatus,
} from "@/lib/api/ledger-schemas";
import { useScopedRead } from "./payments";

export interface DiscrepancyFilters {
  run_id?: string;
  kind?: string;
  status?: string;
  cursor?: string;
}

export function useReconciliationRuns() {
  return useScopedRead(
    ["reconciliation", "runs"],
    "/v1/reconciliation/runs?limit=50",
    reconciliationRunPageSchema,
  );
}

export function useDiscrepancies(filters: DiscrepancyFilters, enabled = true) {
  const params = new URLSearchParams({ limit: "20" });
  for (const name of ["run_id", "kind", "status", "cursor"] as const) {
    const value = filters[name];
    if (value) params.set(name, value);
  }
  return useScopedRead(
    ["reconciliation", "discrepancies", filters],
    `/v1/reconciliation/discrepancies?${params.toString()}`,
    discrepancyPageSchema,
    { enabled },
  );
}

/** A fresh run key; keep it while a start request is unconfirmed. */
export function newRunKey(): string {
  return newRequestId("recon");
}

/** The default window: the last 24 hours, with a minute of clock tolerance. */
export function defaultWindow(now = Date.now()) {
  return {
    window_start: new Date(now - 24 * 3_600_000).toISOString(),
    window_end: new Date(now + 60_000).toISOString(),
  };
}

export function useStartReconciliation() {
  const queryClient = useQueryClient();
  return useMutation({
    retry: false,
    mutationFn: ({
      runKey,
      window,
    }: {
      runKey: string;
      window: { window_start: string; window_end: string };
    }) =>
      apiRequestWithMeta("/v1/reconciliation/runs", reconciliationRunSchema, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": runKey,
        },
        body: JSON.stringify(window),
        timeoutMs: 30_000,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["railvanta"] }),
  });
}

export function useResolveDiscrepancy() {
  const queryClient = useQueryClient();
  return useMutation({
    retry: false,
    mutationFn: ({
      id,
      status,
      note,
    }: {
      id: string;
      status: DiscrepancyStatus;
      note: string;
    }) =>
      apiRequestWithMeta(
        `/v1/reconciliation/discrepancies/${encodeURIComponent(id)}/resolve`,
        discrepancySchema,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ status, note }),
        },
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["railvanta"] }),
  });
}
