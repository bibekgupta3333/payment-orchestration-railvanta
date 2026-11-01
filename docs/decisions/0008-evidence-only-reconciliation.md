# ADR 0008: Reconciliation is synchronous, evidence-only and reads the mock's records

Status: accepted (implemented in `backend/internal/reconciliation`, WBS 4.2).

## Context

The original component diagram showed the reconciliation service calling the provider adapter, and the flow text described an operator-started comparison that could lead to "validated resolution or explicit reversal". With only a mock provider, adapter-based comparison would just ask the mock what it already decided, and a resolution that edits payments or journals would break the append-only ledger and terminal-state rules.

## Decision

`POST /v1/reconciliation/runs` runs one synchronous comparison in a single write transaction, keyed by the `Idempotency-Key` and a window of at most 366 days. It reads payments, the latest attempt's key, the posted success journal and the mock charge for that key (through `mock.ChargesByKeys`, not `provider.Provider`). It writes only `reconciliation_*` rows and marks consumed `payment.late_outcome` rows delivered ([ADR 0004](0004-late-outcomes-as-outbox-rows.md)). It never calls a provider and never changes a payment, attempt, journal or charge.

Resolving a discrepancy records a status (`open`, `investigating`, `resolved`), a required note and the actor in an append-only history. `resolved` is final. Triggers enforce evidence immutability. Evidence is labelled simulated.

## Consequences

- A run is idempotent and cheap to repeat, and cannot "fix" anything by accident.
- There is no correction path: an `unknown` payment stays `unknown`, a missing posting stays missing and a late success stays unreversed until a correction workflow is designed. Reversal exists in `ledger.Reverse` but is not exposed.
- A real provider would need an adapter-side lookup and a bounded, asynchronous run; this design does not scale to large windows.
- Reconciliation depends on mock internals, so it must be redesigned with the first real adapter.
