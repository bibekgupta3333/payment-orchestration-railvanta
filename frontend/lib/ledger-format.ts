import { formatAggregateMoney } from "@/lib/money";

/**
 * Formats an exact signed minor-unit string (a balance can be negative). Only
 * BigInt-safe string handling is used; there is no Number conversion.
 */
export function formatSignedMoney(
  amountMinor: string,
  currency: string,
): string {
  if (amountMinor.startsWith("-")) {
    return `-${formatAggregateMoney(amountMinor.slice(1), currency)}`;
  }
  return formatAggregateMoney(amountMinor, currency);
}

export interface EntryLine {
  side: "debit" | "credit";
  amount_minor: string;
}

export interface EntryTotals {
  debit: bigint;
  credit: bigint;
  balanced: boolean;
}

/**
 * Debit and credit sums computed from the returned lines, so the "debits equal
 * credits" check never trusts a server-supplied total. BigInt cannot overflow
 * or lose precision, so values beyond Number.MAX_SAFE_INTEGER stay exact.
 */
export function totalsFromEntries(entries: readonly EntryLine[]): EntryTotals {
  let debit = 0n;
  let credit = 0n;
  for (const entry of entries) {
    const amount = BigInt(entry.amount_minor);
    if (entry.side === "debit") debit += amount;
    else credit += amount;
  }
  return {
    debit,
    credit,
    balanced: entries.length >= 2 && debit === credit,
  };
}

export function shortId(id: string): string {
  return id.length > 13 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}
