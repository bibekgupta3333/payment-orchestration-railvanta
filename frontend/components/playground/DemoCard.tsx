"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button, Card, ErrorState } from "@/components/ui";
import { ApiError } from "@/lib/api/client";
import type { Simulation } from "@/lib/api/routing-schemas";
import {
  failedInvariants,
  unavailableReason,
  type Demo,
} from "@/lib/hooks/playground";
import { useSimulation, useStartSimulation } from "@/lib/hooks/routing";
import { RunResult } from "./RunResult";

export interface RunSignal {
  id: string;
  nonce: number;
}

export interface SettledRun {
  demoId: string;
  ok: boolean;
  run: Simulation;
}

function freshSeed(): string {
  return `pg-${Date.now().toString(36)}${Math.floor(Math.random() * 1296).toString(36)}`;
}

export function DemoCard({
  demo,
  scenarioIds,
  scenarioImplemented,
  signal,
  onSettled,
  disabled,
  children,
}: {
  demo: Demo;
  /** Scenario IDs from GET /v1/sandbox/scenarios; null while loading. */
  scenarioIds: Set<string> | null;
  scenarioImplemented: (id: string) => boolean;
  signal: RunSignal | null;
  onSettled: (result: SettledRun) => void;
  /** Another run (run all) is in progress. */
  disabled: boolean;
  children?: ReactNode;
}) {
  const start = useStartSimulation();
  const [runId, setRunId] = useState<string | null>(null);
  const [lastScenario, setLastScenario] = useState(demo.scenario);
  const simulation = useSimulation(runId);
  const resultRef = useRef<HTMLDivElement>(null);
  const settledFor = useRef<string | null>(null);
  const focusAfter = useRef(false);
  const handledNonce = useRef(0);

  const run = simulation.query.data?.data ?? null;

  const begin = (scenario: string, focus: boolean) => {
    focusAfter.current = focus;
    setLastScenario(scenario);
    start.mutate(
      // The demo narrative is a $100.00 sandbox payment.
      { scenario, seed: freshSeed(), amount_minor: "10000", currency: "USD" },
      { onSuccess: (accepted) => setRunId(accepted.id) },
    );
  };

  const available = (scenario: string | null) =>
    scenario === null
      ? true
      : scenarioIds !== null &&
        scenarioIds.has(scenario) &&
        scenarioImplemented(scenario);

  // Run-all: start when the parent signals this card.
  useEffect(() => {
    if (
      signal &&
      signal.id === demo.id &&
      signal.nonce !== handledNonce.current &&
      demo.scenario
    ) {
      handledNonce.current = signal.nonce;
      begin(demo.scenario, false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signal, demo.id, demo.scenario]);

  // Report each run once when it reaches a terminal state.
  useEffect(() => {
    if (!run || run.status === "running" || settledFor.current === run.id)
      return;
    settledFor.current = run.id;
    onSettled({
      demoId: demo.id,
      ok: run.status === "completed" && failedInvariants(run).length === 0,
      run,
    });
    if (focusAfter.current) {
      focusAfter.current = false;
      resultRef.current?.focus();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run]);

  const running =
    start.isPending || (Boolean(runId) && (!run || run.status === "running"));
  const mainAvailable = available(demo.scenario);
  const contrastAvailable = demo.contrast
    ? available(demo.contrast.scenario)
    : false;
  const headingId = `demo-${demo.id}-title`;
  const error = start.error;

  return (
    <Card aria-labelledby={headingId} data-testid={`demo-${demo.id}`}>
      <div className="card-heading">
        <h2 id={headingId}>
          {demo.number}. {demo.title}
        </h2>
        <span className="badge">Simulated / sandbox</span>
      </div>
      <dl className="detail-list">
        <div>
          <dt>What happens</dt>
          <dd>{demo.what}</dd>
        </div>
        <div>
          <dt>What this proves</dt>
          <dd>{demo.proves}</dd>
        </div>
      </dl>
      {demo.scenario && (
        <div className="mt-3 flex flex-wrap gap-2">
          <Button
            onClick={() => demo.scenario && begin(demo.scenario, true)}
            disabled={!mainAvailable || running || disabled}
            aria-describedby={mainAvailable ? undefined : `${headingId}-reason`}
          >
            {running && lastScenario === demo.scenario
              ? "Running…"
              : `Run demo ${demo.number}`}
          </Button>
          {demo.contrast && (
            <Button
              className="button-quiet"
              onClick={() =>
                demo.contrast && begin(demo.contrast.scenario, true)
              }
              disabled={!contrastAvailable || running || disabled}
              title={demo.contrast.what}
            >
              {running && lastScenario === demo.contrast.scenario
                ? "Running…"
                : demo.contrast.label}
            </Button>
          )}
        </div>
      )}
      {demo.scenario && scenarioIds !== null && !mainAvailable && (
        <p id={`${headingId}-reason`} className="muted small" role="note">
          Disabled: {unavailableReason}.
        </p>
      )}
      {demo.contrast && <p className="muted small">{demo.contrast.what}</p>}
      {children}
      {error && (
        <ErrorState
          title="This demo could not start"
          message={error.message}
          requestId={
            error instanceof ApiError ? error.requestId : "unavailable"
          }
          retry={() => lastScenario && begin(lastScenario, true)}
        />
      )}
      <div ref={resultRef} tabIndex={-1} className="outline-offset-4">
        {runId && simulation.query.isError && !run && (
          <ErrorState
            title="Run result unavailable"
            message={
              simulation.query.error instanceof ApiError
                ? simulation.query.error.message
                : "The run could not be read."
            }
            requestId={
              simulation.query.error instanceof ApiError
                ? simulation.query.error.requestId
                : "unavailable"
            }
            retry={() => void simulation.query.refetch()}
          />
        )}
        {run && <RunResult run={run} />}
      </div>
    </Card>
  );
}
