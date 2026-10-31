# Frontend Design

Next.js App Router frontend. It owns presentation only; the Go API owns all authority and state ([architecture](../architecture.md)). Page layouts are in [wireframes](wireframes.md).

## Stack and responsibilities

| Tool | Planned responsibility |
| --- | --- |
| Next.js App Router + TypeScript | Routes, layouts, static public documentation, server rendering |
| Tailwind CSS | Design tokens, responsive layout, light/dark themes |
| Tailwind UI / Tailwind Plus | Licensed application-shell, form, dialog, and table patterns where available |
| TanStack Query | API query state, polling, scoped cache invalidation |
| React Hook Form + Zod | Accessible forms and runtime request validation |
| Lucide React + Recharts | Icons and charts with text/table alternatives |
| Vitest + React Testing Library | Component behavior and formatting tests |
| Playwright | Browser flows and responsive/accessibility checks |

Tailwind UI was renamed Tailwind Plus. Use licensed assets only when the project has authorized access; this repository contains none. Otherwise build original accessible components with Tailwind CSS. Record component provenance before adding commercial source. See the [official rename announcement](https://tailwindcss.com/blog/tailwind-plus) and [component documentation](https://tailwindcss.com/plus/ui-blocks/documentation/using-html).

## Proposed directory structure

```text
frontend/
  app/
    layout.tsx                 # fonts, theme, global providers
    page.tsx                   # public landing page
    docs/page.tsx              # public developer documentation
    dashboard/
      layout.tsx               # workspace shell
      page.tsx                 # overview
      payments/[id]/page.tsx
      routing/page.tsx
      providers/[id]/page.tsx
      simulator/page.tsx
      ledger/[id]/page.tsx
      reconciliation/page.tsx
      events/page.tsx
      settings/page.tsx
  components/{ui,layout,payments,charts,simulator}/
  lib/{api,hooks,money}/
  tests/components/            # Vitest + Testing Library; browser E2E lives in /e2e
```

Add list `page.tsx` files under `payments/`, `providers/`, and `ledger/`; detail routes supplement their list pages. Keep feature-specific components near their domain. 

## Rendering and API boundary

Render the public landing page and developer documentation on the server. Keep dashboard layouts server-rendered where practical; isolate query providers, filters, forms, dialogs, and charts in Client Components. The [`use client` boundary](https://nextjs.org/docs/app/api-reference/directives/use-client) belongs at interactive entry points, not every file.

The browser calls a same-origin `/v1` reverse proxy routed to the Go API. Next.js owns presentation; Go owns authorization, payment transitions, routing decisions, validation, persistence, and provider credentials. Never open SQLite from Next.js. Configure the same `/v1` routing contract in local development and deployment. An ingress or server-side proxy forwards cookies and correlation identifiers but must not cache workspace responses publicly. The local Next.js rewrite now preserves `/v1/:path*` using `API_INTERNAL_ORIGIN`; deployed ingress configuration remains later work. Runtime verification is pending dependency installation.

Use a central typed client in `lib/api/`. Generate contracts from the future OpenAPI document and validate external payloads at the boundary. TypeScript types alone cannot validate network responses. All endpoints below remain proposed.

| Route | API dependencies | Scope |
| --- | --- | --- |
| `/` | None | Landing, architecture overview, sandbox entry |
| `/dashboard` | `GET /v1/dashboard/summary`, `/v1/dashboard/payment-volume`, `/v1/payments` | Workspace metrics and recent activity |
| `/dashboard/payments` | `GET /v1/payments`, `POST /v1/payments` | Filtered list and sandbox creation |
| `/dashboard/payments/[id]` | `GET /v1/payments/{id}`, `/v1/payments/{id}/attempts` | Attempts, transitions, related events/ledger |
| `/dashboard/routing` | `GET /v1/routing/rules`, `POST /v1/routing/evaluate` | Inspect rules and evaluate a decision |
| `/dashboard/providers`, `/dashboard/providers/[id]` | `GET /v1/providers/health` | Configured capabilities and observed health |
| `/dashboard/simulator` | `POST /v1/sandbox/simulations`, payment/attempt/event reads | Backend mock-provider fault injection |
| `/dashboard/ledger`, `/dashboard/ledger/[id]` | `GET /v1/ledger/transactions` | Read-only journal and balance detail |
| `/dashboard/reconciliation` | `GET /v1/reconciliation/runs`, `/v1/reconciliation/discrepancies` | Runs, discrepancies, resolution history |
| `/dashboard/events` | `GET /v1/events` | Correlated events and processing results |
| `/docs` | Future OpenAPI artifact | Supported API examples and retry semantics |
| `/dashboard/settings` | None for local theme; authorized configuration API deferred | Theme, environment, configured capabilities |

Use URL query parameters for search, filters, sort, and cursor so refresh/back navigation retains the current view. Page responses return `items` and an opaque `next_cursor`; queries include workspace and environment in their cache keys. The backend derives permitted workspace access from the session and validates every supplied identifier.

Payment filters use the canonical values `created`, `processing`, `succeeded`, `failed`, `canceled`, and `unknown`. Attempt details use `created`, `submitted`, `succeeded`, `failed`, and `unknown`. Present `unknown` as “Outcome unknown” with a reconciliation explanation and no payment retry action. Display labels may change capitalization; API enum values must remain unchanged.

## Money, requests, and errors

Represent API minor units as decimal strings, for example `{ "amount_minor": "25000", "currency": "USD" }`. Keep strings at the JSON boundary and use exact integer arithmetic for formatting and aggregation; never convert monetary values through floating-point `Number`. Currency metadata determines decimal places; not every currency has two. Validate precision and bounds on both client and server.

A payment-create request also includes `payment_method_id` referencing an authorized existing sandbox method. The mock demo can preselect a seeded method rather than collecting raw payment credentials. A complete method-selection/read contract belongs in the future OpenAPI specification; provider-bound methods must remain compatible with the routing result.

Generate one idempotency key for one payment intent. Retain it with the unchanged request through a transport timeout; editing the intent requires a new key. Disable duplicate clicks while a request is pending. Never display payment success optimistically. An uncertain request displays “Outcome unknown — checking status,” not a declined state or an invitation to switch providers. Before implementing replay after a lost response, specify a secure server lookup/replay contract in OpenAPI.

Configure mutation retries explicitly as disabled; recovery uses the backend's documented idempotency/status contract. Configure bounded retries for safe reads, excluding authorization, validation, and missing-resource errors. See [TanStack query retries](https://tanstack.com/query/latest/docs/framework/react/guides/query-retries) and [mutation behavior](https://tanstack.com/query/latest/docs/framework/react/guides/mutations).

Use an error envelope with a machine-readable code, safe message, request identifier, and optional field errors. Show field errors adjacent to inputs. Show session expiry with a sign-in action, permission errors without automatic retry, and transient read failures with “Retry.” A request timeout does not establish the provider outcome. Do not expose stack traces, raw webhook bodies, authentication tokens, or full idempotency keys in the UI.

## Shared visual and interaction rules

Use deep navy `#14253E`, teal `#12A89D`, and background `#F8FAFC` from the brief. Use semantic tokens for text, borders, focus, success, warning, and danger; choose accessible text shades independently of decorative accent colors. Use Inter or Geist, restrained borders, an 8-pixel spacing rhythm, and tabular numerals for money. Support light/dark themes without changing semantic status labels.

The desktop shell contains persistent navigation, workspace context, a fixed Sandbox badge, notifications only when available, and an account menu. The mobile shell uses an accessible navigation drawer and single-column content. Live controls remain unavailable. Stripe/Plaid cards say “Not configured” until a real authorized sandbox adapter exists; mock activity says “Simulated.” Metrics show source, time range, and last-update time; absent observations display “No data,” never invented percentages.

Provide loading skeletons with stable dimensions, deliberate empty states, inline recoverable errors, and a clear unavailable-feature state. Disabled future actions explain their dependency; a visually enabled action must perform its advertised operation. Routing changes, reconciliation reruns, capture, refunds, and key management stay deferred until their backend contracts and authorization exist.

## Accessibility and acceptance checks

- Provide landmarks, a skip link, visible focus, meaningful headings, explicit form labels, and keyboard access to every action.
- Trap focus inside dialogs/drawers, support Escape, and restore focus to the trigger. Never move focus on background refresh.
- Give charts a summary and equivalent data table. Pair status colors with text/icons. Respect reduced motion and announce asynchronous results without repeatedly announcing polling updates.
- At 375-pixel and 1440-pixel widths, verify navigation, filters, long identifiers, tables, dialogs, error text, and zoom behavior. Preserve access to full row details on narrow screens.
- Test exact money formatting, duplicate submission prevention, stale-cache workspace isolation, unknown outcomes, empty/error/loading states, and immutable journal presentation. Run browser flows against the real deterministic backend mock; frontend fixture tests do not prove financial correctness.
