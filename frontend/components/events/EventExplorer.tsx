"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId, type FormEvent } from "react";
import { formatTimestamp, shortId } from "@/components/payments/PaymentTable";
import { Badge, Button, EmptyState, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import { signatureLabels, type EventItem } from "@/lib/api/event-schemas";
import {
  eventFilterNames,
  useEvents,
  type EventFilters,
} from "@/lib/hooks/events";

const pageSize = "25";

export function eventFiltersFromParams(params: URLSearchParams): EventFilters {
  const filters: EventFilters = {};
  for (const name of eventFilterNames) {
    const value = params.get(name);
    if (value) filters[name] = value;
  }
  return filters;
}

const states = [
  ["pending", "Pending"],
  ["processing", "Processing"],
  ["processed", "Processed (inbox)"],
  ["delivered", "Delivered (outbox)"],
  ["failed", "Failed"],
  ["applied", "Applied a state change"],
  ["ignored", "Ignored (no state change)"],
  ["investigation", "Needs investigation"],
] as const;

export function outcomeLabel(event: EventItem): string {
  if (event.outcome === "ignored") return "Ignored";
  if (event.outcome === "applied") return "Applied";
  return event.status.charAt(0).toUpperCase() + event.status.slice(1);
}

export function EventStatus({ event }: { event: EventItem }) {
  return (
    <>
      {outcomeLabel(event)}
      {event.outcome_reason && (
        <span className="text-label">
          {" "}
          ({event.outcome_reason.replace(/_/g, " ")})
        </span>
      )}
      {event.needs_investigation && (
        <span className="flag-investigate"> ! Needs investigation</span>
      )}
    </>
  );
}

/** Events list. Filters and the cursor live only in the URL. */
export function EventExplorer() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const formId = useId();
  const filters = eventFiltersFromParams(new URLSearchParams(searchParams));
  const { query, workspace } = useEvents({ limit: pageSize, ...filters });
  const currentQuery = searchParams.toString();

  const navigate = (next: EventFilters) => {
    const params = new URLSearchParams();
    for (const name of eventFilterNames) {
      const value = next[name];
      if (value) params.set(name, value);
    }
    const text = params.toString();
    router.push(text ? `${pathname}?${text}` : pathname);
  };

  const applyFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const next: EventFilters = {};
    for (const name of eventFilterNames) {
      const value = data.get(name);
      if (typeof value === "string" && value.trim()) next[name] = value.trim();
    }
    delete next.cursor;
    navigate(next);
  };

  const detailQuery = currentQuery
    ? `?from=${encodeURIComponent(currentQuery)}`
    : "";
  const hasFilters = eventFilterNames.some(
    (name) => !["cursor", "limit"].includes(name) && filters[name],
  );

  return (
    <>
      <div className="page-heading page-heading-actions">
        <div>
          <p className="eyebrow">Payment operations</p>
          <h1>Events</h1>
          <p>
            Webhook inbox and outbox records for this workspace. Duplicate
            deliveries are counted, never re-applied, and payloads are redacted.
          </p>
        </div>
        <Button
          className="button-quiet"
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          {query.isFetching ? "Refreshing…" : "Refresh"}
        </Button>
      </div>

      <form
        className="filter-bar"
        aria-label="Event filters"
        onSubmit={applyFilters}
        key={currentQuery}
      >
        <div className="field field-wide">
          <label htmlFor={`${formId}-q`}>
            Payment, event or correlation ID
          </label>
          <input
            id={`${formId}-q`}
            name="q"
            type="search"
            defaultValue={filters.q ?? ""}
            autoComplete="off"
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-state`}>State</label>
          <select
            id={`${formId}-state`}
            name="state"
            defaultValue={filters.state ?? ""}
          >
            <option value="">Any state</option>
            {states.map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${formId}-source`}>Source</label>
          <select
            id={`${formId}-source`}
            name="source"
            defaultValue={filters.source ?? ""}
          >
            <option value="">Inbox and outbox</option>
            <option value="inbox">Inbox (webhooks)</option>
            <option value="outbox">Outbox (internal)</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${formId}-from`}>From date</label>
          <input
            id={`${formId}-from`}
            name="created_from"
            type="date"
            defaultValue={filters.created_from ?? ""}
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-to`}>To date</label>
          <input
            id={`${formId}-to`}
            name="created_to"
            type="date"
            defaultValue={filters.created_to ?? ""}
          />
        </div>
        <div className="filter-actions">
          <Button type="submit">Apply filters</Button>
          {hasFilters && (
            <Button
              type="button"
              className="button-quiet"
              onClick={() => navigate({})}
            >
              Clear filters
            </Button>
          )}
        </div>
      </form>

      <QueryState
        query={query}
        workspace={workspace}
        loadingLabel="Loading events"
        lines={4}
      >
        {(result) => {
          const items = result.data.items;
          return (
            <>
              <p className="event-meta text-label" role="status">
                <span>Source: simulated mock events (sandbox)</span>
                <span>
                  Updated {new Date(query.dataUpdatedAt).toLocaleTimeString()}
                </span>
                <Badge>Sandbox</Badge>
              </p>
              {items.length === 0 ? (
                <EmptyState title="No events match">
                  {hasFilters
                    ? "No inbox or outbox events match these filters."
                    : "Create a sandbox payment to see its events."}
                </EmptyState>
              ) : (
                <>
                  <div className="event-table-wrap">
                    <Table label="Events" className="event-table">
                      <thead>
                        <tr>
                          <th scope="col">Received</th>
                          <th scope="col">Event</th>
                          <th scope="col">Source</th>
                          <th scope="col">Result</th>
                          <th scope="col">Payment</th>
                          <th scope="col" className="numeric">
                            Duplicates
                          </th>
                          <th scope="col">Signature</th>
                        </tr>
                      </thead>
                      <tbody>
                        {items.map((event) => (
                          <tr key={event.id}>
                            <td className="nowrap">
                              {formatTimestamp(event.created_at)}
                            </td>
                            <td>
                              <Link
                                className="text-link mono"
                                href={`/dashboard/events/${event.id}${detailQuery}`}
                              >
                                {event.event_type}
                              </Link>
                              {event.provider_event_id && (
                                <div className="text-label mono">
                                  {event.provider_event_id}
                                </div>
                              )}
                            </td>
                            <td>{event.source}</td>
                            <td>
                              <EventStatus event={event} />
                            </td>
                            <td>
                              {event.payment_id ? (
                                <Link
                                  className="text-link mono"
                                  href={`/dashboard/payments/${event.payment_id}`}
                                >
                                  {shortId(event.payment_id)}
                                </Link>
                              ) : (
                                "None"
                              )}
                            </td>
                            <td className="numeric">
                              {event.source === "inbox"
                                ? event.duplicate_count
                                : "n/a"}
                            </td>
                            <td>{signatureLabels[event.signature_status]}</td>
                          </tr>
                        ))}
                      </tbody>
                    </Table>
                  </div>
                  <ul className="event-list-cards" aria-label="Events">
                    {items.map((event) => (
                      <li key={event.id} className="card">
                        <Link
                          className="text-link mono"
                          href={`/dashboard/events/${event.id}${detailQuery}`}
                        >
                          {event.event_type}
                        </Link>
                        <p className="text-label">
                          {formatTimestamp(event.created_at)} · {event.source}
                        </p>
                        <p>
                          <EventStatus event={event} />
                        </p>
                        <p className="text-label">
                          Duplicates:{" "}
                          {event.source === "inbox"
                            ? event.duplicate_count
                            : "n/a"}{" "}
                          · Signature: {signatureLabels[event.signature_status]}
                        </p>
                      </li>
                    ))}
                  </ul>
                </>
              )}
              <nav className="pager" aria-label="Event pages">
                {filters.cursor && (
                  <Button
                    className="button-quiet"
                    onClick={() => navigate({ ...filters, cursor: undefined })}
                  >
                    First page
                  </Button>
                )}
                {result.data.next_cursor && (
                  <Button
                    className="button-quiet"
                    onClick={() =>
                      navigate({
                        ...filters,
                        cursor: result.data.next_cursor ?? undefined,
                      })
                    }
                  >
                    Next page
                  </Button>
                )}
              </nav>
            </>
          );
        }}
      </QueryState>
    </>
  );
}
