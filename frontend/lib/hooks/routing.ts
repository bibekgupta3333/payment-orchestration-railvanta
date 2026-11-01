"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { z } from "zod";
import {
  ApiError,
  apiRequestWithMeta,
  shouldRetryRead,
  type ApiResult,
} from "@/lib/api/client";
import {
  providerDetailSchema,
  providerHealthPageSchema,
  providerPageSchema,
  routingDecisionSchema,
  routingRulePageSchema,
  scenarioCatalogSchema,
  simulationSchema,
  type RoutingDecision,
  type Simulation,
} from "@/lib/api/routing-schemas";
import { scopedKey, useWorkspace } from "@/lib/hooks/payments";

/** Poll cadence for a running simulation. Manual refresh is always offered. */
export const simulationPollMs = 1_000;

function useRead<T>(
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

export function useRoutingRules() {
  return useRead(
    ["routing", "rules"],
    "/v1/routing/rules",
    routingRulePageSchema,
  );
}

export function useProviders() {
  return useRead(["providers", "list"], "/v1/providers", providerPageSchema);
}

export function useProviderDetail(id: string) {
  return useRead(
    ["providers", "detail", id],
    `/v1/providers/${encodeURIComponent(id)}`,
    providerDetailSchema,
  );
}

export function useProviderHealth() {
  return useRead(
    ["providers", "health"],
    "/v1/providers/health",
    providerHealthPageSchema,
  );
}

export function useScenarios() {
  return useRead(
    ["sandbox", "scenarios"],
    "/v1/sandbox/scenarios",
    scenarioCatalogSchema,
  );
}

export function useSimulation(id: string | null) {
  return useRead(
    ["sandbox", "simulation", id],
    `/v1/sandbox/simulations/${encodeURIComponent(id ?? "none")}`,
    simulationSchema,
    {
      enabled: Boolean(id),
      refetchInterval: (data) =>
        data && data.data.status !== "running" ? false : simulationPollMs,
    },
  );
}

export interface EvaluationInput {
  payment_method_id: string;
  amount_minor: string;
  currency: string;
  payment_id?: string;
}

export interface EvaluationFailure {
  message: string;
  requestId: string;
  status: number;
  /** Per-provider reasons from a 422 no_eligible_provider response. */
  reasons: { field: string; message: string }[];
}

/** Evaluation is read-only on the server; it is still a POST (operator role). */
export function useEvaluateRouting() {
  return useMutation<RoutingDecision, EvaluationFailure, EvaluationInput>({
    mutationFn: async (input) => {
      try {
        const result = await apiRequestWithMeta(
          "/v1/routing/evaluate",
          routingDecisionSchema,
          {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(input),
          },
        );
        return result.data;
      } catch (error) {
        if (error instanceof ApiError) {
          throw {
            message: error.message,
            requestId: error.requestId,
            status: error.status,
            reasons: error.details.field_errors,
          } satisfies EvaluationFailure;
        }
        throw {
          message: "Routing evaluation failed.",
          requestId: "unavailable",
          status: 0,
          reasons: [],
        } satisfies EvaluationFailure;
      }
    },
  });
}

export interface StartSimulationInput {
  scenario: string;
  seed: string;
  /** Overrides the seed-derived amount, in minor units (decimal string). */
  amount_minor?: string;
  currency?: string;
}

export function useStartSimulation() {
  const client = useQueryClient();
  const workspace = useWorkspace();
  return useMutation<Simulation, ApiError, StartSimulationInput>({
    mutationFn: async (input) => {
      const result = await apiRequestWithMeta(
        "/v1/sandbox/simulations",
        simulationSchema,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(input),
        },
      );
      return result.data;
    },
    onSuccess: (run) => {
      client.setQueryData(
        scopedKey(workspace.data?.data, "sandbox", "simulation", run.id),
        {
          data: run,
          status: 202,
          requestId: "accepted",
          correlationId: null,
          headers: new Headers(),
          receivedAt: Date.now(),
        } satisfies ApiResult<Simulation>,
      );
    },
  });
}
