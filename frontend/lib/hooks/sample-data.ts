"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";
import { apiRequestWithMeta } from "@/lib/api/client";
import {
  scenarioCatalogSchema,
  simulationSchema,
} from "@/lib/api/routing-schemas";
import { freshSeed, invariantSummary } from "@/lib/hooks/dashboard-demo";

/** Fixed order; each is a real backend scenario. */
export const sampleScenarios = [
  "success",
  "decline",
  "duplicate_request",
  "timeout_after_success",
  "outage",
  "duplicate_posting",
  "duplicate_webhook",
  "settlement_mismatch",
] as const;

export type SampleOutcome =
  | { state: "pending" }
  | { state: "running" }
  | { state: "skipped"; text: string }
  | { state: "done"; text: string; passed: number; total: number }
  | { state: "error"; text: string };

export interface SampleRow {
  scenario: string;
  outcome: SampleOutcome;
}

export type SamplePhase = "idle" | "running" | "finished" | "stopped";

export const samplePollMs = 1_000;
const maxPolls = 60;

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

const initialRows = (): SampleRow[] =>
  sampleScenarios.map((scenario) => ({
    scenario,
    outcome: { state: "pending" },
  }));

/**
 * Runs the sample scenarios strictly one at a time: start, poll until the run
 * leaves "running", record its invariant summary, then the next. A failed
 * invariant is reported and the sequence continues; a network error stops it.
 * Workspace queries are refreshed at the end. Nothing starts until `start()`.
 */
export function useSampleData() {
  const client = useQueryClient();
  const [rows, setRows] = useState<SampleRow[]>(initialRows);
  const [phase, setPhase] = useState<SamplePhase>("idle");
  const busy = useRef(false);

  const patch = useCallback((scenario: string, outcome: SampleOutcome) => {
    setRows((current) =>
      current.map((row) =>
        row.scenario === scenario ? { ...row, outcome } : row,
      ),
    );
  }, []);

  const start = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    setRows(initialRows());
    setPhase("running");
    let stopped = false;
    try {
      let available: Map<string, boolean>;
      try {
        const catalog = await apiRequestWithMeta(
          "/v1/sandbox/scenarios",
          scenarioCatalogSchema,
        );
        available = new Map(
          catalog.data.items.map((item) => [item.id, item.implemented]),
        );
      } catch (error) {
        setRows((current) =>
          current.map((row, index) =>
            index === 0
              ? {
                  ...row,
                  outcome: {
                    state: "error",
                    text: `Could not load scenarios: ${error instanceof Error ? error.message : "network error"}`,
                  },
                }
              : row,
          ),
        );
        stopped = true;
        return;
      }
      for (const scenario of sampleScenarios) {
        if (!available.get(scenario)) {
          patch(scenario, {
            state: "skipped",
            text: "skipped: backend scenario not available",
          });
          continue;
        }
        patch(scenario, { state: "running" });
        try {
          const accepted = await apiRequestWithMeta(
            "/v1/sandbox/simulations",
            simulationSchema,
            {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({
                scenario,
                seed: freshSeed(),
                amount_minor: "10000",
                currency: "USD",
              }),
            },
          );
          let run = accepted.data;
          for (
            let polls = 0;
            run.status === "running" && polls < maxPolls;
            polls++
          ) {
            await wait(samplePollMs);
            run = (
              await apiRequestWithMeta(
                `/v1/sandbox/simulations/${encodeURIComponent(run.id)}`,
                simulationSchema,
              )
            ).data;
          }
          if (run.status === "running") {
            patch(scenario, {
              state: "error",
              text: "still running after 60 seconds",
            });
            stopped = true;
            break;
          }
          const summary = invariantSummary(run);
          patch(scenario, {
            state: "done",
            passed: summary.passed,
            total: summary.total,
            text:
              run.status === "failed"
                ? `run failed: ${run.failure_reason ?? "unknown"}; ${summary.text}`
                : summary.text,
          });
        } catch (error) {
          patch(scenario, {
            state: "error",
            text: error instanceof Error ? error.message : "network error",
          });
          stopped = true;
          break;
        }
      }
    } finally {
      busy.current = false;
      setPhase(stopped ? "stopped" : "finished");
      void client.invalidateQueries({ queryKey: ["railvanta"] });
    }
  }, [client, patch]);

  const settledCount = rows.filter(
    (row) => !["pending", "running"].includes(row.outcome.state),
  ).length;
  return { start, phase, rows, settledCount, total: rows.length };
}
