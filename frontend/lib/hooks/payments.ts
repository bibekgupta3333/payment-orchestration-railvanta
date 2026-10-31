"use client";

import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { z } from "zod";
import {
  ApiError,
  apiRequestWithMeta,
  shouldRetryRead,
  type ApiResult,
} from "@/lib/api/client";
import {
  attemptPageSchema,
  ledgerJournalPageSchema,
  metricSummarySchema,
  paymentHistorySchema,
  paymentMethodPageSchema,
  paymentPageSchema,
  paymentSchema,
  paymentVolumeSchema,
  terminalStatuses,
  workspaceSchema,
  type LedgerJournalPage,
  type MetricWindow,
  type Payment,
  type PaymentStatus,
  type Workspace,
} from "@/lib/api/schemas";

/** Poll cadence for non-terminal payments (created, processing, unknown). */
export const pollIntervalMs = 3_000;

export function isTerminal(status: PaymentStatus): boolean {
  return terminalStatuses.includes(status);
}

export type Scope = Pick<Workspace, "id" | "environment">;

/** Every workspace-owned cache key starts with workspace and environment. */
export function scopedKey(scope: Scope | undefined, ...parts: unknown[]) {
  return [
    "railvanta",
    scope?.id ?? "none",
    scope?.environment ?? "none",
    ...parts,
  ];
}

export function useWorkspace(): UseQueryResult<ApiResult<Workspace>, unknown> {
  return useQuery({
    queryKey: ["railvanta", "workspace"],
    queryFn: ({ signal }) =>
      apiRequestWithMeta("/v1/workspace", workspaceSchema, { signal }),
    retry: shouldRetryRead,
    staleTime: 60_000,
  });
}

/**
 * A workspace-scoped read. It waits for the workspace so cache entries can
 * never be shared across workspaces or environments, and surfaces the
 * workspace error when that read fails.
 */
function useScopedRead<T>(
  parts: unknown[],
  path: `/v1/${string}`,
  schema: z.ZodType<T>,
  options: {
    enabled?: boolean;
    refetchInterval?: (data: ApiResult<T> | undefined) => number | false;
  } = {},
) {
  const workspace = useWorkspace();
  const scope = workspace.data?.data;
  const query = useQuery({
    queryKey: scopedKey(scope, ...parts),
    queryFn: ({ signal }) => apiRequestWithMeta(path, schema, { signal }),
    enabled: Boolean(scope) && options.enabled !== false,
    retry: shouldRetryRead,
    refetchInterval: options.refetchInterval
      ? (current) => options.refetchInterval?.(current.state.data) ?? false
      : undefined,
    refetchIntervalInBackground: false,
  });
  return { query, workspace, scope };
}

export interface PaymentFilters {
  q?: string;
  status?: string;
  rail?: string;
  provider_id?: string;
  currency?: string;
  created_from?: string;
  created_to?: string;
  amount_min?: string;
  amount_max?: string;
  sort?: string;
  cursor?: string;
  limit?: string;
}

export const filterNames: (keyof PaymentFilters)[] = [
  "q",
  "status",
  "rail",
  "provider_id",
  "currency",
  "created_from",
  "created_to",
  "amount_min",
  "amount_max",
  "sort",
  "cursor",
  "limit",
];

export function paymentQueryString(filters: PaymentFilters): string {
  const params = new URLSearchParams();
  for (const name of filterNames) {
    const value = filters[name];
    if (value) params.set(name, value);
  }
  const text = params.toString();
  return text ? `?${text}` : "";
}

export function usePayments(filters: PaymentFilters, enabled = true) {
  return useScopedRead(
    ["payments", "list", filters],
    `/v1/payments${paymentQueryString(filters)}`,
    paymentPageSchema,
    { enabled },
  );
}

export function usePayment(id: string) {
  return useScopedRead(
    ["payments", "detail", id],
    `/v1/payments/${encodeURIComponent(id)}`,
    paymentSchema,
    {
      refetchInterval: (data) =>
        data && isTerminal(data.data.status) ? false : pollIntervalMs,
    },
  );
}

export function usePaymentAttempts(id: string, payment?: Payment) {
  return useScopedRead(
    ["payments", "attempts", id],
    `/v1/payments/${encodeURIComponent(id)}/attempts`,
    attemptPageSchema,
    {
      refetchInterval: () =>
        payment && isTerminal(payment.status) ? false : pollIntervalMs,
    },
  );
}

export function usePaymentHistory(id: string, payment?: Payment) {
  return useScopedRead(
    ["payments", "history", id],
    `/v1/payments/${encodeURIComponent(id)}/history`,
    paymentHistorySchema,
    {
      refetchInterval: () =>
        payment && isTerminal(payment.status) ? false : pollIntervalMs,
    },
  );
}

export type JournalRead =
  | { available: true; page: LedgerJournalPage }
  | { available: false; requestId: string };

/**
 * Journals for one payment. Until ledger reads exist the API answers 501 (or
 * 404 for an unknown route); that is reported as "not available", never as
 * an empty journal.
 */
export function usePaymentJournal(id: string) {
  const workspace = useWorkspace();
  const scope = workspace.data?.data;
  return useQuery({
    queryKey: scopedKey(scope, "ledger", "payment", id),
    enabled: Boolean(scope),
    retry: shouldRetryRead,
    queryFn: async ({ signal }): Promise<ApiResult<JournalRead>> => {
      try {
        const result = await apiRequestWithMeta(
          `/v1/ledger/transactions?payment_id=${encodeURIComponent(id)}`,
          ledgerJournalPageSchema,
          { signal },
        );
        return { ...result, data: { available: true, page: result.data } };
      } catch (error) {
        if (error instanceof ApiError && [404, 501].includes(error.status)) {
          return {
            data: { available: false, requestId: error.requestId },
            status: error.status,
            requestId: error.requestId,
            correlationId: null,
            headers: new Headers(),
            receivedAt: Date.now(),
          };
        }
        throw error;
      }
    },
  });
}

export function usePaymentMethods() {
  return useScopedRead(
    ["payment-methods"],
    "/v1/payment-methods",
    paymentMethodPageSchema,
  );
}

export function useDashboardSummary(window: MetricWindow) {
  return useScopedRead(
    ["dashboard", "summary", window],
    `/v1/dashboard/summary?window=${window}`,
    metricSummarySchema,
    { refetchInterval: () => 30_000 },
  );
}

export function usePaymentVolume(window: MetricWindow) {
  return useScopedRead(
    ["dashboard", "volume", window],
    `/v1/dashboard/payment-volume?window=${window}`,
    paymentVolumeSchema,
    { refetchInterval: () => 30_000 },
  );
}
