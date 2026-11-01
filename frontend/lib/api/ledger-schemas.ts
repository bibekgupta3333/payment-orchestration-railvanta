import { z } from "zod";
import type { components } from "./schema";
import { currencySchema } from "./schemas";

/**
 * Validators for the ledger explorer (WBS 4.1) and reconciliation (WBS 4.2)
 * reads. Money stays a decimal string; BigInt is the only arithmetic.
 */
type Schemas = components["schemas"];

const minorUnits = z.string().regex(/^[1-9]\d{0,18}$/);
const nonNegativeMinor = z.string().regex(/^(0|[1-9]\d{0,18})$/);
const signedMinor = z.string().regex(/^-?(0|[1-9]\d{0,18})$/);

export const ledgerEntrySchema = z.object({
  id: z.string(),
  account_id: z.string(),
  account_code: z.string(),
  provider_id: z.string().nullable(),
  side: z.enum(["debit", "credit"]),
  amount_minor: minorUnits,
});
export type LedgerEntry = z.infer<typeof ledgerEntrySchema>;

export const ledgerJournalSchema: z.ZodType<Schemas["LedgerJournal"]> =
  z.object({
    id: z.string(),
    payment_id: z.string().nullable(),
    currency: currencySchema,
    status: z.literal("posted"),
    posting_key: z.string(),
    reversal_of: z.string().nullable(),
    reversed_by: z.string().nullable(),
    created_at: z.string(),
    posted_at: z.string(),
    entries: z.array(ledgerEntrySchema),
  });
export type LedgerJournal = Schemas["LedgerJournal"];

export const ledgerTransactionPageSchema = z.object({
  items: z.array(ledgerJournalSchema),
  next_cursor: z.string().nullable(),
});
export type LedgerTransactionPage = z.infer<typeof ledgerTransactionPageSchema>;

export const ledgerJournalDetailSchema = z.object({
  id: z.string(),
  payment_id: z.string().nullable(),
  currency: currencySchema,
  status: z.literal("posted"),
  posting_key: z.string(),
  reversal_of: z.string().nullable(),
  reversed_by: z.string().nullable(),
  created_at: z.string(),
  posted_at: z.string(),
  entries: z.array(ledgerEntrySchema),
  debit_total_minor: nonNegativeMinor,
  credit_total_minor: nonNegativeMinor,
  payment: z
    .object({
      id: z.string(),
      status: z.string(),
      amount_minor: minorUnits,
      currency: currencySchema,
    })
    .nullable(),
});
export type LedgerJournalDetail = z.infer<typeof ledgerJournalDetailSchema>;

export const ledgerAccountSchema = z.object({
  id: z.string(),
  code: z.string(),
  kind: z.enum(["asset", "liability", "income", "expense", "equity"]),
  currency: currencySchema,
  provider_id: z.string().nullable(),
  provider_code: z.string().nullable(),
  debit_total_minor: nonNegativeMinor.nullable(),
  credit_total_minor: nonNegativeMinor.nullable(),
  balance_minor: signedMinor.nullable(),
  balance_error: z.literal("amount_overflow").nullable(),
  normal_balance: z.enum(["debit", "credit"]),
  entry_count: z.number().int(),
});
export type LedgerAccount = z.infer<typeof ledgerAccountSchema>;

export const ledgerAccountPageSchema = z.object({
  items: z.array(ledgerAccountSchema),
});

export const discrepancyKindSchema = z.enum([
  "missing_posting",
  "unknown_outcome",
  "amount_mismatch",
  "status_mismatch",
  "missing_at_provider",
]);
export type DiscrepancyKind = z.infer<typeof discrepancyKindSchema>;
export const discrepancyStatusSchema = z.enum([
  "open",
  "investigating",
  "resolved",
]);
export type DiscrepancyStatus = z.infer<typeof discrepancyStatusSchema>;

export const reconciliationRunSchema = z.object({
  id: z.string(),
  run_key: z.string(),
  status: z.enum(["pending", "running", "completed", "failed"]),
  window_start: z.string(),
  window_end: z.string(),
  created_at: z.string(),
  completed_at: z.string().nullable(),
  requested_by: z.string().nullable(),
  counts: z.object({
    compared: z.number().int(),
    matched: z.number().int(),
    unmatched: z.number().int(),
    mismatch: z.number().int(),
  }),
  evidence_consumed_count: z.number().int(),
  simulated: z.boolean(),
});
export type ReconciliationRun = z.infer<typeof reconciliationRunSchema>;

export const reconciliationRunPageSchema = z.object({
  items: z.array(reconciliationRunSchema),
  next_cursor: z.string().nullable(),
});

export const discrepancySchema = z.object({
  id: z.string(),
  run_id: z.string(),
  payment_id: z.string().nullable(),
  kind: discrepancyKindSchema,
  status: discrepancyStatusSchema,
  details: z.object({
    source: z.string(),
    simulated: z.boolean(),
    summary: z.string(),
    expected: z.string().optional(),
    payment: z.object({
      id: z.string(),
      status: z.string(),
      amount_minor: z.string(),
      currency: z.string(),
    }),
    provider_charge: z
      .object({
        id: z.string(),
        provider_reference: z.string(),
        status: z.string(),
        amount_minor: z.string(),
        currency: z.string(),
        scenario: z.string(),
      })
      .nullable(),
    journal: z
      .object({
        id: z.string(),
        debit_total_minor: z.string(),
        credit_total_minor: z.string(),
      })
      .nullable(),
    late_outcomes: z.array(
      z.object({
        event_id: z.string(),
        outcome: z.string(),
        source: z.string(),
        payment_status: z.string(),
        attempt_status: z.string(),
        observed_at: z.number(),
      }),
    ),
  }),
  resolution_note: z.string().nullable(),
  resolved_by: z.string().nullable(),
  created_at: z.string(),
  resolved_at: z.string().nullable(),
  related_journal_id: z.string().nullable(),
  history: z.array(
    z.object({
      id: z.string(),
      from_status: discrepancyStatusSchema,
      to_status: discrepancyStatusSchema,
      note: z.string(),
      actor: z.string(),
      created_at: z.string(),
    }),
  ),
  simulated: z.boolean(),
});
export type Discrepancy = z.infer<typeof discrepancySchema>;

export const discrepancyPageSchema = z.object({
  items: z.array(discrepancySchema),
  next_cursor: z.string().nullable(),
});

export const sessionSchema = z.object({
  workspace_id: z.string(),
  role: z.enum(["read", "operator"]),
});
export type Session = z.infer<typeof sessionSchema>;
