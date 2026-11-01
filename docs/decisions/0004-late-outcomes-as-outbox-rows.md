# ADR 0004: Late provider outcomes recorded as outbox rows

Status: accepted (implemented in `payment/recovery.go`, webhook inbox and reconciliation).

## Context

The design said a terminal state must never regress and that reconciliation should find disagreements, but it had no place to record a provider result that arrives after the payment is already terminal (a success after `failed`, a decline after `succeeded`). Dropping the result loses evidence; applying it breaks the terminal-state rule or silently changes money; adding a new table needed a migration for a rarely used record.

## Decision

When the dispatch path, the lookup worker or a webhook delivers a result that conflicts with a terminal state, nothing on the payment changes. In the same transaction that completes the command, write one `outbox_events` row of type `payment.late_outcome` (deduplication key `payment.late_outcome:{attempt_id}:{outcome}`, payload describing payment, attempt, provider, outcome, recorded statuses and source `dispatch|lookup|webhook`). No dispatcher handler is registered for that type, so the row stays `pending` and is durable.

Reconciliation consumes pending rows for the payments in its window: it attaches them to the discrepancy as evidence and marks them `delivered`. `GET /v1/payments/{id}/recovery` lists them.

## Consequences

- No new table or migration; the evidence is queryable and deduplicated by the existing outbox unique key.
- The outbox now contains rows that intentionally have no consumer. Metrics and the events explorer must treat a long-lived `pending` late-outcome row as normal, not as a stuck command.
- A late success is never auto-reversed or reposted. Resolving it needs an operator decision and a separately designed correction, which does not exist.
- Reconciliation is the only consumer. If it is never run, late outcomes stay visible but unresolved.
