"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { EmptyState, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import { ApiError } from "@/lib/api/client";
import type { LedgerJournalDetail } from "@/lib/api/ledger-schemas";
import { useLedgerJournal } from "@/lib/hooks/ledger";
import { totalsFromEntries } from "@/lib/ledger-format";
import { formatAggregateMoney } from "@/lib/money";
import { formatTimestamp } from "@/components/payments/PaymentTable";
import "./ledger.css";

/**
 * Journal detail. Everything here is read-only: a posted journal and its
 * entries are immutable, so no edit, delete or repost control exists. The
 * "debits equal credits" check is computed from the returned lines.
 */
export function JournalDetail({ id }: { id: string }) {
  const searchParams = useSearchParams();
  const from = searchParams.get("from");
  const backHref = `/dashboard/ledger${from ? `?${from}` : ""}`;
  const { query, workspace } = useLedgerJournal(id);
  const notFound =
    query.error instanceof ApiError && query.error.status === 404;
  return (
    <>
      <Link className="text-link back-link" href={backHref}>
        <span aria-hidden="true">← </span>Back to ledger
      </Link>
      {notFound ? (
        <>
          <div className="page-heading">
            <h1>Journal not found</h1>
          </div>
          <EmptyState title="No posted journal with this ID in this workspace">
            Check the ID, or return to the ledger. Request ID:{" "}
            {(query.error as ApiError).requestId}
          </EmptyState>
        </>
      ) : (
        <QueryState
          query={query}
          workspace={workspace}
          loadingLabel="Loading journal"
          lines={4}
        >
          {(result) => <JournalBody journal={result.data} from={from} />}
        </QueryState>
      )}
    </>
  );
}

function reversalHref(id: string, from: string | null): string {
  return `/dashboard/ledger/${id}${from ? `?from=${encodeURIComponent(from)}` : ""}`;
}

export function JournalBody({
  journal,
  from = null,
}: {
  journal: LedgerJournalDetail;
  from?: string | null;
}) {
  const totals = totalsFromEntries(journal.entries);
  const debit = totals.debit.toString();
  const credit = totals.credit.toString();
  return (
    <>
      <div className="page-heading page-heading-actions">
        <div>
          <p className="eyebrow">Journal</p>
          <h1 className="mono payment-title">{journal.id}</h1>
          <p className="payment-meta">
            {journal.currency} · posted {formatTimestamp(journal.posted_at)}
          </p>
        </div>
        <div className="heading-actions">
          <span className="immutable-badge">Posted / immutable</span>
        </div>
      </div>

      <div
        className={`balance-check ${
          totals.balanced ? "balance-check-ok" : "balance-check-bad"
        }`}
        role="status"
      >
        <strong>
          {totals.balanced
            ? "✓ Debits equal credits"
            : "✕ Debits do not equal credits"}
        </strong>
        <span className="numeric">
          Debits {formatAggregateMoney(debit, journal.currency)}
        </span>
        <span className="numeric">
          Credits {formatAggregateMoney(credit, journal.currency)}
        </span>
        <span className="text-label">Computed from the lines below</span>
      </div>

      <div className="link-pair">
        {journal.payment_id && (
          <Link
            className="text-link"
            href={`/dashboard/payments/${journal.payment_id}`}
          >
            Payment <span className="mono">{journal.payment_id}</span>
            {journal.payment ? ` (${journal.payment.status})` : ""}
          </Link>
        )}
        {journal.reversal_of && (
          <Link
            className="text-link"
            href={reversalHref(journal.reversal_of, from)}
          >
            Reverses journal <span className="mono">{journal.reversal_of}</span>
          </Link>
        )}
        {journal.reversed_by && (
          <Link
            className="text-link"
            href={reversalHref(journal.reversed_by, from)}
          >
            Reversed by journal{" "}
            <span className="mono">{journal.reversed_by}</span>
          </Link>
        )}
      </div>

      <Table label="Journal entries" className="stack-table">
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
          {journal.entries.map((entry) => (
            <tr key={entry.id}>
              <td data-label="Account" className="mono">
                {entry.account_code}
              </td>
              <td data-label="Side">{entry.side}</td>
              <td data-label="Amount" className="numeric nowrap">
                {formatAggregateMoney(entry.amount_minor, journal.currency)}
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      <p className="text-label">
        Posting key <span className="mono">{journal.posting_key}</span> ·
        sandbox data, no real funds. Corrections are made by posting a separate
        reversal journal; posted journals are never edited or deleted.
      </p>
    </>
  );
}
