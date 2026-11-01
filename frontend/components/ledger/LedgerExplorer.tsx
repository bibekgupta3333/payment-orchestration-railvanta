"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId, type FormEvent, type KeyboardEvent } from "react";
import { Badge, Button, EmptyState, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import type { LedgerAccount, LedgerJournal } from "@/lib/api/ledger-schemas";
import {
  useLedgerAccounts,
  useLedgerJournals,
  type JournalFilters,
} from "@/lib/hooks/ledger";
import {
  formatSignedMoney,
  shortId,
  totalsFromEntries,
} from "@/lib/ledger-format";
import { currencies, formatAggregateMoney } from "@/lib/money";
import { formatTimestamp } from "@/components/payments/PaymentTable";
import "./ledger.css";

type TabName = "accounts" | "journal";
const tabs: { id: TabName; label: string }[] = [
  { id: "accounts", label: "Accounts" },
  { id: "journal", label: "Journal" },
];

/**
 * Read-only ledger explorer. The tab, currency filter, payment filter and
 * cursor live in the URL. There is no edit or delete control: posted journals
 * are immutable.
 */
export function LedgerExplorer() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const formId = useId();
  const tab: TabName =
    searchParams.get("tab") === "journal" ? "journal" : "accounts";
  const currency = searchParams.get("currency") ?? "";
  const paymentId = searchParams.get("payment_id") ?? "";
  const cursor = searchParams.get("cursor") ?? "";

  const go = (next: Record<string, string>) => {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(next)) {
      if (value) params.set(key, value);
    }
    const text = params.toString();
    router.push(text ? `${pathname}?${text}` : pathname);
  };

  const applyFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    go({
      tab,
      currency: String(data.get("currency") ?? ""),
      payment_id:
        tab === "journal" ? String(data.get("payment_id") ?? "").trim() : "",
    });
  };

  const onTabKey = (event: KeyboardEvent, index: number) => {
    const moves: Record<string, number> = {
      ArrowRight: index + 1,
      ArrowLeft: index - 1,
      Home: 0,
      End: tabs.length - 1,
    };
    if (event.key in moves) {
      event.preventDefault();
      const next = tabs[(moves[event.key] + tabs.length) % tabs.length];
      go({ tab: next.id, currency });
    }
  };

  const hasFilters = Boolean(currency || paymentId);

  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Ledger</p>
        <h1>Ledger</h1>
        <p>
          Balanced, immutable journals and the accounts they post to. Balances
          are shown per currency and are never added across currencies. Sandbox
          data; no real funds.
        </p>
      </div>

      <div role="tablist" aria-label="Ledger views" className="tab-list">
        {tabs.map((item, index) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            id={`ledger-tab-${item.id}`}
            aria-selected={tab === item.id}
            aria-controls="ledger-panel"
            tabIndex={tab === item.id ? 0 : -1}
            className="tab"
            onClick={() => go({ tab: item.id, currency })}
            onKeyDown={(event) => onTabKey(event, index)}
          >
            {item.label}
          </button>
        ))}
      </div>

      <form
        className="filter-bar"
        aria-label="Ledger filters"
        onSubmit={applyFilters}
        key={searchParams.toString()}
      >
        <div className="field">
          <label htmlFor={`${formId}-currency`}>Currency</label>
          <select
            id={`${formId}-currency`}
            name="currency"
            defaultValue={currency}
          >
            <option value="">Any currency</option>
            {currencies.map((code) => (
              <option key={code} value={code}>
                {code}
              </option>
            ))}
          </select>
        </div>
        {tab === "journal" && (
          <div className="field field-wide">
            <label htmlFor={`${formId}-payment`}>Payment ID</label>
            <input
              id={`${formId}-payment`}
              name="payment_id"
              defaultValue={paymentId}
              autoComplete="off"
              maxLength={64}
            />
          </div>
        )}
        <div className="filter-actions">
          <Button type="submit">Apply filters</Button>
          {hasFilters && (
            <Link
              className="button button-quiet"
              href={`${pathname}?tab=${tab}`}
            >
              Clear filters
            </Link>
          )}
        </div>
      </form>

      <div
        role="tabpanel"
        id="ledger-panel"
        aria-labelledby={`ledger-tab-${tab}`}
      >
        {tab === "accounts" ? (
          <AccountsPanel currency={currency} />
        ) : (
          <JournalPanel
            filters={{ currency, payment_id: paymentId, cursor }}
            onPage={(next) =>
              go({ tab, currency, payment_id: paymentId, cursor: next })
            }
            returnTo={searchParams.toString()}
            filtered={hasFilters}
          />
        )}
      </div>
    </>
  );
}

function groupByCurrency(accounts: LedgerAccount[]) {
  const groups = new Map<string, LedgerAccount[]>();
  for (const account of accounts) {
    groups.set(account.currency, [
      ...(groups.get(account.currency) ?? []),
      account,
    ]);
  }
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b));
}

function AccountsPanel({ currency }: { currency: string }) {
  const { query, workspace } = useLedgerAccounts(currency || undefined);
  return (
    <QueryState
      query={query}
      workspace={workspace}
      loadingLabel="Loading ledger accounts"
    >
      {(result) => {
        const accounts = result.data.items;
        if (accounts.length === 0) {
          return (
            <EmptyState title="No ledger accounts yet">
              Accounts appear when the first payment success is posted. Nothing
              is shown rather than invented.
            </EmptyState>
          );
        }
        return (
          <>
            {groupByCurrency(accounts).map(([code, group]) => (
              <section
                key={code}
                className="currency-group"
                aria-label={`${code} accounts`}
              >
                <h2 className="section-heading">{code} accounts</h2>
                <Table
                  label={`${code} ledger accounts`}
                  className="stack-table"
                >
                  <thead>
                    <tr>
                      <th scope="col">Account</th>
                      <th scope="col">Kind</th>
                      <th scope="col">Provider</th>
                      <th scope="col" className="numeric">
                        Debits
                      </th>
                      <th scope="col" className="numeric">
                        Credits
                      </th>
                      <th scope="col" className="numeric">
                        Balance
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {group.map((account) => (
                      <AccountRow key={account.id} account={account} />
                    ))}
                  </tbody>
                </Table>
              </section>
            ))}
            <p className="text-label">
              Updated {new Date(result.receivedAt).toLocaleTimeString()} ·
              computed from posted entries only
            </p>
          </>
        );
      }}
    </QueryState>
  );
}

function AccountRow({ account }: { account: LedgerAccount }) {
  const overflow = account.balance_error !== null;
  return (
    <tr>
      <td data-label="Account" className="mono">
        {account.code}
      </td>
      <td data-label="Kind">{account.kind}</td>
      <td data-label="Provider">{account.provider_code ?? "—"}</td>
      {overflow ? (
        <td
          data-label="Balance"
          colSpan={3}
          className="overflow-cell"
          role="alert"
        >
          Total exceeds the supported range (amount overflow). No rounded value
          is shown.
        </td>
      ) : (
        <>
          <td data-label="Debits" className="numeric nowrap">
            {formatAggregateMoney(
              account.debit_total_minor ?? "0",
              account.currency,
            )}
          </td>
          <td data-label="Credits" className="numeric nowrap">
            {formatAggregateMoney(
              account.credit_total_minor ?? "0",
              account.currency,
            )}
          </td>
          <td data-label="Balance" className="numeric nowrap">
            {formatSignedMoney(account.balance_minor ?? "0", account.currency)}
          </td>
        </>
      )}
    </tr>
  );
}

function JournalPanel({
  filters,
  onPage,
  returnTo,
  filtered,
}: {
  filters: JournalFilters;
  onPage: (cursor: string) => void;
  returnTo: string;
  filtered: boolean;
}) {
  const { query, workspace } = useLedgerJournals(filters);
  const from = returnTo ? `?from=${encodeURIComponent(returnTo)}` : "";
  return (
    <QueryState
      query={query}
      workspace={workspace}
      loadingLabel="Loading journals"
    >
      {(result) => {
        const page = result.data;
        if (page.items.length === 0) {
          return (
            <EmptyState
              title={
                filtered ? "No journals match these filters" : "No journals yet"
              }
            >
              Only posted journals are listed. A journal is posted when a
              payment succeeds.
            </EmptyState>
          );
        }
        return (
          <>
            <Table label="Posted journals" className="stack-table">
              <thead>
                <tr>
                  <th scope="col">Journal</th>
                  <th scope="col">Posted</th>
                  <th scope="col">Payment</th>
                  <th scope="col" className="numeric">
                    Debits = credits
                  </th>
                  <th scope="col">Links</th>
                </tr>
              </thead>
              <tbody>
                {page.items.map((journal) => (
                  <JournalRow key={journal.id} journal={journal} from={from} />
                ))}
              </tbody>
            </Table>
            <nav className="pagination" aria-label="Journal pages">
              {filters.cursor && (
                <Button className="button-quiet" onClick={() => onPage("")}>
                  First page
                </Button>
              )}
              <span className="text-label">
                Updated {new Date(result.receivedAt).toLocaleTimeString()}
              </span>
              {page.next_cursor && (
                <Button
                  className="button-quiet"
                  onClick={() => onPage(page.next_cursor ?? "")}
                >
                  Next page
                </Button>
              )}
            </nav>
          </>
        );
      }}
    </QueryState>
  );
}

function JournalRow({
  journal,
  from,
}: {
  journal: LedgerJournal;
  from: string;
}) {
  const totals = totalsFromEntries(journal.entries);
  return (
    <tr>
      <td data-label="Journal">
        <Link
          className="text-link mono"
          href={`/dashboard/ledger/${journal.id}${from}`}
        >
          {shortId(journal.id)}
        </Link>
      </td>
      <td data-label="Posted" className="nowrap">
        {formatTimestamp(journal.posted_at)}
      </td>
      <td data-label="Payment">
        {journal.payment_id ? (
          <Link
            className="text-link mono"
            href={`/dashboard/payments/${journal.payment_id}`}
          >
            {shortId(journal.payment_id)}
          </Link>
        ) : (
          "—"
        )}
      </td>
      <td data-label="Debits = credits" className="numeric nowrap">
        {formatAggregateMoney(totals.debit.toString(), journal.currency)}{" "}
        <span className="text-label">
          {totals.balanced ? "balanced" : "UNBALANCED"}
        </span>
      </td>
      <td data-label="Links">
        {journal.reversal_of && <Badge>Reversal</Badge>}
        {journal.reversed_by && <Badge>Reversed</Badge>}
        {!journal.reversal_of && !journal.reversed_by && "—"}
      </td>
    </tr>
  );
}
