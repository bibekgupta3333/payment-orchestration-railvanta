"use client";

import type { Simulation } from "@/lib/api/routing-schemas";
import { useDiscrepancies } from "@/lib/hooks/reconciliation";

/** One guided demo. `scenario` is the backend scenario ID it drives. */
export interface Demo {
  id: string;
  number: number;
  title: string;
  scenario: string | null;
  what: string;
  proves: string;
  /** Part of "Run all in order" (demo 4 has no simulation). */
  inRunAll: boolean;
  contrast?: { scenario: string; label: string; what: string };
}

export const demos: Demo[] = [
  {
    id: "live",
    number: 1,
    title: "Live payment processing",
    scenario: "success",
    what: "A sandbox payment is created, routed to the mock provider and succeeds, and a balanced journal is posted.",
    proves:
      "Payment state machine, provider adapter boundary and ledger posting.",
    inRunAll: true,
  },
  {
    id: "duplicate",
    number: 2,
    title: "Duplicate payment protection",
    scenario: "duplicate_request",
    what: "The same Idempotency-Key is sent many times at once, and once with a changed body.",
    proves:
      "Idempotency and concurrency control: one payment, one attempt, one charge.",
    inRunAll: true,
  },
  {
    id: "lost",
    number: 3,
    title: "Provider succeeds, response lost",
    scenario: "timeout_after_success",
    what: "The provider charges the customer but its answer never arrives. The payment sits in unknown until a lookup resolves it.",
    proves:
      "Ambiguous-outcome handling: no blind retry, no second charge, no failover.",
    inRunAll: true,
    contrast: {
      scenario: "outage",
      label: "Contrast: provider outage",
      what: "The provider never answers: the payment stays unknown and is never failed over.",
    },
  },
  {
    id: "routing",
    number: 4,
    title: "Intelligent payment routing",
    scenario: null,
    what: "Evaluate the routing rules for a method, amount and currency without moving money.",
    proves:
      "Rule-based routing with explainable decisions and safe failover limits.",
    inRunAll: false,
  },
  {
    id: "ledger",
    number: 5,
    title: "Double-entry ledger",
    scenario: "duplicate_posting",
    what: "The success journal is posted twice more with the same posting key.",
    proves:
      "Ledger integrity: balanced, immutable, idempotent postings (debits = credits).",
    inRunAll: true,
  },
  {
    id: "reconciliation",
    number: 6,
    title: "Reconciliation",
    scenario: "settlement_mismatch",
    what: "The provider settlement report disagrees with our records and a discrepancy is raised.",
    proves:
      "Detecting and tracing money mismatches without silently adjusting the ledger.",
    inRunAll: true,
  },
];

export const unavailableReason = "backend scenario not available yet";

/** Sum of entry amounts per side, computed in BigInt (never Number). */
export function journalTotals(journal: Simulation["journals"][number]) {
  let debit = 0n;
  let credit = 0n;
  for (const entry of journal.entries) {
    const amount = BigInt(entry.amount_minor);
    if (entry.side === "debit") debit += amount;
    else credit += amount;
  }
  return {
    debit: debit.toString(),
    credit: credit.toString(),
    balanced: debit === credit,
  };
}

export function succeededAttempts(run: Simulation): number {
  return run.attempts.filter((attempt) => attempt.status === "succeeded")
    .length;
}

export function failedInvariants(run: Simulation) {
  return run.invariants.filter((item) => item.status === "fail");
}

/** Reconciliation discrepancies for the payment of a settled run. */
export function usePaymentDiscrepancy(
  paymentId: string | null,
  enabled: boolean,
) {
  const result = useDiscrepancies({}, enabled && Boolean(paymentId));
  const items = result.query.data?.data.items ?? [];
  return {
    ...result,
    match: items.find((item) => item.payment_id === paymentId) ?? null,
  };
}
