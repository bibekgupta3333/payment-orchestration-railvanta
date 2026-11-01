"use client";

import {
  ledgerAccountPageSchema,
  ledgerJournalDetailSchema,
  ledgerTransactionPageSchema,
  sessionSchema,
} from "@/lib/api/ledger-schemas";
import { useScopedRead } from "./payments";

export interface JournalFilters {
  currency?: string;
  payment_id?: string;
  cursor?: string;
}

export function journalQueryString(filters: JournalFilters, limit = "20") {
  const params = new URLSearchParams({ limit });
  for (const name of ["currency", "payment_id", "cursor"] as const) {
    const value = filters[name];
    if (value) params.set(name, value);
  }
  return `?${params.toString()}`;
}

export function useLedgerAccounts(currency?: string) {
  const suffix = currency ? `?currency=${encodeURIComponent(currency)}` : "";
  return useScopedRead(
    ["ledger", "accounts", currency ?? "all"],
    `/v1/ledger/accounts${suffix}`,
    ledgerAccountPageSchema,
  );
}

export function useLedgerJournals(filters: JournalFilters) {
  return useScopedRead(
    ["ledger", "journals", filters],
    `/v1/ledger/transactions${journalQueryString(filters)}`,
    ledgerTransactionPageSchema,
  );
}

export function useLedgerJournal(id: string) {
  return useScopedRead(
    ["ledger", "journal", id],
    `/v1/ledger/transactions/${encodeURIComponent(id)}`,
    ledgerJournalDetailSchema,
  );
}

export function useSession() {
  return useScopedRead(["session"], "/v1/session", sessionSchema);
}
