# ADR 0005: Operator-authenticated, unsigned sandbox webhook endpoint

Status: accepted (implemented in `POST /v1/sandbox/webhooks/mock`, WBS 4.3).

## Context

The design said provider webhooks require raw-body signature verification and a durable deduplicating inbox before any adapter is enabled, and drew an inbound "signed provider delivery". There is no real provider, so there is nothing to sign or verify. The inbox, duplicate handling and late-outcome behavior still needed to be built and demonstrated.

## Decision

Implement the durable inbox and its state handling now, and feed it through one sandbox endpoint, `POST /v1/sandbox/webhooks/mock`. It uses the normal `/v1` authentication, requires the `operator` role and is scoped to the caller's workspace. It is not a public receiver. Mock events are unsigned, so `signature_status` is always `not_applicable`; `verified` and `rejected` exist in the schema for a future adapter but nothing writes them.

Inbox behavior is real: the first `(provider, event_id)` is inserted and applied through the status-guarded outcome functions (202); a byte-identical repeat increments `duplicate_count` and answers 200; a different payload hash answers 409 `event_payload_conflict`, sets `investigation_flag` and keeps the first payload; events that do not match state are recorded as `ignored` with a reason. Triggers make identity and payload immutable and forbid deletes.

## Consequences

- Duplicate, reordered and conflicting deliveries are exercised end to end without pretending to verify authenticity.
- Real signature verification, replay-window checks and a public, unauthenticated-by-session receiver are not implemented and must be designed against a real provider's contract.
- The simulator's `duplicate_webhook` and `reordered_events` scenarios are still listed as unavailable (their catalog entry names WBS 4.3 as the dependency); the behavior itself is covered by Go and Playwright event tests instead.
- Anyone with an operator key can post events for their own workspace. That is a sandbox-only trust assumption.
