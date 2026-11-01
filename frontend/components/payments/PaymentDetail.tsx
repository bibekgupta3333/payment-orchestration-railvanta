"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Skeleton,
  StatusPill,
  Table,
  paymentStatusLabels,
} from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import { Tabs } from "@/components/ui/tabs";
import { ApiError } from "@/lib/api/client";
import type { Payment } from "@/lib/api/schemas";
import {
  isTerminal,
  pollIntervalMs,
  usePayment,
  usePaymentAttempts,
  usePaymentHistory,
  usePaymentJournal,
  usePaymentMethods,
} from "@/lib/hooks/payments";
import { formatMoney } from "@/lib/money";
import { formatTimestamp } from "./PaymentTable";

/** Explains an unknown outcome. Deliberately offers no retry or failover. */
export function UnknownOutcomeNotice() {
  return (
    <section className="unknown-notice" role="status">
      <h2>
        <span aria-hidden="true">! </span>Outcome unknown — checking status
      </h2>
      <p>
        The provider did not return a definitive result, so this payment may or
        may not have been executed. Railvanta will not retry it or send it to
        another provider, because that could charge twice. The outcome is
        resolved by reconciliation: the provider&apos;s status is queried and
        the payment moves to succeeded or failed only when evidence confirms it.
        This page keeps checking automatically.
      </p>
    </section>
  );
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      className="button-quiet button-small"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 2_000);
        } catch {
          setCopied(false);
        }
      }}
    >
      {copied ? "Copied" : label}
    </Button>
  );
}

function AttemptsTab({ id, payment }: { id: string; payment: Payment }) {
  const { query, workspace } = usePaymentAttempts(id, payment);
  return (
    <QueryState
      query={query}
      workspace={workspace}
      loadingLabel="Loading attempts"
    >
      {(result) =>
        result.data.items.length === 0 ? (
          <EmptyState title="No provider attempts yet">
            An attempt appears when the dispatcher routes this payment to a
            provider. Until then nothing has been sent to any provider.
          </EmptyState>
        ) : (
          <Table label="Provider attempts">
            <thead>
              <tr>
                <th scope="col">#</th>
                <th scope="col">Provider</th>
                <th scope="col">Status</th>
                <th scope="col">Routing reason</th>
                <th scope="col">Provider reference</th>
                <th scope="col">Error</th>
                <th scope="col">Submitted</th>
                <th scope="col">Completed</th>
              </tr>
            </thead>
            <tbody>
              {result.data.items.map((attempt) => (
                <tr key={attempt.id}>
                  <td>{attempt.attempt_number}</td>
                  <td className="mono">{attempt.provider_id}</td>
                  <td>
                    {attempt.status === "unknown"
                      ? "Outcome unknown"
                      : attempt.status}
                  </td>
                  <td>{attempt.routing_reason}</td>
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
        )
      }
    </QueryState>
  );
}

function EventsTab({ id, payment }: { id: string; payment: Payment }) {
  const { query, workspace } = usePaymentHistory(id, payment);
  return (
    <QueryState
      query={query}
      workspace={workspace}
      loadingLabel="Loading events"
    >
      {(result) =>
        result.data.events.length === 0 ? (
          <EmptyState title="No durable events recorded">
            Events written for this payment will appear here.
          </EmptyState>
        ) : (
          <Table label="Payment events">
            <thead>
              <tr>
                <th scope="col">Event</th>
                <th scope="col">Delivery status</th>
                <th scope="col">Delivery attempts</th>
                <th scope="col">Recorded</th>
                <th scope="col">Delivered</th>
              </tr>
            </thead>
            <tbody>
              {result.data.events.map((event) => (
                <tr key={event.id}>
                  <td className="mono">{event.event_type}</td>
                  <td>{event.status}</td>
                  <td>{event.attempts}</td>
                  <td className="nowrap">
                    {formatTimestamp(event.created_at)}
                  </td>
                  <td className="nowrap">
                    {event.delivered_at
                      ? formatTimestamp(event.delivered_at)
                      : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )
      }
    </QueryState>
  );
}

function JournalTab({ id }: { id: string }) {
  const query = usePaymentJournal(id);
  if (query.isError && !query.data) {
    const error = query.error;
    return (
      <ErrorState
        message={
          error instanceof ApiError ? error.message : "Unable to read journals."
        }
        requestId={error instanceof ApiError ? error.requestId : "unavailable"}
        retry={() => void query.refetch()}
      />
    );
  }
  if (!query.data) return <Skeleton label="Loading journal" />;
  const read = query.data.data;
  if (!read.available) {
    return (
      <EmptyState title="Journal appears once posting is available">
        Ledger journal reads are not available from this API yet (request ID{" "}
        {read.requestId}). No journal entries are shown rather than invented
        ones.
      </EmptyState>
    );
  }
  if (read.page.items.length === 0) {
    return (
      <EmptyState title="No journal posted for this payment">
        A balanced journal is posted only after the API confirms a final
        outcome.
      </EmptyState>
    );
  }
  return (
    <>
      {read.page.items.map((journal) => (
        <Table key={journal.id} label={`Journal ${journal.id}`}>
          <caption>
            Journal{" "}
            <Link
              className="text-link mono"
              href={`/dashboard/ledger/${journal.id}`}
            >
              {journal.id}
            </Link>{" "}
            · posted · immutable
          </caption>
          <thead>
            <tr>
              <th scope="col">Account</th>
              <th scope="col">Side</th>
              <th scope="col" className="numeric">
                Amount
              </th>
            </tr>
          </thead>
          <tbody>
            {journal.entries.map((entry, index) => (
              <tr key={`${entry.account_id}-${index}`}>
                <td className="mono">
                  {entry.account_code ?? entry.account_id}
                </td>
                <td>{entry.side}</td>
                <td className="numeric">
                  {formatMoney(entry.amount_minor, journal.currency)}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      ))}
    </>
  );
}

function SummaryTab({
  id,
  payment,
  rail,
  correlationId,
}: {
  id: string;
  payment: Payment;
  rail: string;
  correlationId: string | null;
}) {
  const { query, workspace } = usePaymentHistory(id, payment);
  return (
    <div className="detail-grid">
      <section className="card">
        <h2>Details</h2>
        <dl className="detail-list">
          <div>
            <dt>Amount</dt>
            <dd className="numeric">
              {formatMoney(payment.amount_minor, payment.currency)}
            </dd>
          </div>
          <div>
            <dt>Rail</dt>
            <dd>{rail}</dd>
          </div>
          <div>
            <dt>Payment method</dt>
            <dd className="mono">{payment.payment_method_id}</dd>
          </div>
          <div>
            <dt>Merchant reference</dt>
            <dd>{payment.merchant_reference ?? "—"}</dd>
          </div>
          <div>
            <dt>Created</dt>
            <dd>{formatTimestamp(payment.created_at)}</dd>
          </div>
          <div>
            <dt>Last updated</dt>
            <dd>{formatTimestamp(payment.updated_at)}</dd>
          </div>
          <div>
            <dt>Idempotency key</dt>
            <dd className="mono">
              {query.data?.data.idempotency_key_masked ?? "—"}{" "}
              <span className="text-label">(masked)</span>
            </dd>
          </div>
          <div>
            <dt>Correlation ID (latest read)</dt>
            <dd className="mono">{correlationId ?? "—"}</dd>
          </div>
        </dl>
      </section>
      <section className="card">
        <h2>State history</h2>
        <QueryState
          query={query}
          workspace={workspace}
          loadingLabel="Loading state history"
        >
          {(result) => (
            <ol className="timeline">
              {result.data.transitions.map((transition, index) => (
                <li key={`${transition.created_at}-${index}`}>
                  <StatusPill status={transition.to_status} />
                  <div>
                    <p>
                      {transition.from_status
                        ? `${paymentStatusLabels[transition.from_status]} → ${paymentStatusLabels[transition.to_status]}`
                        : paymentStatusLabels[transition.to_status]}
                      : {transition.reason}
                    </p>
                    <p className="text-label">
                      {formatTimestamp(transition.created_at)} ·{" "}
                      {transition.actor_reference}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
          )}
        </QueryState>
      </section>
    </div>
  );
}

export function PaymentDetail({ id }: { id: string }) {
  const searchParams = useSearchParams();
  const from = searchParams.get("from");
  const backHref = `/dashboard/payments${from ? `?${from}` : ""}`;
  const { query, workspace } = usePayment(id);
  const methods = usePaymentMethods();
  const notFound =
    query.error instanceof ApiError && query.error.status === 404;

  return (
    <>
      <Link className="text-link back-link" href={backHref}>
        <span aria-hidden="true">← </span>Back to payments
      </Link>
      {notFound ? (
        <>
          <div className="page-heading">
            <h1>Payment not found</h1>
          </div>
          <EmptyState title="No payment with this ID in this workspace">
            Check the ID, or return to the payment list. Request ID:{" "}
            {(query.error as ApiError).requestId}
          </EmptyState>
        </>
      ) : (
        <QueryState
          query={query}
          workspace={workspace}
          loadingLabel="Loading payment"
          lines={4}
        >
          {(result) => {
            const payment = result.data;
            const terminal = isTerminal(payment.status);
            const rail =
              methods.query.data?.data.items.find(
                (method) => method.id === payment.payment_method_id,
              )?.rail ?? "—";
            return (
              <>
                <div className="page-heading page-heading-actions">
                  <div>
                    <p className="eyebrow">Payment</p>
                    <h1 className="mono payment-title">{payment.id}</h1>
                    <p className="payment-meta">
                      <span className="numeric">
                        {formatMoney(payment.amount_minor, payment.currency)}
                      </span>{" "}
                      · {rail} · <Badge>Sandbox</Badge>
                    </p>
                  </div>
                  <div className="heading-actions">
                    <CopyButton value={payment.id} label="Copy ID" />
                    <StatusPill status={payment.status} />
                  </div>
                </div>
                {payment.status === "unknown" && <UnknownOutcomeNotice />}
                <section className="poll-status" aria-label="Status checks">
                  {terminal ? (
                    <p>
                      Final state confirmed by the API:{" "}
                      <strong>{paymentStatusLabels[payment.status]}</strong>.
                      Automatic checks have stopped.
                    </p>
                  ) : (
                    <p>
                      {payment.status === "unknown"
                        ? "Awaiting reconciliation evidence."
                        : "Pending — no final outcome yet."}{" "}
                      Checking status every {pollIntervalMs / 1000} seconds.
                    </p>
                  )}
                  <span className="text-label">
                    Last read {new Date(result.receivedAt).toLocaleTimeString()}
                  </span>
                  <Button
                    className="button-quiet button-small"
                    onClick={() => void query.refetch()}
                    disabled={query.isFetching}
                  >
                    {query.isFetching ? "Refreshing…" : "Refresh status"}
                  </Button>
                </section>
                <Tabs
                  label="Payment detail sections"
                  tabs={[
                    {
                      id: "summary",
                      label: "Summary",
                      content: (
                        <SummaryTab
                          id={id}
                          payment={payment}
                          rail={rail}
                          correlationId={result.correlationId}
                        />
                      ),
                    },
                    {
                      id: "attempts",
                      label: "Attempts",
                      content: <AttemptsTab id={id} payment={payment} />,
                    },
                    {
                      id: "events",
                      label: "Events",
                      content: <EventsTab id={id} payment={payment} />,
                    },
                    {
                      id: "journal",
                      label: "Journal",
                      content: <JournalTab id={id} />,
                    },
                  ]}
                />
              </>
            );
          }}
        </QueryState>
      )}
    </>
  );
}
