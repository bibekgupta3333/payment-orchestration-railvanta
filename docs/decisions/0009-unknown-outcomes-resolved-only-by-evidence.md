# ADR 0009: Unknown outcomes resolve only on provider evidence, with unbounded lookup

Status: accepted (implemented in `payment/recovery.go`, `event/dispatcher.go`, WBS 2.4).

## Context

The design said an unknown outcome is resolved by querying the original provider and must never trigger another provider, but left open what happens when the provider stays silent, when it has no record, and whether an operator or a timer may end the uncertainty. The outbox dispatcher also parks a row as `failed` after 10 acquisitions, which would abandon a lookup that is still needed.

## Decision

- A `payment.lookup` command is created for each ambiguous attempt (one per attempt) and runs against the attempt's own provider with the stored `provider_idempotency_key`.
- A recorded success resolves to `succeeded` and posts the journal; a recorded decline resolves to `failed`. Silence, `ErrUnavailable`, "still processing" and "no record" keep the payment `unknown`.
- Absence of a record is never proof of no effect. If the adapter declares `IdempotentReplay`, the worker may resubmit the same key to the same provider; otherwise nothing is sent.
- Lookup retries use `event.RetryAfterUnbounded`: jittered exponential backoff (`RECOVERY_BASE_BACKOFF` 1 s to `RECOVERY_MAX_BACKOFF` 5 min) and no `MaxAttempts` parking.
- No endpoint retries, resubmits or clears `unknown`; `retry_allowed` is always `false`.
- On startup, any `submitted` attempt without a live dispatch lease is moved to `unknown` and looked up, never resubmitted.

## Consequences

- No automatic cross-provider retry, no timer-based failure, no operator override: the invariant is simple to test, and crash points converge on one attempt, at most one charge and one journal.
- A provider with neither lookup nor replay leaves payments `unknown` indefinitely; only reconciliation evidence records the situation.
- Startup recovery after a crash can wait up to `DISPATCHER_LEASE` (30 s default) for a live lease to expire.
- Operators must watch the unknown count and oldest-unresolved age; metrics are in-process only.
