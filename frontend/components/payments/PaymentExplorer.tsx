"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId, useState, type FormEvent } from "react";
import { Button, EmptyState } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import { paymentStatusSchema } from "@/lib/api/schemas";
import {
  filterNames,
  usePaymentMethods,
  usePayments,
  type PaymentFilters,
} from "@/lib/hooks/payments";
import {
  usePendingSubmission,
  type PendingSubmission,
} from "@/lib/hooks/useCreatePayment";
import { currencies, formatMoney } from "@/lib/money";
import { CreatePaymentDialog, rails } from "./CreatePaymentDialog";
import { PaymentTable } from "./PaymentTable";

const pageSize = "20";

export function filtersFromParams(params: URLSearchParams): PaymentFilters {
  const filters: PaymentFilters = {};
  for (const name of filterNames) {
    const value = params.get(name);
    if (value) filters[name] = value;
  }
  return filters;
}

function activeFilterCount(filters: PaymentFilters): number {
  return filterNames.filter(
    (name) => !["sort", "cursor", "limit"].includes(name) && filters[name],
  ).length;
}

/**
 * Payment list. Search, filters, sort and cursor live only in the URL, so a
 * refresh or back navigation restores exactly the same view.
 */
export function PaymentExplorer() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const formId = useId();
  const filters = filtersFromParams(new URLSearchParams(searchParams));
  const { query, workspace } = usePayments({ limit: pageSize, ...filters });
  const methods = usePaymentMethods();
  const [dialogOpen, setDialogOpen] = useState(false);
  const pending = usePendingSubmission();
  const [resume, setResume] = useState<PendingSubmission | null>(null);

  const navigate = (next: PaymentFilters) => {
    const params = new URLSearchParams();
    for (const name of filterNames) {
      const value = next[name];
      if (value) params.set(name, value);
    }
    const text = params.toString();
    router.push(text ? `${pathname}?${text}` : pathname);
  };

  const applyFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const next: PaymentFilters = {};
    for (const name of filterNames) {
      const value = data.get(name);
      if (typeof value === "string" && value.trim()) next[name] = value.trim();
    }
    delete next.cursor; // a new filter set starts at the first page
    navigate(next);
  };

  const currentQuery = searchParams.toString();
  const detailQuery = currentQuery
    ? `?from=${encodeURIComponent(currentQuery)}`
    : "";
  const filterCount = activeFilterCount(filters);
  const methodList = methods.query.data?.data.items ?? [];

  return (
    <>
      <div className="page-heading page-heading-actions">
        <div>
          <p className="eyebrow">Payment operations</p>
          <h1>Payments</h1>
          <p>
            Logical sandbox payments for this workspace. Statuses come from the
            API; nothing here is shown as succeeded before the API confirms it.
          </p>
        </div>
        <Button
          onClick={() => {
            setResume(null);
            setDialogOpen(true);
          }}
          disabled={methods.query.isPending}
        >
          Create sandbox payment
        </Button>
      </div>

      {pending && !dialogOpen && (
        <section className="stale-notice" role="alert">
          <p>
            <strong>
              A payment submission from this tab was not confirmed.
            </strong>{" "}
            {formatMoney(pending.intent.amount_minor, pending.intent.currency)}{" "}
            started {new Date(pending.startedAt).toLocaleTimeString()}. Its
            outcome is unknown until the API answers; resending the unchanged
            request reuses its idempotency key and cannot create a second
            payment.
          </p>
          <Button
            className="button-quiet button-small"
            onClick={() => {
              setResume(pending);
              setDialogOpen(true);
            }}
          >
            Review unconfirmed submission
          </Button>
        </section>
      )}

      <form
        className="filter-bar"
        aria-label="Payment filters"
        onSubmit={applyFilters}
        key={currentQuery}
      >
        <div className="field field-wide">
          <label htmlFor={`${formId}-q`}>Search payment ID or reference</label>
          <input
            id={`${formId}-q`}
            name="q"
            type="search"
            defaultValue={filters.q ?? ""}
            autoComplete="off"
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-status`}>Status</label>
          <select
            id={`${formId}-status`}
            name="status"
            defaultValue={filters.status ?? ""}
          >
            <option value="">Any status</option>
            {paymentStatusSchema.options.map((status) => (
              <option key={status} value={status}>
                {status === "unknown" ? "Outcome unknown" : status}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${formId}-rail`}>Rail</label>
          <select
            id={`${formId}-rail`}
            name="rail"
            defaultValue={filters.rail ?? ""}
          >
            <option value="">Any rail</option>
            {rails.map((rail) => (
              <option key={rail} value={rail}>
                {rail}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${formId}-provider`}>Provider ID</label>
          <input
            id={`${formId}-provider`}
            name="provider_id"
            defaultValue={filters.provider_id ?? ""}
            autoComplete="off"
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-currency`}>Currency</label>
          <select
            id={`${formId}-currency`}
            name="currency"
            defaultValue={filters.currency ?? ""}
          >
            <option value="">Any currency</option>
            {currencies.map((currency) => (
              <option key={currency} value={currency}>
                {currency}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${formId}-from`}>Created from</label>
          <input
            id={`${formId}-from`}
            name="created_from"
            type="date"
            defaultValue={filters.created_from ?? ""}
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-to`}>Created to</label>
          <input
            id={`${formId}-to`}
            name="created_to"
            type="date"
            defaultValue={filters.created_to ?? ""}
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-min`}>Min amount (minor units)</label>
          <input
            id={`${formId}-min`}
            name="amount_min"
            inputMode="numeric"
            pattern="[1-9][0-9]*"
            defaultValue={filters.amount_min ?? ""}
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-max`}>Max amount (minor units)</label>
          <input
            id={`${formId}-max`}
            name="amount_max"
            inputMode="numeric"
            pattern="[1-9][0-9]*"
            defaultValue={filters.amount_max ?? ""}
          />
        </div>
        <div className="field">
          <label htmlFor={`${formId}-sort`}>Sort</label>
          <select
            id={`${formId}-sort`}
            name="sort"
            defaultValue={filters.sort ?? "created_desc"}
          >
            <option value="created_desc">Newest first</option>
            <option value="created_asc">Oldest first</option>
          </select>
        </div>
        {filters.limit && (
          <input type="hidden" name="limit" value={filters.limit} />
        )}
        <div className="filter-actions">
          <Button type="submit">Apply filters</Button>
          {(filterCount > 0 || filters.sort || filters.cursor) && (
            <Link className="button button-quiet" href={pathname}>
              Clear filters
            </Link>
          )}
          <span className="text-label" aria-live="polite">
            {filterCount} active filter{filterCount === 1 ? "" : "s"}
          </span>
        </div>
      </form>

      <QueryState
        query={query}
        workspace={workspace}
        loadingLabel="Loading payments"
      >
        {(result) => {
          const page = result.data;
          if (page.items.length === 0) {
            return filterCount > 0 || filters.cursor ? (
              <EmptyState
                title="No payments match these filters"
                action={
                  <Link className="button button-quiet" href={pathname}>
                    Clear filters
                  </Link>
                }
              >
                Nothing in this workspace matches the current search and
                filters.
              </EmptyState>
            ) : (
              <EmptyState title="No payments yet">
                Create a sandbox payment to see it here. Amounts are simulated
                and no real funds move.
              </EmptyState>
            );
          }
          return (
            <>
              <PaymentTable
                payments={page.items}
                methods={methodList}
                detailQuery={detailQuery}
              />
              <nav className="pagination" aria-label="Payment pages">
                {filters.cursor && (
                  <Button
                    className="button-quiet"
                    onClick={() => navigate({ ...filters, cursor: undefined })}
                  >
                    First page
                  </Button>
                )}
                <span className="text-label">
                  Updated {new Date(result.receivedAt).toLocaleTimeString()}
                </span>
                {page.next_cursor && (
                  <Button
                    className="button-quiet"
                    onClick={() =>
                      navigate({
                        ...filters,
                        cursor: page.next_cursor ?? undefined,
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

      <CreatePaymentDialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        methods={methodList}
        resume={resume?.intent ?? null}
      />
    </>
  );
}
