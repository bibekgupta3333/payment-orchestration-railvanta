"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId, useState, type FormEvent } from "react";
import { Button, Card, EmptyState, StatusPill, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import { Tabs, type TabDefinition } from "@/components/ui/tabs";
import { formatTimestamp } from "@/components/payments/PaymentTable";
import { ApiError } from "@/lib/api/client";
import type { Invariant, Simulation } from "@/lib/api/routing-schemas";
import {
  useScenarios,
  useSimulation,
  useStartSimulation,
} from "@/lib/hooks/routing";
import { formatMoney } from "@/lib/money";

export const simulatedLabel = "SIMULATED / SANDBOX ONLY";

const invariantLabels = { pending: "Pending", pass: "Pass", fail: "Fail" };
const invariantIcons = { pending: "○", pass: "✓", fail: "✕" };

export function InvariantStatus({ status }: { status: Invariant["status"] }) {
  return (
    <span className={`status-pill invariant-${status}`}>
      <span aria-hidden="true">{invariantIcons[status]}</span>{" "}
      {invariantLabels[status]}
    </span>
  );
}

function Json({ value }: { value: unknown }) {
  return (
    <pre className="json-block mono">{JSON.stringify(value, null, 2)}</pre>
  );
}

function RequestTab({ run }: { run: Simulation }) {
  const { request } = run;
  return (
    <dl className="detail-list">
      <div>
        <dt>Scenario</dt>
        <dd>{run.scenario}</dd>
      </div>
      <div>
        <dt>Seed</dt>
        <dd className="mono">{run.seed}</dd>
      </div>
      <div>
        <dt>Injected fault</dt>
        <dd>{run.injected_fault}</dd>
      </div>
      <div>
        <dt>Amount</dt>
        <dd className="numeric">
          {formatMoney(request.amount_minor, request.currency)} (
          {request.amount_minor} minor units)
        </dd>
      </div>
      <div>
        <dt>Payment method</dt>
        <dd className="mono">{request.payment_method_id}</dd>
      </div>
      <div>
        <dt>Idempotency key</dt>
        <dd className="mono">{request.idempotency_key}</dd>
      </div>
      <div>
        <dt>Merchant reference</dt>
        <dd className="mono">{request.merchant_reference}</dd>
      </div>
    </dl>
  );
}

function ResponseTab({ run }: { run: Simulation }) {
  if (!run.response) {
    return (
      <EmptyState title="No stored response yet">
        The acceptance response appears once the backend stored it.
      </EmptyState>
    );
  }
  return (
    <>
      <p>
        Acceptance response: HTTP {run.response.status_code}
        {run.payment && (
          <>
            {" "}
            · payment now <StatusPill status={run.payment.status} />
          </>
        )}
      </p>
      <Json value={run.response.body} />
    </>
  );
}

export function FailoverNotice({ run }: { run: Simulation }) {
  if (!run.routing.failover_blocked) return null;
  return (
    <section className="unknown-notice" role="status">
      <h3>
        <span aria-hidden="true">! </span>Unsafe failover blocked
      </h3>
      <p>{run.routing.failover_reason}</p>
    </section>
  );
}

function RoutingTab({ run }: { run: Simulation }) {
  return (
    <>
      <FailoverNotice run={run} />
      {run.routing.decisions.length === 0 ? (
        <EmptyState title="No routing decision yet">
          The dispatcher records a decision when it creates the first attempt.
        </EmptyState>
      ) : (
        <Table label="Routing decisions">
          <thead>
            <tr>
              <th scope="col">Attempt</th>
              <th scope="col">Provider</th>
              <th scope="col">Rule</th>
              <th scope="col">Reason</th>
            </tr>
          </thead>
          <tbody>
            {run.routing.decisions.map((decision) => (
              <tr key={decision.attempt_number}>
                <td>{decision.attempt_number}</td>
                <td className="mono">{decision.provider_id}</td>
                <td className="mono">{decision.rule_id ?? "—"}</td>
                <td>{decision.reason}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}

function AttemptsTab({ run }: { run: Simulation }) {
  if (run.attempts.length === 0)
    return (
      <EmptyState title="No provider attempts yet">
        Nothing has been sent to a provider.
      </EmptyState>
    );
  return (
    <Table label="Provider attempts">
      <thead>
        <tr>
          <th scope="col">#</th>
          <th scope="col">Status</th>
          <th scope="col">Provider reference</th>
          <th scope="col">Error</th>
          <th scope="col">Submitted</th>
          <th scope="col">Completed</th>
        </tr>
      </thead>
      <tbody>
        {run.attempts.map((attempt) => (
          <tr key={attempt.id}>
            <td>{attempt.attempt_number}</td>
            <td>
              {attempt.status === "unknown"
                ? "Outcome unknown"
                : attempt.status}
            </td>
            <td className="mono">{attempt.provider_reference ?? "—"}</td>
            <td>{attempt.error_code ?? "—"}</td>
            <td className="nowrap">
              {attempt.submitted_at
                ? formatTimestamp(attempt.submitted_at)
                : "—"}
            </td>
            <td className="nowrap">
              {attempt.completed_at
                ? formatTimestamp(attempt.completed_at)
                : "—"}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function EventsTab({ run }: { run: Simulation }) {
  if (run.events.length === 0 && run.webhooks.length === 0)
    return (
      <EmptyState title="No durable events yet">
        Outbox rows for this payment appear as the pipeline writes them.
      </EmptyState>
    );
  return (
    <>
      {run.webhooks.length > 0 ? (
        <Table label="Webhook inbox">
          <thead>
            <tr>
              <th scope="col">Provider event</th>
              <th scope="col">Type</th>
              <th scope="col">Outcome</th>
              <th scope="col">Repeat deliveries</th>
              <th scope="col">Signature</th>
              <th scope="col">Received</th>
            </tr>
          </thead>
          <tbody>
            {run.webhooks.map((hook) => (
              <tr key={hook.id}>
                <td className="mono">{hook.provider_event_id}</td>
                <td className="mono">{hook.event_type}</td>
                <td>
                  {hook.outcome ?? hook.status}
                  {hook.outcome_reason ? ` (${hook.outcome_reason})` : ""}
                </td>
                <td className="numeric">{hook.duplicate_count}</td>
                <td>{hook.signature_status}</td>
                <td className="nowrap">{formatTimestamp(hook.received_at)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : null}
      {run.events.length > 0 ? (
        <Table label="Durable events">
          <thead>
            <tr>
              <th scope="col">Event</th>
              <th scope="col">Status</th>
              <th scope="col">Delivery attempts</th>
              <th scope="col">Recorded</th>
              <th scope="col">Delivered</th>
            </tr>
          </thead>
          <tbody>
            {run.events.map((event) => (
              <tr key={event.id}>
                <td className="mono">{event.event_type}</td>
                <td>{event.status}</td>
                <td className="numeric">{event.attempts}</td>
                <td className="nowrap">{formatTimestamp(event.created_at)}</td>
                <td className="nowrap">
                  {event.delivered_at
                    ? formatTimestamp(event.delivered_at)
                    : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : null}
    </>
  );
}

function JournalTab({ run }: { run: Simulation }) {
  if (run.journals.length === 0)
    return (
      <EmptyState title="No journal posted">
        A journal exists only for a succeeded payment.
        {run.status === "running" ? " The run is still in progress." : ""}
      </EmptyState>
    );
  return (
    <>
      {run.journals.map((journal) => (
        <div key={journal.id}>
          <p className="small">
            Journal <span className="mono">{journal.id}</span> ·{" "}
            {journal.status} · key{" "}
            <span className="mono">{journal.posting_key}</span>
          </p>
          <Table label={`Entries of journal ${journal.id}`}>
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col">Side</th>
                <th scope="col">Amount</th>
              </tr>
            </thead>
            <tbody>
              {journal.entries.map((entry) => (
                <tr key={entry.id}>
                  <td className="mono">{entry.account_code}</td>
                  <td>{entry.side}</td>
                  <td className="numeric">
                    {formatMoney(entry.amount_minor, journal.currency)}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      ))}
    </>
  );
}

function InvariantsTab({ run }: { run: Simulation }) {
  const pending = run.invariants.every((item) => item.status === "pending");
  return (
    <>
      {pending && (
        <p className="muted">
          Invariants stay pending until the backend stores evidence for them.
        </p>
      )}
      <Table label="Invariant results">
        <thead>
          <tr>
            <th scope="col">Invariant</th>
            <th scope="col">Result</th>
            <th scope="col">Evidence</th>
          </tr>
        </thead>
        <tbody>
          {run.invariants.map((item) => (
            <tr key={item.id}>
              <td>{item.title}</td>
              <td>
                <InvariantStatus status={item.status} />
              </td>
              <td>{item.evidence}</td>
            </tr>
          ))}
        </tbody>
      </Table>
      {run.fingerprint && (
        <p className="muted small">
          End-state fingerprint:{" "}
          <span className="mono">{run.fingerprint.slice(0, 16)}</span>. Runs
          with the same scenario and seed produce the same fingerprint.
        </p>
      )}
    </>
  );
}

export function RunTimeline({ run }: { run: Simulation }) {
  return (
    <ol className="sim-timeline" aria-label="Execution timeline">
      {run.timeline.map((row, index) => (
        <li key={`${row.at}-${index}`}>
          <span className="nowrap mono">{formatTimestamp(row.at)}</span>
          <span className="badge">{row.source}</span>
          <span>
            <strong>{row.title}</strong>
            <span className="muted small"> — {row.detail}</span>
          </span>
        </li>
      ))}
    </ol>
  );
}

function RunPanel({
  run,
  refreshedAt,
  isFetching,
  refresh,
}: {
  run: Simulation;
  refreshedAt: number;
  isFetching: boolean;
  refresh: () => void;
}) {
  const tabs: TabDefinition[] = [
    { id: "request", label: "Request", content: <RequestTab run={run} /> },
    { id: "response", label: "Response", content: <ResponseTab run={run} /> },
    { id: "routing", label: "Routing", content: <RoutingTab run={run} /> },
    { id: "attempts", label: "Attempts", content: <AttemptsTab run={run} /> },
    { id: "events", label: "Events", content: <EventsTab run={run} /> },
    { id: "journal", label: "Journal", content: <JournalTab run={run} /> },
    {
      id: "invariants",
      label: "Invariants",
      content: <InvariantsTab run={run} />,
    },
  ];
  const statusText = {
    running: "Running: waiting for the backend pipeline",
    completed: "Completed",
    failed: "Run failed",
  }[run.status];
  return (
    <Card aria-labelledby="run-heading">
      <div className="card-heading">
        <h2 id="run-heading">Run {run.scenario}</h2>
        <span className="badge">{simulatedLabel}</span>
      </div>
      <div className="poll-status">
        <p>
          <strong data-testid="run-status">{statusText}</strong>
          {run.payment && (
            <>
              {" "}
              · payment <StatusPill status={run.payment.status} />
            </>
          )}
        </p>
        <p className="muted small">
          Last refreshed {new Date(refreshedAt).toLocaleTimeString()}
          {run.status === "running" && " · checks every second"}
        </p>
        <Button
          className="button-quiet button-small"
          onClick={refresh}
          disabled={isFetching}
        >
          {isFetching ? "Refreshing…" : "Refresh now"}
        </Button>
      </div>
      {run.failure_reason && (
        <p role="alert" className="field-error">
          {run.failure_reason}
        </p>
      )}
      <FailoverNotice run={run} />
      <h3>Execution timeline</h3>
      <RunTimeline run={run} />
      <h3>Inspector</h3>
      <Tabs label="Simulation inspector" tabs={tabs} />
    </Card>
  );
}

export function SimulatorView() {
  const id = useId();
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const runId = params.get("run");
  const scenarios = useScenarios();
  const start = useStartSimulation();
  const simulation = useSimulation(runId);
  const [scenario, setScenario] = useState("");
  const [seed, setSeed] = useState("demo");
  const [announced, setAnnounced] = useState(false);
  const items = scenarios.query.data?.data.items ?? [];
  const chosen = scenario || items.find((item) => item.implemented)?.id || "";
  const chosenItem = items.find((item) => item.id === chosen);

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!chosenItem?.implemented) return;
    setAnnounced(false);
    start.mutate(
      { scenario: chosen, seed: seed.trim() || "default" },
      {
        onSuccess: (run) => {
          setAnnounced(true);
          router.push(`${pathname}?run=${encodeURIComponent(run.id)}`);
        },
      },
    );
  };

  const startError = start.error;
  return (
    <>
      <div className="simulated-banner" role="note">
        <strong>{simulatedLabel}</strong>
        <span>
          Runs use the deterministic Mock provider. No real funds move and
          nothing here is production data.
        </span>
      </div>
      {/* Announced once when a run is accepted; polling never rewrites it. */}
      <p role="status" aria-live="polite" className="sr-only">
        {announced
          ? "Simulation run accepted. Results appear as the backend records them."
          : ""}
      </p>
      <Card aria-labelledby={`${id}-config`}>
        <h2 id={`${id}-config`}>Configure a run</h2>
        <QueryState
          query={scenarios.query}
          workspace={scenarios.workspace}
          loadingLabel="Loading scenarios"
        >
          {() => (
            <>
              <form onSubmit={onSubmit} className="filter-bar">
                <div className="field field-wide">
                  <label htmlFor={`${id}-scenario`}>Scenario</label>
                  <select
                    id={`${id}-scenario`}
                    value={chosen}
                    onChange={(event) => setScenario(event.target.value)}
                    aria-describedby={`${id}-scenario-help`}
                  >
                    {items.map((item) => (
                      <option
                        key={item.id}
                        value={item.id}
                        disabled={!item.implemented}
                      >
                        {item.label}
                        {item.implemented
                          ? ""
                          : ` (unavailable: waiting for ${item.missing_dependency})`}
                      </option>
                    ))}
                  </select>
                  <p id={`${id}-scenario-help`} className="field-hint">
                    {chosenItem?.description}
                  </p>
                </div>
                <div className="field">
                  <label htmlFor={`${id}-seed`}>Seed</label>
                  <input
                    id={`${id}-seed`}
                    value={seed}
                    onChange={(event) => setSeed(event.target.value)}
                    autoComplete="off"
                    pattern="[A-Za-z0-9_\-]{1,64}"
                    aria-describedby={`${id}-seed-help`}
                  />
                  <p id={`${id}-seed-help`} className="field-hint">
                    The same seed gives the same amount and the same results.
                  </p>
                </div>
                <div className="filter-actions">
                  <Button
                    type="submit"
                    disabled={start.isPending || !chosenItem?.implemented}
                  >
                    {start.isPending ? "Starting…" : "Run scenario"}
                  </Button>
                </div>
              </form>
              {startError && (
                <p role="alert" className="field-error">
                  {startError instanceof ApiError
                    ? `${startError.message} (request ${startError.requestId})`
                    : "The run could not be started."}
                </p>
              )}
              <h3>Scenario catalog</h3>
              <ul className="scenario-list">
                {items.map((item) => (
                  <li
                    key={item.id}
                    className={item.implemented ? "" : "scenario-disabled"}
                  >
                    <strong>{item.label}</strong>
                    {item.implemented ? (
                      <span className="muted small"> — {item.description}</span>
                    ) : (
                      <span className="muted small">
                        {" "}
                        — Unavailable: waiting for {item.missing_dependency}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            </>
          )}
        </QueryState>
      </Card>
      {runId ? (
        <QueryState
          query={simulation.query}
          workspace={simulation.workspace}
          loadingLabel="Loading simulation run"
          lines={4}
        >
          {(result) => (
            <RunPanel
              run={result.data}
              refreshedAt={simulation.query.dataUpdatedAt || result.receivedAt}
              isFetching={simulation.query.isFetching}
              refresh={() => void simulation.query.refetch()}
            />
          )}
        </QueryState>
      ) : (
        <EmptyState title="No run selected">
          Choose a scenario and run it. Results come from the backend; nothing
          is shown until the backend records it.
        </EmptyState>
      )}
    </>
  );
}
