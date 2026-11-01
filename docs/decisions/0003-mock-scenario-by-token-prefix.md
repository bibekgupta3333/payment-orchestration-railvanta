# ADR 0003: Mock provider scenario chosen by token reference prefix

Status: accepted (implemented in `backend/internal/provider/mock`).

## Context

The design called for a deterministic mock provider with scripted outcomes (success, decline, timeout, outage, delayed success) but did not say what selects the outcome. Options were a request field, a header, the amount, a global switch, or randomness. A request field or header would let a client choose outcomes on the public payment API; the amount would couple unrelated tests; randomness breaks reproducibility and crash tests.

## Decision

The scenario comes only from the persisted `payment_method_tokens.token_reference` for the chosen provider. A longest-prefix match picks it: `mock_success`, `mock_decline`, `mock_delayed_success`, `mock_timeout_before_execution`, `mock_timeout_after_success`, `mock_outage`; anything else is `success`. Seeded demo methods `00000000-0000-7000-8000-00000000012{1..6}` carry these tokens. The chosen scenario is stored on the charge row keyed by the provider idempotency key, so a lookup or replay uses the recorded behavior even if the token later changes.

The simulator selects a fault by picking the matching seeded method, so it goes through the ordinary `POST /v1/payments` path.

## Consequences

- Outcomes are reproducible and require no special API surface; the payment API has no test-only parameters.
- Anyone who can choose a payment method can choose the outcome. That is acceptable only because the provider is a sandbox mock and methods are workspace data, not client input.
- Scenario names are part of token naming; a real adapter must not interpret tokens this way.
- Delayed-success charges settle on their first lookup or replay, so the mock cannot model a provider that stays "processing" across many lookups.
