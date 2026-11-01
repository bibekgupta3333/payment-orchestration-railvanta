# ADR 0012: Dashboard hub and embedded Swagger API reference

Status: accepted (implemented in `frontend/components/dashboard/`, `frontend/app/dashboard/api-reference/` and `scripts/generate-api.mjs`).

## Context

The dashboard home was a metrics page. To show the product a presenter had to hunt through Settings (to generate a key), the Playground (to run a failure demo) and `/docs` (to read the contract), and there was no interactive API reference: a reviewer could read curl examples but not try an endpoint in the browser.

## Decision

- `/dashboard` becomes a hub and is treated like the main page. It keeps the live sandbox metrics but leads with quick actions, an "Integration credentials" card and a "Run a demo now" card (layout in [wireframes](../frontend/wireframes.md)).
- The credentials card shows the base URL, the bearer format, the active-key count and a curl with a `$KEY` placeholder. A one-click **Generate default demo key** calls `POST /v1/api-keys` (ADR 0011). **Nothing is minted on page load**; the secret is shown once and kept only in component state.
- "Run a demo now" offers three one-click runs (`timeout_after_success`, `duplicate_request`, `duplicate_posting`). They are real $100 simulations through `POST /v1/sandbox/simulations`, labelled Simulated, and show a compact result with a link to the Playground.
- A new page `/dashboard/api-reference` (sidebar item "API reference", linked from the hub and `/docs`) embeds Swagger UI (`swagger-ui-dist`, pinned in `frontend/package.json`) over the real OpenAPI document. `pnpm generate:api` writes a same-origin copy, `frontend/public/openapi.json`, from `backend/api/openapi.yaml` with `servers` replaced by `/`; `pnpm check:generated` fails when the copy drifts. An Authorize button takes a bearer key generated in Settings or on the hub.

## Consequences

- No key is minted on load, so merely opening the dashboard cannot exhaust the 20-active-key cap, and the secret is not retrievable after the panel is hidden or the page reloaded.
- Swagger "Try it out" writes need a bearer key because cookie-authenticated writes also need the CSRF token and origin; the page does not attempt to supply them. Read-only keys can read but not write.
- Swagger UI internals are third-party markup, so the accessibility of its internals is not asserted; the surrounding page (heading, help text, skip link, landmarks) is covered by the usual checks.
- `swagger-ui-dist` is a new third-party runtime dependency (and `yaml` a build-time one for the generator); the `@scarf/scarf` analytics postinstall is disabled in `pnpm-workspace.yaml`.
- The ADR 0011 caveats still apply: an operator key can mint further keys until revoked or expired, there is no rotation or per-key scope beyond read/operator, and the keys authorize only the sandbox API. The hub's demo buttons write simulated payments to the sandbox database like the Playground does.
