"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId, useRef, useState, type FormEvent } from "react";
import { Badge, Button, Card, EmptyState, Table } from "@/components/ui";
import { Dialog } from "@/components/ui/dialog";
import { QueryState } from "@/components/ui/query-state";
import { formatTimestamp } from "@/components/payments/PaymentTable";
import { ApiError } from "@/lib/api/client";
import {
  discrepancyKindSchema,
  discrepancyStatusSchema,
  type Discrepancy,
  type DiscrepancyStatus,
  type ReconciliationRun,
} from "@/lib/api/ledger-schemas";
import { useSession } from "@/lib/hooks/ledger";
import {
  defaultWindow,
  newRunKey,
  useDiscrepancies,
  useReconciliationRuns,
  useResolveDiscrepancy,
  useStartReconciliation,
} from "@/lib/hooks/reconciliation";
import { shortId } from "@/lib/ledger-format";
import { formatAggregateMoney } from "@/lib/money";
import "@/components/ledger/ledger.css";

export const kindLabels = {
  missing_posting: "Missing posting",
  unknown_outcome: "Unknown outcome",
  amount_mismatch: "Amount mismatch",
  status_mismatch: "Status mismatch",
  missing_at_provider: "Missing at provider",
} as const;

function money(amount: string, currency: string): string {
  try {
    return formatAggregateMoney(amount, currency);
  } catch {
    return `${currency} ${amount}`;
  }
}

function Simulated() {
  return <span className="simulated-tag">Simulated</span>;
}

/**
 * Reconciliation: run history, counts, discrepancies and a detail pane
 * (evidence, then resolution history, then related journal). All evidence
 * comes from the mock provider and is labelled Simulated. Resolution records a
 * note; it never changes a payment or a journal.
 */
export function ReconciliationView() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const formId = useId();
  const session = useSession();
  const role = session.query.data?.data.role;
  const runs = useReconciliationRuns();
  const runList = runs.query.data?.data.items ?? [];
  const selectedRunId = searchParams.get("run") || runList[0]?.id || "";
  const selectedRun = runList.find((run) => run.id === selectedRunId);
  const kind = searchParams.get("kind") ?? "";
  const status = searchParams.get("status") ?? "";
  const cursor = searchParams.get("cursor") ?? "";
  const selectedId = searchParams.get("d") ?? "";
  const discrepancies = useDiscrepancies(
    { run_id: selectedRunId, kind, status, cursor },
    Boolean(selectedRunId),
  );

  const go = (next: Record<string, string>) => {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(next)) {
      if (value) params.set(key, value);
    }
    const text = params.toString();
    router.push(text ? `${pathname}?${text}` : pathname);
  };
  const current = {
    run: selectedRunId,
    kind,
    status,
    cursor,
    d: selectedId,
  };

  const applyFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    go({
      run: selectedRunId,
      kind: String(data.get("kind") ?? ""),
      status: String(data.get("status") ?? ""),
    });
  };

  return (
    <>
      <div className="page-heading page-heading-actions">
        <div>
          <p className="eyebrow">Reconciliation</p>
          <h1>Reconciliation</h1>
          <p>
            Compares mock-provider charges, payment state and posted journals.
            Evidence is <Simulated /> sandbox data. A run never creates a
            journal or changes a payment; resolving a discrepancy only records a
            note.
          </p>
        </div>
        <RunControl role={role} sessionPending={session.query.isPending} />
      </div>

      <QueryState
        query={runs.query}
        workspace={runs.workspace}
        loadingLabel="Loading reconciliation runs"
      >
        {() =>
          runList.length === 0 ? (
            <EmptyState title="No reconciliation runs yet">
              Run a sandbox reconciliation to compare provider evidence with
              payment and ledger records.
            </EmptyState>
          ) : (
            <>
              <div className="field">
                <label htmlFor={`${formId}-run`}>Run history</label>
                <select
                  id={`${formId}-run`}
                  value={selectedRunId}
                  onChange={(event) => go({ run: event.target.value })}
                >
                  {runList.map((run) => (
                    <option key={run.id} value={run.id}>
                      {formatTimestamp(run.created_at)} · {run.counts.compared}{" "}
                      compared · {run.counts.unmatched + run.counts.mismatch}{" "}
                      discrepancies
                    </option>
                  ))}
                </select>
              </div>
              {selectedRun && <RunSummary run={selectedRun} />}

              <form
                className="filter-bar"
                aria-label="Discrepancy filters"
                onSubmit={applyFilters}
                key={searchParams.toString()}
              >
                <div className="field">
                  <label htmlFor={`${formId}-kind`}>Kind</label>
                  <select id={`${formId}-kind`} name="kind" defaultValue={kind}>
                    <option value="">Any kind</option>
                    {discrepancyKindSchema.options.map((option) => (
                      <option key={option} value={option}>
                        {kindLabels[option]}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="field">
                  <label htmlFor={`${formId}-status`}>Status</label>
                  <select
                    id={`${formId}-status`}
                    name="status"
                    defaultValue={status}
                  >
                    <option value="">Any status</option>
                    {discrepancyStatusSchema.options.map((option) => (
                      <option key={option} value={option}>
                        {option}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="filter-actions">
                  <Button type="submit">Apply filters</Button>
                  {(kind || status) && (
                    <Link
                      className="button button-quiet"
                      href={`${pathname}?run=${selectedRunId}`}
                    >
                      Clear filters
                    </Link>
                  )}
                </div>
              </form>

              <QueryState
                query={discrepancies.query}
                workspace={discrepancies.workspace}
                loadingLabel="Loading discrepancies"
              >
                {(result) => {
                  const page = result.data;
                  const selected = page.items.find(
                    (item) => item.id === selectedId,
                  );
                  if (page.items.length === 0) {
                    return (
                      <EmptyState
                        title={
                          kind || status
                            ? "No discrepancies match these filters"
                            : "No discrepancies in this run"
                        }
                      >
                        {kind || status
                          ? "Clear the filters to see every discrepancy for this run."
                          : "Every compared payment matched its provider charge and ledger posting."}
                      </EmptyState>
                    );
                  }
                  return (
                    <div className="recon-layout">
                      <div>
                        <Table label="Discrepancies" className="stack-table">
                          <thead>
                            <tr>
                              <th scope="col">Kind</th>
                              <th scope="col">Payment</th>
                              <th scope="col">Status</th>
                              <th scope="col">Found</th>
                              <th scope="col">
                                <span className="sr-only">Actions</span>
                              </th>
                            </tr>
                          </thead>
                          <tbody>
                            {page.items.map((item) => (
                              <tr
                                key={item.id}
                                className={
                                  item.id === selectedId
                                    ? "recon-row-selected"
                                    : undefined
                                }
                              >
                                <td data-label="Kind">
                                  {kindLabels[item.kind]}
                                </td>
                                <td data-label="Payment" className="mono">
                                  {item.payment_id
                                    ? shortId(item.payment_id)
                                    : "—"}
                                </td>
                                <td data-label="Status">
                                  <Badge>{item.status}</Badge>
                                </td>
                                <td data-label="Found" className="nowrap">
                                  {formatTimestamp(item.created_at)}
                                </td>
                                <td data-label="Details">
                                  <Button
                                    className="button-quiet button-small"
                                    aria-pressed={item.id === selectedId}
                                    onClick={() =>
                                      go({ ...current, d: item.id })
                                    }
                                  >
                                    View evidence
                                    <span className="sr-only">
                                      {" "}
                                      for {kindLabels[item.kind]} on payment{" "}
                                      {item.payment_id}
                                    </span>
                                  </Button>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </Table>
                        <nav
                          className="pagination"
                          aria-label="Discrepancy pages"
                        >
                          {cursor && (
                            <Button
                              className="button-quiet"
                              onClick={() => go({ ...current, cursor: "" })}
                            >
                              First page
                            </Button>
                          )}
                          <span className="text-label">
                            Updated{" "}
                            {new Date(result.receivedAt).toLocaleTimeString()}
                          </span>
                          {page.next_cursor && (
                            <Button
                              className="button-quiet"
                              onClick={() =>
                                go({
                                  ...current,
                                  cursor: page.next_cursor ?? "",
                                })
                              }
                            >
                              Next page
                            </Button>
                          )}
                        </nav>
                      </div>
                      <aside aria-label="Discrepancy detail">
                        {selected ? (
                          <DiscrepancyDetail
                            item={selected}
                            canResolve={role === "operator"}
                          />
                        ) : (
                          <Card>
                            <p className="muted">
                              Select a discrepancy to see its evidence,
                              resolution history and related journal.
                            </p>
                          </Card>
                        )}
                      </aside>
                    </div>
                  );
                }}
              </QueryState>
            </>
          )
        }
      </QueryState>
    </>
  );
}

function RunSummary({ run }: { run: ReconciliationRun }) {
  const cards: [string, number, string][] = [
    ["Compared", run.counts.compared, "Settled mock-provider payments"],
    ["Matched", run.counts.matched, "Provider, payment and journal agree"],
    [
      "Unmatched",
      run.counts.unmatched,
      "Missing posting, provider record or unresolved outcome",
    ],
    ["Mismatch", run.counts.mismatch, "Amount or status disagree"],
  ];
  return (
    <section aria-label="Run summary">
      <p className="text-label">
        Window {formatTimestamp(run.window_start)} to{" "}
        {formatTimestamp(run.window_end)} · {run.status} ·{" "}
        {run.evidence_consumed_count} late outcome
        {run.evidence_consumed_count === 1 ? "" : "s"} consumed · <Simulated />
      </p>
      <div className="count-grid">
        {cards.map(([label, value, hint]) => (
          <Card key={label} className="metric-card">
            <p className="eyebrow">{label}</p>
            <p className="metric-value numeric" data-testid={`count-${label}`}>
              {value}
            </p>
            <p className="text-label">{hint}</p>
          </Card>
        ))}
      </div>
    </section>
  );
}

function RunControl({
  role,
  sessionPending,
}: {
  role: "read" | "operator" | undefined;
  sessionPending: boolean;
}) {
  const [open, setOpen] = useState(false);
  const runKey = useRef<string | null>(null);
  const windowRef = useRef<ReturnType<typeof defaultWindow> | null>(null);
  const start = useStartReconciliation();
  const reasonId = useId();
  const allowed = role === "operator";
  const reason = sessionPending
    ? "Checking your permission."
    : role === undefined
      ? "Your permission could not be determined."
      : role !== "operator"
        ? "Operator permission is required to run reconciliation."
        : null;

  const confirm = async () => {
    // The key and window are kept until the API answers, so retrying an
    // unconfirmed request replays the same run instead of creating another.
    runKey.current ??= newRunKey();
    windowRef.current ??= defaultWindow();
    try {
      await start.mutateAsync({
        runKey: runKey.current,
        window: windowRef.current,
      });
      runKey.current = null;
      windowRef.current = null;
      setOpen(false);
    } catch (error) {
      if (
        error instanceof ApiError &&
        error.status >= 400 &&
        error.status < 500
      ) {
        runKey.current = null;
        windowRef.current = null;
      }
    }
  };

  const result = start.data;
  return (
    <div className="heading-actions">
      <Button
        onClick={() => setOpen(true)}
        disabled={!allowed || start.isPending}
        aria-describedby={reason ? reasonId : undefined}
      >
        Run sandbox reconciliation
      </Button>
      {reason && (
        <p id={reasonId} className="text-label">
          {reason}
        </p>
      )}
      <div role="status" aria-live="polite" className="text-label">
        {start.isPending && "Reconciliation run in progress…"}
        {result &&
          !start.isPending &&
          result.status === 200 &&
          "This run already existed; showing its stored result. No duplicate was created."}
        {result &&
          !start.isPending &&
          result.status === 201 &&
          `Run ${shortId(result.data.id)} ${result.data.status}.`}
      </div>
      <Dialog
        open={open}
        title="Run sandbox reconciliation"
        onClose={() => !start.isPending && setOpen(false)}
        className="create-payment-dialog"
      >
        <p>
          This compares the last 24 hours of mock-provider charges, payment
          state and posted journals. It is read-only: it creates no journals,
          changes no payments and moves no funds. Pending late outcomes for
          those payments are marked consumed as evidence.
        </p>
        {start.isError && (
          <p role="alert" className="field-error">
            {start.error instanceof ApiError
              ? `${start.error.message} (request ${start.error.requestId}).`
              : "The run could not be confirmed."}
            {start.error instanceof ApiError && start.error.transportUncertain
              ? " Retrying reuses the same run key, so it cannot create a second run."
              : ""}
          </p>
        )}
        <div className="dialog-actions">
          <Button
            className="button-quiet"
            onClick={() => setOpen(false)}
            disabled={start.isPending}
          >
            Cancel
          </Button>
          <Button onClick={() => void confirm()} disabled={start.isPending}>
            {start.isPending ? "Running…" : "Run reconciliation"}
          </Button>
        </div>
      </Dialog>
    </div>
  );
}

function DiscrepancyDetail({
  item,
  canResolve,
}: {
  item: Discrepancy;
  canResolve: boolean;
}) {
  const details = item.details;
  const charge = details.provider_charge;
  const journal = details.journal;
  return (
    <div className="recon-detail">
      <Card>
        <h2>{kindLabels[item.kind]}</h2>
        <p>
          <Badge>{item.status}</Badge> <Simulated />
        </p>
        <h3>Evidence</h3>
        <p>{details.summary}</p>
        <dl className="detail-list">
          {details.expected && (
            <>
              <dt>Expected</dt>
              <dd>{details.expected}</dd>
            </>
          )}
          <dt>Payment</dt>
          <dd>
            {item.payment_id ? (
              <Link
                className="text-link mono"
                href={`/dashboard/payments/${item.payment_id}`}
              >
                {shortId(item.payment_id)}
              </Link>
            ) : (
              "—"
            )}{" "}
            {details.payment.status} ·{" "}
            <span className="numeric">
              {money(details.payment.amount_minor, details.payment.currency)}
            </span>
          </dd>
          <dt>Provider charge</dt>
          <dd>
            {charge ? (
              <>
                {charge.status} ·{" "}
                <span className="numeric">
                  {money(charge.amount_minor, charge.currency)}
                </span>{" "}
                · <span className="mono">{charge.provider_reference}</span> (
                {charge.scenario})
              </>
            ) : (
              "No charge recorded by the mock provider"
            )}
          </dd>
          <dt>Journal</dt>
          <dd>
            {journal ? (
              <span className="numeric">
                debits{" "}
                {money(journal.debit_total_minor, details.payment.currency)} ·
                credits{" "}
                {money(journal.credit_total_minor, details.payment.currency)}
              </span>
            ) : (
              "No posted success journal"
            )}
          </dd>
        </dl>
        {details.late_outcomes.length > 0 && (
          <>
            <h3>Late outcomes consumed as evidence</h3>
            <ul>
              {details.late_outcomes.map((late) => (
                <li key={late.event_id}>
                  Provider reported <strong>{late.outcome}</strong> via{" "}
                  {late.source} after the payment was {late.payment_status}.
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>

      <Card>
        <h3>Resolution history</h3>
        {item.history.length === 0 ? (
          <p className="muted">
            No status changes yet. History is append-only.
          </p>
        ) : (
          <ol className="history-list">
            {item.history.map((event) => (
              <li key={event.id}>
                <strong>
                  {event.from_status} → {event.to_status}
                </strong>{" "}
                by <span className="mono">{event.actor}</span> at{" "}
                {formatTimestamp(event.created_at)}
                <br />
                {event.note}
              </li>
            ))}
          </ol>
        )}
        {item.status === "resolved" && item.resolved_at && (
          <p className="text-label">
            Resolved {formatTimestamp(item.resolved_at)} by {item.resolved_by}.
            Resolution recorded a note only; the payment and ledger are
            unchanged.
          </p>
        )}
        <ResolveForm item={item} canResolve={canResolve} />
      </Card>

      <Card>
        <h3>Related journal</h3>
        {item.related_journal_id ? (
          <Link
            className="text-link mono"
            href={`/dashboard/ledger/${item.related_journal_id}`}
          >
            {item.related_journal_id}
          </Link>
        ) : (
          <p className="muted">
            This payment has no posted success journal. Reconciliation does not
            create one.
          </p>
        )}
      </Card>
    </div>
  );
}

function ResolveForm({
  item,
  canResolve,
}: {
  item: Discrepancy;
  canResolve: boolean;
}) {
  const formId = useId();
  const resolve = useResolveDiscrepancy();
  const [note, setNote] = useState("");
  if (item.status === "resolved") return null;
  if (!canResolve) {
    return (
      <p className="text-label">
        Operator permission is required to change a discrepancy status.
      </p>
    );
  }
  const options = discrepancyStatusSchema.options.filter(
    (option) => option !== item.status,
  );
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const status = String(data.get("status")) as DiscrepancyStatus;
    if (!note.trim()) return;
    try {
      await resolve.mutateAsync({ id: item.id, status, note: note.trim() });
      setNote("");
    } catch {
      /* shown below */
    }
  };
  return (
    <form className="resolve-form" onSubmit={submit} aria-label="Change status">
      <div className="field">
        <label htmlFor={`${formId}-status`}>New status</label>
        <select id={`${formId}-status`} name="status" defaultValue="resolved">
          {options.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor={`${formId}-note`}>Note (required)</label>
        <textarea
          id={`${formId}-note`}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          maxLength={2000}
          required
        />
        <p className="field-hint">
          Recorded with your identity and a timestamp. It does not change the
          payment or ledger.
        </p>
      </div>
      {resolve.isError && (
        <p role="alert" className="field-error">
          {resolve.error instanceof ApiError
            ? `${resolve.error.message} (request ${resolve.error.requestId})`
            : "The change could not be confirmed."}
        </p>
      )}
      <Button type="submit" disabled={resolve.isPending || !note.trim()}>
        {resolve.isPending ? "Saving…" : "Record status change"}
      </Button>
    </form>
  );
}
