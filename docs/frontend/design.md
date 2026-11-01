# Frontend Design

Next.js App Router frontend, implemented in `frontend/` and verified locally (Vitest, Playwright, axe at 375 and 1440 px; see the [verification matrix](../verification-matrix.md)). It owns presentation only; the Go API owns all authority and state ([architecture](../architecture.md)). Page layouts are design intent in [wireframes](wireframes.md), which notes where the build differs.

## Stack and responsibilities

| Tool | Responsibility (present unless noted) |
| --- | --- |
| Next.js App Router + TypeScript | Routes, layouts, static public documentation, server rendering |
| Tailwind CSS 4 | Theme tokens (`@theme`), responsive layout, light/dark themes, plus global and feature stylesheets |
| Tailwind UI / Tailwind Plus | **Not used.** No licensed source; see [ADR 0010](../decisions/0010-original-frontend-components.md) |
| TanStack Query | API query state, polling, scoped cache invalidation |
| React Hook Form + Zod | Accessible forms and runtime request validation |
| Recharts | Payment-volume chart with a text alternative. **`lucide-react` is not used**; navigation icons are Unicode glyphs |
| Vitest + React Testing Library | Component behavior and formatting tests |
| Playwright | Browser flows and responsive/accessibility checks |

Tailwind UI was renamed Tailwind Plus. This repository contains no licensed assets, so all components are original and accessible. Record component provenance before adding any commercial source. See the [official rename announcement](https://tailwindcss.com/blog/tailwind-plus) and [component documentation](https://tailwindcss.com/plus/ui-blocks/documentation/using-html).

## Directory structure

```text
frontend/
  app/
    layout.tsx, globals.css    # fonts (Inter), theme, global providers
    page.tsx                   # public landing page
    not-found.tsx
    docs/page.tsx              # public developer documentation (examples in lib/docs/examples.json)
    dashboard/
      layout.tsx               # workspace shell
      page.tsx                 # dashboard hub (credentials, demo runs, metrics)
      api-reference/page.tsx   # embedded Swagger UI
      payments/page.tsx, payments/[id]/page.tsx
      routing/page.tsx
      providers/page.tsx, providers/[id]/page.tsx
      simulator/page.tsx
      ledger/page.tsx, ledger/[id]/page.tsx
      reconciliation/page.tsx
      events/page.tsx, events/[id]/page.tsx
      settings/page.tsx
  components/
    ui/                        # Button, Badge, Table, Skeleton, EmptyState, dialog, tabs, query-state, copy-code
    layout/app-shell.tsx       # sidebar, topbar, mobile drawer
    payments/ routing/ provider-views/ simulator/ ledger/ reconciliation/ events/
    settings/ dashboard/ charts/ docs/
    providers.tsx, theme.tsx, feature-page.tsx
  lib/
    api/                       # client.ts, Zod schemas per area, generated schema.d.ts
    hooks/                     # payments, routing, ledger, reconciliation, events, useCreatePayment
    money/                     # exact minor-unit formatting
    docs/                      # example snippets, checked against OpenAPI by pnpm check:docs
    ledger-format.ts
  tests/components/            # Vitest + Testing Library; browser E2E lives in /e2e
```

## Rendering and API boundary

The dashboard home is a hub treated like the main page: heading and quick actions, an "Integration credentials" card (base URL, bearer format, active-key count, curl with a `$KEY` placeholder, and a **Generate default demo key** button that mints only on click and shows the secret once from component state), a "Run a demo now" card (Lost response, Duplicate requests, Double-entry ledger as real $100 simulations with a compact invariant result and a link to the Playground), a capability strip and the live metrics. Read-only roles see an explanation instead of the generate button. `/dashboard/api-reference` embeds Swagger UI for the real OpenAPI document; the sidebar item is "API reference". Swagger UI internals are third-party markup and are outside the accessibility claims below.

Render the public landing page and developer documentation on the server. Keep dashboard layouts server-rendered where practical; isolate query providers, filters, forms, dialogs, and charts in Client Components. The [`use client` boundary](https://nextjs.org/docs/app/api-reference/directives/use-client) belongs at interactive entry points, not every file.

The browser calls a same-origin `/v1` reverse proxy routed to the Go API. Next.js owns presentation; Go owns authorization, payment transitions, routing decisions, validation, persistence, and provider credentials. Never open SQLite from Next.js. Configure the same `/v1` routing contract in local development and deployment. An ingress or server-side proxy forwards cookies and correlation identifiers but must not cache workspace responses publicly. In development `next.config.ts` rewrites `/v1/:path*` to `API_INTERNAL_ORIGIN`; the Compose Nginx proxy and the Kubernetes Ingress do the same routing in deployment, preserving the `/v1` prefix. The build uses `output: "standalone"`. Cookie-authenticated writes send `X-CSRF-Token` read from the `railvanta_csrf` cookie; the session cookie itself is not readable by script.

`lib/api/client.ts` is the central typed client. `pnpm generate:api` produces `lib/api/schema.d.ts` from `backend/api/openapi.yaml` and `pnpm check:generated` fails on drift; Zod schemas in `lib/api/*-schemas.ts` validate response payloads at the boundary because TypeScript types alone cannot. The routes and endpoints below exist. The `/dashboard` hub and the Playground share the simulation endpoints with `/dashboard/simulator`.

| Route | API dependencies | Scope |
| --- | --- | --- |
| `/` | None | Landing, architecture overview, sandbox entry |
| `/dashboard` | `GET /v1/api-keys`, `POST /v1/api-keys`, `GET /v1/sandbox/scenarios`, `POST /v1/sandbox/simulations` (and `GET /v1/sandbox/simulations/{id}` to follow a run), `GET /v1/dashboard/summary`, `/v1/dashboard/payment-volume` | Hub: integration credentials with one-click default demo key, three one-click demo runs, capability strip, live sandbox metrics. Nothing is minted or run on load |
| `/dashboard/api-reference` | None of its own; the embedded Swagger UI loads the OpenAPI document served from the same origin (`frontend/public/openapi.json`, generated from `backend/api/openapi.yaml` by `pnpm generate:api` with `servers` set to `/`; `pnpm check:generated` fails on drift. See ADR 0012) and sends "Try it out" calls to `/v1` with a bearer key | Interactive API reference with an Authorize button |
| `/dashboard/payments` | `GET /v1/payments`, `/v1/payment-methods`, `POST /v1/payments` | Filtered list and sandbox creation |
| `/dashboard/payments/[id]` | `GET /v1/payments/{id}`, `/attempts`, `/history`, `/v1/ledger/transactions` | Attempts, transitions and durable events, linked journal. Polls every 3 s until terminal |
| `/dashboard/routing` | `GET /v1/routing/rules`, `POST /v1/routing/evaluate` | Inspect rules (read-only) and evaluate a decision |
| `/dashboard/providers`, `/dashboard/providers/[id]` | `GET /v1/providers`, `/v1/providers/health`, `/v1/providers/{id}` | Configured capabilities and observed health |
| `/dashboard/simulator` | `GET /v1/sandbox/scenarios`, `POST /v1/sandbox/simulations`, `GET /v1/sandbox/simulations/{id}` | Backend mock-provider fault injection; polls every 1 s while running |
| `/dashboard/ledger`, `/dashboard/ledger/[id]` | `GET /v1/ledger/transactions`, `/v1/ledger/accounts`, `/v1/ledger/transactions/{id}` | Read-only journals, accounts and balances |
| `/dashboard/reconciliation` | `GET /v1/reconciliation/runs`, `/discrepancies`; `POST` runs and `/resolve` | Runs, discrepancies, resolution history (operators can run and resolve) |
| `/dashboard/events`, `/dashboard/events/[id]` | `GET /v1/events`, `/v1/events/{id}` | Correlated inbox and outbox events and processing results |
| `/docs` | None at runtime (static page; examples are checked against the OpenAPI document by `pnpm check:docs`) | Supported API examples and retry semantics; links to the API reference |
| `/dashboard/settings` | `GET /v1/settings`, local theme, `GET`/`POST /v1/api-keys`, `DELETE /v1/api-keys/{id}` | Workspace, role, provider status, the sandbox API key generator (operators) and unavailable capabilities |

API endpoints **not used by the UI**: `POST /v1/payments/{id}/cancel`, `GET /v1/payments/{id}/recovery`, `POST /v1/sandbox/webhooks/mock`, `GET /v1/workspaces/{id}`, `GET /v1/payment-methods/{id}` and `DELETE /v1/api-keys/current`. The UI has no cancel control, no recovery panel and no webhook sender.

Use URL query parameters for search, filters, sort, and cursor so refresh/back navigation retains the current view. Page responses return `items` and an opaque `next_cursor`; queries include workspace and environment in their cache keys. The backend derives permitted workspace access from the session and validates every supplied identifier.

Payment filters use the canonical values `created`, `processing`, `succeeded`, `failed`, `canceled`, and `unknown`. Attempt details use `created`, `submitted`, `succeeded`, `failed`, and `unknown`. Present `unknown` as “Outcome unknown” with a reconciliation explanation and no payment retry action. Display labels may change capitalization; API enum values must remain unchanged.

## Money, requests, and errors

Represent API minor units as decimal strings, for example `{ "amount_minor": "25000", "currency": "USD" }`. Keep strings at the JSON boundary and use exact integer arithmetic for formatting and aggregation; never convert monetary values through floating-point `Number`. Currency metadata determines decimal places; not every currency has two. Validate precision and bounds on both client and server.

A payment-create request also includes `payment_method_id` referencing an authorized existing sandbox method. The mock demo can preselect a seeded method rather than collecting raw payment credentials. `GET /v1/payment-methods` returns safe method metadata (no tokens) and is in the OpenAPI document; the create dialog preselects a seeded sandbox method.

Generate one idempotency key for one payment intent. Retain it with the unchanged request through a transport timeout; editing the intent requires a new key. Disable duplicate clicks while a request is pending. Never display payment success optimistically. An uncertain request displays “Outcome unknown — checking status,” not a declined state or an invitation to switch providers. The create flow keeps a pending intent in `sessionStorage` and resends a lost submission with the same key so the server replays the stored acceptance (`Idempotent-Replayed: true`); the pending record is cleared only on an API-confirmed response.

Mutation retries are explicitly disabled (`retry: false`); recovery uses the idempotency contract above. Reads retry a bounded number of times through `shouldRetryRead`, excluding authorization, validation and missing-resource errors; `staleTime` is 15 s. See [TanStack query retries](https://tanstack.com/query/latest/docs/framework/react/guides/query-retries) and [mutation behavior](https://tanstack.com/query/latest/docs/framework/react/guides/mutations).

Use an error envelope with a machine-readable code, safe message, request identifier, and optional field errors. Show field errors adjacent to inputs. Show session expiry with a sign-in action, permission errors without automatic retry, and transient read failures with “Retry.” A request timeout does not establish the provider outcome. Do not expose stack traces, raw webhook bodies, authentication tokens, or full idempotency keys in the UI.

## Shared visual and interaction rules

Navy `#14253E`, teal `#12A89D` and background `#F8FAFC` from the brief are theme tokens. Semantic tokens cover for text, borders, focus, success, warning, and danger; choose accessible text shades independently of decorative accent colors. The font is Inter (`@fontsource-variable/inter`); money uses tabular numerals. Light, dark and system themes are supported without changing status labels.

The desktop shell has persistent navigation, a fixed "Sandbox workspace" label and Sandbox badge, a theme selector and a static account panel (there is no workspace switcher, notification control or account action). The mobile shell uses a navigation drawer and single-column content. Live controls do not exist. Settings shows Stripe and Plaid as “Not configured”; reconciliation evidence and volume series say “Simulated.” Metrics show source, time range, and last-update time; absent observations display “No data,” never invented percentages.

Provide loading skeletons with stable dimensions, deliberate empty states, inline recoverable errors, and a clear unavailable-feature state. Disabled future actions explain their dependency; a visually enabled action must perform its advertised operation. Routing changes, capture, refunds and key or member management are **not implemented** and have no UI. A reconciliation run is idempotent per run key, so operators can re-run it.

## Accessibility and acceptance checks

- Provide landmarks, a skip link, visible focus, meaningful headings, explicit form labels, and keyboard access to every action.
- Trap focus inside dialogs/drawers, support Escape, and restore focus to the trigger. Never move focus on background refresh.
- Give charts a summary and equivalent data table. Pair status colors with text/icons. Respect reduced motion and announce asynchronous results without repeatedly announcing polling updates.
- At 375-pixel and 1440-pixel widths, verify navigation, filters, long identifiers, tables, dialogs, error text, and zoom behavior. Preserve access to full row details on narrow screens.
- Test exact money formatting, duplicate submission prevention, stale-cache workspace isolation, unknown outcomes, empty/error/loading states, and immutable journal presentation. Run browser flows against the real deterministic backend mock; frontend fixture tests do not prove financial correctness.
