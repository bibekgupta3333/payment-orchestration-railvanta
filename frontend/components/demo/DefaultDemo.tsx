"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import {
  Badge,
  Button,
  Card,
  StatusPill,
  paymentStatusLabels,
} from "@/components/ui";
import { ApiError } from "@/lib/api/client";
import {
  freshSeed,
  invariantSummary,
  liveLabel,
} from "@/lib/hooks/dashboard-demo";
import {
  useScenarios,
  useSimulation,
  useStartSimulation,
} from "@/lib/hooks/routing";

/**
 * A "Default demo" card for a dashboard page. One click starts a REAL $100.00
 * USD sandbox simulation (the same backend scenarios the Playground uses),
 * shows live state and the backend's invariant results, and refreshes every
 * workspace query when the run settles so the page's own lists fill in.
 * Nothing runs on page load. Everything is labelled sandbox / simulated.
 */
export function DefaultDemo({
  id,
  scenario,
  title = "Default demo",
  description,
  actionLabel = "Run the default demo",
  producesLabel,
  children,
}: {
  /** Unique per page; used for ids and the `default-demo-<id>` test id. */
  id: string;
  /** A backend simulation scenario id (see GET /v1/sandbox/scenarios). */
  scenario: string;
  title?: string;
  /** One or two sentences: what the demo does and why it matters on this page. */
  description: string;
  actionLabel?: string;
  /** What appears on this page afterwards, e.g. "a posted journal". */
  producesLabel?: string;
  /** Optional extra content under the result (e.g. page-specific links). */
  children?: React.ReactNode;
}) {
  const client = useQueryClient();
  const scenarios = useScenarios();
  const start = useStartSimulation();
  const [runId, setRunId] = useState<string | null>(null);
  const simulation = useSimulation(runId);
  const run = simulation.query.data?.data ?? null;
  const running =
    start.isPending || (runId !== null && (!run || run.status === "running"));
  const settled = run && run.status !== "running" ? run : null;
  const summary = settled ? invariantSummary(settled) : null;
  const refreshed = useRef<string | null>(null);

  // When a run settles, refresh the page's data once.
  useEffect(() => {
    if (!settled || refreshed.current === settled.id) return;
    refreshed.current = settled.id;
    void client.invalidateQueries({ queryKey: ["railvanta"] });
  }, [settled, client]);

  const found = scenarios.query.data?.data.items.find(
    (item) => item.id === scenario,
  );
  const available = scenarios.query.data ? Boolean(found?.implemented) : false;
  const reason = !scenarios.query.data
    ? "Checking available scenarios…"
    : !found
      ? "backend scenario not available"
      : !found.implemented
        ? "backend scenario not implemented yet"
        : "";

  const begin = () => {
    setRunId(null);
    start.mutate(
      { scenario, seed: freshSeed(), amount_minor: "10000", currency: "USD" },
      { onSuccess: (accepted) => setRunId(accepted.id) },
    );
  };

  const status = settled?.payment?.status;
  const titleId = `default-demo-${id}-title`;

  return (
    <Card
      aria-labelledby={titleId}
      className="default-demo"
      data-testid={`default-demo-${id}`}
    >
      <div className="card-heading">
        <h2 id={titleId}>{title}</h2>
        <Badge>Simulated / sandbox</Badge>
      </div>
      <p className="muted">{description}</p>
      <div className="default-demo-actions">
        <Button
          type="button"
          onClick={begin}
          disabled={!available || running}
          aria-describedby={available ? undefined : `${titleId}-why`}
        >
          {running ? "Running…" : actionLabel}
        </Button>
        {producesLabel && (
          <span className="muted small">Creates {producesLabel}.</span>
        )}
        {!available && (
          <span id={`${titleId}-why`} className="muted small">
            Disabled: {reason}.
          </span>
        )}
      </div>
      <div className="sr-only" aria-live="polite">
        {settled
          ? `Demo finished. ${summary?.text ?? ""}`
          : running
            ? "Demo started."
            : ""}
      </div>
      {start.error && (
        <div className="field-error" role="alert">
          <p>
            {start.error.message}{" "}
            <span className="muted">
              Request ID:{" "}
              <code className="mono">
                {start.error instanceof ApiError
                  ? start.error.requestId
                  : "unavailable"}
              </code>
            </span>
          </p>
        </div>
      )}
      {running && (
        <p className="small" data-testid={`default-demo-${id}-live`}>
          {liveLabel(run)}
        </p>
      )}
      {settled && summary && (
        <div
          className="default-demo-result"
          data-testid={`default-demo-${id}-result`}
        >
          <p>
            {status && status in paymentStatusLabels ? (
              <StatusPill status={status as keyof typeof paymentStatusLabels} />
            ) : (
              <Badge>{settled.status}</Badge>
            )}{" "}
            <span className="small">{summary.text}</span>
          </p>
          {settled.status === "failed" && settled.failure_reason && (
            <p className="small">Run failed: {settled.failure_reason}</p>
          )}
          <p className="small">
            <Link className="text-link" href="/dashboard/playground">
              Open in Playground
            </Link>
          </p>
        </div>
      )}
      {children}
    </Card>
  );
}
