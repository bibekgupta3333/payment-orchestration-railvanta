"use client";

import Link from "next/link";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { CreatePaymentDialog } from "@/components/payments/CreatePaymentDialog";
import { GeneratedKey, curlExample } from "@/components/settings/GeneratedKey";
import { Badge, Button, Card, StatusPill } from "@/components/ui";
import { CopyCode } from "@/components/ui/copy-code";
import { ApiError } from "@/lib/api/client";
import { useApiKeys, useCreateApiKey } from "@/lib/hooks/apikeys";
import {
  activeKeyCount,
  defaultKeyInput,
  freshSeed,
  hasActiveDefaultKey,
  heroDemos,
  invariantSummary,
  liveLabel,
  type HeroScenario,
} from "@/lib/hooks/dashboard-demo";
import { sampleScenarios, useSampleData } from "@/lib/hooks/sample-data";
import { useSettings } from "@/lib/hooks/events";
import { usePaymentMethods } from "@/lib/hooks/payments";
import { paymentStatusLabels } from "@/components/ui";
import {
  useScenarios,
  useSimulation,
  useStartSimulation,
} from "@/lib/hooks/routing";

const flow = [
  { step: "Browser", detail: "Next.js dashboard" },
  { step: "API", detail: "Go · same-origin /v1" },
  { step: "Accept once", detail: "Idempotency-Key" },
  { step: "Route", detail: "Rule engine dispatch" },
  { step: "Mock provider", detail: "Deterministic sandbox" },
  { step: "Journal", detail: "Balanced ledger posting" },
] as const;

export function HubHeading() {
  return (
    <div className="page-heading page-heading-actions">
      <div>
        <p className="eyebrow">Home</p>
        <h1>Railvanta sandbox</h1>
        <p>
          Try the API, run a guided demo or watch live sandbox metrics. Nothing
          here moves real money.
        </p>
      </div>
      <Badge>Sandbox</Badge>
    </div>
  );
}

const subscribeNever = () => () => {};

function focusCredentials() {
  const card = document.getElementById("integration-credentials");
  if (!card) return;
  card.scrollIntoView?.({ block: "start" });
  card.focus({ preventScroll: true });
}

export function QuickActions() {
  const [open, setOpen] = useState(false);
  const methods = usePaymentMethods();
  const items = methods.query.data?.data.items ?? [];
  const firstMock = items.find((method) => method.rail === "mock") ?? items[0];
  return (
    <section aria-labelledby="hub-actions-title" className="hub-section">
      <h2 id="hub-actions-title">Quick actions</h2>
      <ul className="hub-actions">
        <li>
          <Link className="hub-action" href="/dashboard/playground">
            <strong>Run a demo</strong>
            <span className="muted small">Guided, simulated scenarios</span>
          </Link>
        </li>
        <li>
          <button
            type="button"
            className="hub-action"
            onClick={() => setOpen(true)}
            disabled={items.length === 0}
          >
            <strong>Create a $100 payment</strong>
            <span className="muted small">Sandbox, mock rail</span>
          </button>
        </li>
        <li>
          <Link className="hub-action" href="/dashboard/api-reference">
            <strong>API reference (Swagger)</strong>
            <span className="muted small">Browse and try the endpoints</span>
          </Link>
        </li>
        <li>
          <a
            className="hub-action"
            href="#integration-credentials"
            onClick={(event) => {
              event.preventDefault();
              focusCredentials();
            }}
          >
            <strong>Generate API key</strong>
            <span className="muted small">Jump to integration credentials</span>
          </a>
        </li>
      </ul>
      <CreatePaymentDialog
        open={open}
        onClose={() => setOpen(false)}
        methods={items}
        resume={
          firstMock
            ? {
                amount_minor: "10000",
                currency: "USD",
                payment_method_id: firstMock.id,
                merchant_reference: "playground-demo",
              }
            : null
        }
      />
    </section>
  );
}

export function CredentialsCard() {
  const settings = useSettings();
  const role = settings.query.data?.data.role ?? null;
  const isOperator = role === "operator";
  const list = useApiKeys(isOperator);
  const create = useCreateApiKey();
  const created = create.data;
  const panelRef = useRef<HTMLDivElement>(null);
  const origin = useSyncExternalStore(
    subscribeNever,
    () => window.location.origin,
    () => "",
  );

  useEffect(() => {
    if (created) panelRef.current?.focus();
  }, [created]);

  const items = list.query.data?.data.items;
  const limit = list.query.data?.data.limit;
  const existing = items ? hasActiveDefaultKey(items, created?.key.id) : false;
  const failure = create.error instanceof ApiError ? create.error : null;

  return (
    <Card
      id="integration-credentials"
      tabIndex={-1}
      aria-labelledby="hub-credentials-title"
      className="hub-card"
    >
      <div className="card-heading">
        <h2 id="hub-credentials-title">Integration credentials</h2>
        <Badge>Sandbox</Badge>
      </div>
      <dl className="detail-list">
        <div>
          <dt>Base URL</dt>
          <dd>
            <CopyCode code={origin || "…"} label="Base URL" noun="URL" />
          </dd>
        </div>
        <div>
          <dt>Auth format</dt>
          <dd className="mono">Authorization: Bearer &lt;key&gt;</dd>
        </div>
        <div>
          <dt>Active keys</dt>
          <dd>
            {!isOperator
              ? role === null
                ? "Checking your role…"
                : "Key counts need the operator role."
              : items && limit !== undefined
                ? `${activeKeyCount(items)} of ${limit} active keys used`
                : "Loading key count…"}
          </dd>
        </div>
      </dl>
      <CopyCode
        code={curlExample("$KEY")}
        label="curl example (replace $KEY with your key)"
      />

      {role === "read" && (
        <p role="note">
          Generating keys requires the operator role. You are signed in as read
          only.
        </p>
      )}
      {isOperator && (
        <>
          <Button
            type="button"
            onClick={() => create.mutate({ ...defaultKeyInput })}
            disabled={create.isPending}
          >
            {create.isPending ? "Generating…" : "Generate default demo key"}
          </Button>
          <p className="muted small">
            One click, operator role, expires in 24 hours. Nothing is created
            until you click.
          </p>
          {existing && (
            <p className="small" role="note">
              You already have an active Default demo key; its secret cannot be
              shown again. Generating creates another.
            </p>
          )}
        </>
      )}
      {failure && (
        <div className="field-error" role="alert">
          <p>
            {failure.message}{" "}
            <span className="muted">
              Request ID: <code className="mono">{failure.requestId}</code>
            </span>
          </p>
        </div>
      )}
      <div ref={panelRef} tabIndex={-1}>
        <GeneratedKey created={created} onHide={() => create.reset()} />
      </div>
      <p className="muted small">
        Manage and revoke keys in{" "}
        <Link className="text-link" href="/dashboard/settings#api-keys">
          Settings
        </Link>
        .
      </p>
    </Card>
  );
}

export function DemoRunCard() {
  const scenarios = useScenarios();
  const items = scenarios.query.data?.data.items;
  const start = useStartSimulation();
  const [active, setActive] = useState<{
    scenario: HeroScenario;
    id: string;
  } | null>(null);
  const simulation = useSimulation(active?.id ?? null);
  const run = simulation.query.data?.data ?? null;
  const running =
    start.isPending || (active !== null && (!run || run.status === "running"));
  const settled = run && run.status !== "running" ? run : null;
  const summary = settled ? invariantSummary(settled) : null;

  const availability = (scenario: string) => {
    if (!items) return { ok: false, reason: "Checking available scenarios…" };
    const found = items.find((item) => item.id === scenario);
    if (!found) return { ok: false, reason: "backend scenario not available" };
    if (!found.implemented)
      return { ok: false, reason: "backend scenario not implemented yet" };
    return { ok: true, reason: "" };
  };

  const begin = (scenario: HeroScenario) => {
    setActive(null);
    start.mutate(
      { scenario, seed: freshSeed(), amount_minor: "10000", currency: "USD" },
      { onSuccess: (accepted) => setActive({ scenario, id: accepted.id }) },
    );
  };

  const announcement = settled
    ? `Run finished. ${summary?.text ?? ""}`
    : active || start.isPending
      ? "Demo run started."
      : "";
  const status = settled?.payment?.status;

  return (
    <Card aria-labelledby="hub-demo-title" className="hub-card">
      <div className="card-heading">
        <h2 id="hub-demo-title">Run a demo now</h2>
        <span className="badge">Simulated / sandbox</span>
      </div>
      <p className="muted">
        Each button starts a real simulation of a $100.00 USD sandbox payment
        and checks its invariants.
      </p>
      <ul className="hub-demo-buttons">
        {heroDemos.map(({ scenario, label }) => {
          const { ok, reason } = availability(scenario);
          return (
            <li key={scenario}>
              <Button
                type="button"
                className="button-secondary"
                onClick={() => begin(scenario)}
                disabled={!ok || running}
                aria-describedby={ok ? undefined : `hub-demo-${scenario}-why`}
              >
                {label}
              </Button>
              {!ok && (
                <span id={`hub-demo-${scenario}-why`} className="muted small">
                  Disabled: {reason}.
                </span>
              )}
            </li>
          );
        })}
      </ul>
      <div className="sr-only" aria-live="polite">
        {announcement}
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
        <p className="small" data-testid="hub-demo-live">
          {liveLabel(run)}
        </p>
      )}
      {settled && summary && (
        <div className="hub-result" data-testid="hub-demo-result">
          <p>
            {status && status in paymentStatusLabels ? (
              <StatusPill status={status as keyof typeof paymentStatusLabels} />
            ) : (
              <Badge>{settled.status}</Badge>
            )}
          </p>
          {settled.status === "failed" && settled.failure_reason && (
            <p className="small">Run failed: {settled.failure_reason}</p>
          )}
          <p className="small">{summary.text}</p>
          <Link className="text-link" href="/dashboard/playground">
            Open in Playground
          </Link>
        </div>
      )}
      {active && simulation.query.isError && !run && (
        <p className="field-error" role="alert">
          The run result could not be read.
        </p>
      )}
    </Card>
  );
}

export function ArchitectureStrip() {
  return (
    <section aria-labelledby="hub-arch-title" className="hub-section">
      <div className="section-title">
        <h2 id="hub-arch-title">How a payment flows</h2>
        <span className="text-label">Sandbox build</span>
      </div>
      <ol className="flow-list flow-strip">
        {flow.map(({ step, detail }, index) => (
          <li key={step}>
            <span>{index + 1}</span>
            <div>
              <strong>{step}</strong>
              <p className="text-label">{detail} · Available</p>
            </div>
          </li>
        ))}
      </ol>
      <p className="muted">Stripe and Plaid adapters: Planned</p>
    </section>
  );
}

export function SampleDataCard() {
  const { start, phase, rows, settledCount, total } = useSampleData();
  const running = phase === "running";
  const skipped = rows.filter((row) => row.outcome.state === "skipped");
  const latest = [...rows]
    .reverse()
    .find(
      (row) => row.outcome.state === "done" || row.outcome.state === "error",
    );
  const announcement =
    phase === "finished"
      ? `Sample data finished. ${settledCount} of ${total} scenarios done.`
      : phase === "stopped"
        ? "Sample data stopped after an error."
        : running && latest
          ? `${settledCount} of ${total} done: ${latest.scenario}. ${latest.outcome.state === "done" || latest.outcome.state === "error" ? latest.outcome.text : ""}`
          : "";
  return (
    <Card
      aria-labelledby="hub-sample-title"
      className="hub-card"
      data-testid="sample-data-card"
    >
      <div className="card-heading">
        <h2 id="hub-sample-title">Load sample data</h2>
        <Badge>Simulated / sandbox</Badge>
      </div>
      <p className="muted">
        Runs {sampleScenarios.length} real backend scenarios one after another
        (successes, declines, duplicates, an outage, a ledger replay, a
        duplicate webhook and a settlement mismatch) so every dashboard page has
        something to show. It takes about 20 seconds; two scenarios
        intentionally wait about 6 seconds. Running it again adds more sandbox
        payments.
      </p>
      <Button type="button" onClick={() => void start()} disabled={running}>
        {running ? "Loading sample data…" : "Load sample data"}
      </Button>
      <div className="sr-only" aria-live="polite">
        {announcement}
      </div>
      {phase !== "idle" && (
        <>
          <p className="small" data-testid="sample-data-progress">
            {settledCount} of {total} done
            {phase === "finished"
              ? "."
              : phase === "stopped"
                ? " (stopped)."
                : "…"}
          </p>
          <ol className="small" data-testid="sample-data-list">
            {rows.map((row) => (
              <li key={row.scenario}>
                <span className="mono">{row.scenario}</span>:{" "}
                {row.outcome.state === "pending"
                  ? "waiting"
                  : row.outcome.state === "running"
                    ? "running…"
                    : row.outcome.text}
              </li>
            ))}
          </ol>
          {skipped.length > 0 && (
            <p className="small" role="note">
              Skipped (not in the backend catalog):{" "}
              {skipped.map((row) => row.scenario).join(", ")}.
            </p>
          )}
          {phase === "finished" && (
            <p className="small">
              Done.{" "}
              <Link className="text-link" href="/dashboard/payments">
                View payments
              </Link>
            </p>
          )}
        </>
      )}
    </Card>
  );
}
