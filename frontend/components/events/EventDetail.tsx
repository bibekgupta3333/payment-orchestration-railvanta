"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { formatTimestamp } from "@/components/payments/PaymentTable";
import { Badge, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import {
  signatureLabels,
  type EventDetail as Detail,
} from "@/lib/api/event-schemas";
import { useEvent } from "@/lib/hooks/events";
import { EventStatus } from "./EventExplorer";

/**
 * A trace link is offered only when a trace destination is configured at
 * build time (an https URL template containing {id}). Otherwise nothing is
 * rendered: there is no placeholder link to a tool that does not exist.
 */
export function traceUrl(
  id: string,
  template = process.env.NEXT_PUBLIC_TRACE_URL_TEMPLATE,
): string | null {
  if (
    !template ||
    !template.startsWith("https://") ||
    !template.includes("{id}")
  )
    return null;
  return template.replace("{id}", encodeURIComponent(id));
}

export function EventDetailView({ event }: { event: Detail }) {
  const trace = traceUrl(event.id);
  return (
    <>
      <section className="card">
        <h2>Summary</h2>
        <dl className="definition-grid">
          <dt>Event type</dt>
          <dd className="mono">{event.event_type}</dd>
          <dt>Source</dt>
          <dd>
            {event.source === "inbox" ? "Inbox (webhook)" : "Outbox (internal)"}{" "}
            <Badge>Simulated</Badge>
          </dd>
          <dt>Result</dt>
          <dd>
            <EventStatus event={event} />
          </dd>
          <dt>Provider event ID</dt>
          <dd className="mono">
            {event.provider_event_id ?? "Not applicable"}
          </dd>
          <dt>Signature</dt>
          <dd>Signature: {signatureLabels[event.signature_status]}</dd>
          <dt>Received</dt>
          <dd>{formatTimestamp(event.created_at)}</dd>
          <dt>Last received</dt>
          <dd>{formatTimestamp(event.last_received_at)}</dd>
          <dt>Processed</dt>
          <dd>
            {event.processed_at
              ? formatTimestamp(event.processed_at)
              : "Not yet"}
          </dd>
          {event.source === "inbox" ? (
            <>
              <dt>Duplicate deliveries</dt>
              <dd>{event.duplicate_count}</dd>
            </>
          ) : (
            <>
              <dt>Delivery attempts</dt>
              <dd>{event.delivery_attempts}</dd>
            </>
          )}
        </dl>
        {trace && (
          <p>
            <a className="text-link" href={trace} rel="noreferrer noopener">
              Open trace
            </a>
          </p>
        )}
      </section>

      {event.needs_investigation && (
        <section className="stale-notice" role="alert">
          <h2>
            <span aria-hidden="true">! </span>Needs investigation
          </h2>
          <p>
            This event ID was received again with different content{" "}
            {event.conflict_count} time{event.conflict_count === 1 ? "" : "s"}.
            The first delivery was kept unchanged and the later content was not
            applied.
          </p>
          <dl className="definition-grid">
            <dt>Stored payload hash</dt>
            <dd className="mono">{event.payload_hash}</dd>
            <dt>Conflicting hash</dt>
            <dd className="mono">{event.conflicting_payload_hash}</dd>
          </dl>
        </section>
      )}

      <section className="card">
        <h2>Payload summary (redacted)</h2>
        {event.payload_summary.fields.length === 0 ? (
          <p>No fields are safe to show.</p>
        ) : (
          <dl className="definition-grid">
            {event.payload_summary.fields.map((field) => (
              <div key={field.name} style={{ display: "contents" }}>
                <dt className="mono">{field.name}</dt>
                <dd className="mono">{field.value}</dd>
              </div>
            ))}
          </dl>
        )}
        <p className="text-label">
          {event.payload_summary.redacted_count} other field
          {event.payload_summary.redacted_count === 1 ? "" : "s"} redacted. The
          raw payload is never shown.
        </p>
        {event.payload_hash && (
          <p className="text-label">
            Payload hash: <span className="mono">{event.payload_hash}</span>
          </p>
        )}
      </section>

      <section className="card">
        <h2>Related payment</h2>
        {event.payment_id ? (
          <>
            <p>
              <Link
                className="text-link mono"
                href={`/dashboard/payments/${event.payment_id}`}
              >
                {event.payment_id}
              </Link>
              {event.payment_status && <> · status {event.payment_status}</>}
            </p>
            {event.attempts.length > 0 && (
              <Table label="Related attempts">
                <thead>
                  <tr>
                    <th scope="col">Attempt</th>
                    <th scope="col">Provider</th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {event.attempts.map((attempt) => (
                    <tr key={attempt.id}>
                      <td>#{attempt.attempt_number}</td>
                      <td className="mono">{attempt.provider_id}</td>
                      <td>{attempt.status}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </>
        ) : (
          <p>This event is not linked to a payment.</p>
        )}
      </section>
    </>
  );
}

export function EventDetailPage({ id }: { id: string }) {
  const searchParams = useSearchParams();
  const from = searchParams.get("from");
  const { query, workspace } = useEvent(id);
  return (
    <>
      <Link
        className="text-link back-link"
        href={`/dashboard/events${from ? `?${from}` : ""}`}
      >
        ← Back to events
      </Link>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>Event details</h1>
        <p className="mono">{id}</p>
      </div>
      <QueryState
        query={query}
        workspace={workspace}
        loadingLabel="Loading event"
        lines={5}
      >
        {(result) => <EventDetailView event={result.data} />}
      </QueryState>
    </>
  );
}
