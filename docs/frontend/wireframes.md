# Frontend Wireframes

Low-fidelity layouts per route. IDs, amounts, times and metrics are illustrative; the real app shows only API data or explicitly labeled seeded/simulated data. Routes, API dependencies and accessibility rules are in [frontend design](design.md).

## Implementation status

All routes below exist in `frontend/`, as does `/dashboard/playground` (see the [demo runbook](../demo/README.md)). The wireframes remain design intent, not a specification of pixels or copy. Differences found by comparing them with the code (features, not a pixel review):

- **Shell:** there is no workspace switcher (the topbar shows a fixed "Sandbox workspace" label and the Sandbox badge), no notification control, and the account menu is a static panel with no actions. A "Sandbox build / Sandbox infrastructure. No real funds." note sits in the sidebar. See [ADR 0010](../decisions/0010-original-frontend-components.md).
- **Icons:** navigation uses Unicode glyphs, not an icon set.
- **Landing page:** no GitHub link (no destination is configured); Stripe and Plaid are shown as planned and not configured.
- **Payments:** filters and cursor live in the URL as designed. The detail page has Attempts, Events and Journal tabs. There is **no cancel control** and **no recovery panel**, although `POST /v1/payments/{id}/cancel` and `GET /v1/payments/{id}/recovery` exist in the API.
- **Routing:** rules are inspect-only, as drawn; there is no rule editor.
- **Simulator:** `duplicate_webhook` and `reordered_events` are listed but disabled.
- **Settings:** workspace, providers and appearance are read-only or local. Operators also get a sandbox API key generator (name, role, expiry; the secret is shown once; list and two-step revoke, ADR 0011). Member roles and live activation appear as unavailable capabilities, not controls.
- **Dashboard hub and API reference:** `/dashboard` is the hub drawn in section 2 and `/dashboard/api-reference` the Swagger page in section 10a; the sidebar gains an "API reference" item. Nothing is minted or run on page load. The hub's demo buttons run real simulations and are labelled Simulated. Swagger UI internals are third-party markup, so their accessibility is not asserted by the sweep (ADR 0012). The hub and demo cards are verified against the code at the time of writing; the metrics section below them is the previous overview content (including the Recent payments table). The provider-distribution chart drawn in the earlier overview wireframe was never built.
- **Not compared line by line:** the providers, ledger, reconciliation, events and docs layouts and the shared loading, empty and error states. The Playwright specs and the 375/1440 px accessibility sweep (`e2e/ui/a11y-routes.spec.ts`) cover their behavior, not their resemblance to these drawings.

## Shared desktop and mobile shells

Desktop target: 1440 pixels. Content uses a bounded readable width, a persistent sidebar, and stacked sections when space becomes tight.

```text
+----------------+----------------------------------------------------------+
| RAILVANTA      | [Demo workspace v] [SANDBOX]       [Theme] [Account v]    |
|----------------+----------------------------------------------------------|
| Overview       | Breadcrumb / Page title                  [Primary action]|
| Playground     | Description + data source + last updated                 |
| Payments       |                                                          |
| Routing        |                                                          |
| Providers      | Page content                                             |
| Simulator      |                                                          |
| Ledger         |                                                          |
| Reconciliation |                                                          |
| Events         |                                                          |
| API reference  |                                                          |
| Settings       |                                                          |
|----------------|                                                          |
| API docs       |                                                          |
+----------------+----------------------------------------------------------+
```

Mobile target: 375 pixels. Navigation opens a modal drawer; page actions remain reachable without horizontal page scrolling. Tables use a labeled scroll region or summary cards with a full detail link.

```text
+-----------------------------------+
| [Menu] RAILVANTA  [SANDBOX] [User] |
| Demo workspace                 v  |
|-----------------------------------|
| Page title                        |
| [Primary action]                  |
| Source / updated time             |
| [Filters: 2 active]                |
|                                   |
| Card / chart / row summary         |
|                                   |
| [View details]                    |
+-----------------------------------+
```

Drawer: workspace, every navigation destination, theme, and close button. Opening moves focus into the drawer; Escape closes it and restores focus. Do not hide sandbox status or financial state labels at any viewport width.

## 1. Public landing page — `/`

```text
+--------------------------------------------------------------------------+
| RAILVANTA                       Features  API docs  Architecture  GitHub  |
|--------------------------------------------------------------------------|
| Payments, intelligently routed.                                          |
| One API. Every rail. Complete control.                                    |
| [Explore sandbox]  [View architecture]                                    |
|                                                                          |
| Browser -> API -> Router -> Mock provider -> Journal                       |
|                            Stripe / Plaid: planned sandbox adapters       |
|                                                                          |
| [Idempotency]       [Routing]       [Recovery]       [Reconciliation]       |
| [Copy API example]                    Sandbox only / no real funds        |
+--------------------------------------------------------------------------+
```

Mobile: stack hero, calls to action, vertical architecture, capability cards, and code sample. Architecture links to this documentation set. Only show a GitHub link once its real destination is configured. Distinguish planned capabilities from implemented ones.

## 2. Dashboard hub — `/dashboard`

The dashboard home is a hub and is treated like the main page of the product. A presenter or reviewer can obtain a key, start a real simulation and open the API reference from here without visiting Settings, the Playground or `/docs` first. Nothing is created on page load: no key is minted and no simulation runs until a button is clicked.

Desktop (1440 px):

```text
+--------------------------------------------------------------------------+
| HOME                                                                     |
| Railvanta sandbox                                           [SANDBOX]    |
| Try the API, run a failure demo and read the contract from one place.     |
|--------------------------------------------------------------------------|
| [Run a demo]  [Create a $100 payment]  [API reference (Swagger)]          |
| [Generate API key]                                                        |
|--------------------------------------------------------------------------|
| Integration credentials  #integration-credentials | Run a demo now        |
|                                                   | Simulated / sandbox   |
| Base URL  https://host/v1                 [Copy]  | [Lost response]       |
| Auth      Authorization: Bearer <key>             | [Duplicate requests]  |
| 3 of 20 active keys used                          | [Double-entry ledger] |
| curl -H "Authorization: Bearer $KEY" ...          |                       |
| [Generate default demo key]                       | Each button runs a    |
|                                                   | real $100 simulation. |
| One-time panel after the click:                   | Result:               |
| Key: oper_ab12.****  [Copy key]                   | 6 of 6 invariants     |
| curl with the real key  [Copy]                    | passed, payment       |
| Shown once. [I have copied it, hide the key]      | succeeded             |
|                                                   | [Open in Playground]  |
|--------------------------------------------------------------------------|
| Browser -> API -> Accept once -> Route -> Mock provider -> Journal        |
| [Available] each step            Stripe and Plaid adapters: [Planned]     |
|--------------------------------------------------------------------------|
| Live sandbox metrics            Time range [Last 24 hours v]              |
| Needs attention: 1 unknown outcome                  [Inspect payment]     |
| [Volume by currency] [Success rate] [Payment count] [Observed p95]         |
| Payment volume                                                             |
| [Line chart + table alternative]                                           |
| Recent payments (5)                              [View all payments]       |
+--------------------------------------------------------------------------+
```

Mobile (375 px): one column, no horizontal page scroll.

```text
+-----------------------------------+
| HOME                    [SANDBOX] |
| Railvanta sandbox                 |
| Sub-line                          |
|-----------------------------------|
| [Run a demo]  [Create a $100]     |
| [API reference] [Generate API key]|
|-----------------------------------|
| Integration credentials           |
| Base URL ................ [Copy]  |
| Authorization: Bearer <key>       |
| 3 of 20 active keys used          |
| curl snippet ($KEY placeholder)   |
| [Generate default demo key]       |
|-----------------------------------|
| Run a demo now (Simulated)        |
| [Lost response]                   |
| [Duplicate requests]              |
| [Double-entry ledger]             |
|-----------------------------------|
| Capability strip (vertical)       |
| Stripe and Plaid adapters: Planned|
|-----------------------------------|
| Live sandbox metrics              |
| Needs attention banner            |
| Metric cards (two per row)        |
| Volume chart                      |
| Recent payments                   |
+-----------------------------------+
```

Sections, top to bottom:

1. **Heading.** Eyebrow "Home", h1 "Railvanta sandbox", a one-line description and the Sandbox badge.
2. **Quick actions** (four, 2x2 on mobile): "Run a demo" goes to `/dashboard/playground`; "Create a $100 payment" opens the create-payment dialog prefilled with a sandbox mock method (disabled until payment methods load); "API reference (Swagger)" goes to `/dashboard/api-reference`; "Generate API key" scrolls to and focuses the credentials card (`#integration-credentials`).
3. **Two columns** (one on mobile). Left, **Integration credentials** (Sandbox badge): the base URL (this origin) with a copy button, the auth format `Authorization: Bearer <key>`, "N of 20 active keys used" (operators only; others see "Key counts need the operator role."), and a curl snippet with a `$KEY` placeholder. For operators the button **Generate default demo key** mints a key (operator role, 24-hour expiry, ADR 0011 rules and 20-key cap) through `POST /v1/api-keys`, with the note "Nothing is created until you click"; if an active "Default demo key" already exists a note says its secret cannot be shown again and that generating creates another. The one-time panel (shared with Settings) has a copy-key button, the curl with the real key and **I have copied it, hide the key**. A link points to Settings to manage and revoke keys. Right, **Run a demo now** (badge "Simulated / sandbox"): **Lost response** (`timeout_after_success`), **Duplicate requests** (`duplicate_request`) and **Double-entry ledger** (`duplicate_posting`) each start a real $100.00 USD simulation through `POST /v1/sandbox/simulations`; while it runs a live label shows progress, then a compact result: the payment status pill, "N of N invariants passed" and **Open in Playground**. One run at a time (all three buttons are disabled while a run is in progress).
4. **Capability strip** "How a payment flows" echoing the landing page flow (Browser, API, Accept once, Route, Mock provider, Journal), each step marked Available, and "Stripe and Plaid adapters: Planned".
5. **Live sandbox metrics** (the previous overview content, with a Time range select of 24 hours, 7 days or 30 days kept in the `window` URL parameter): a "Needs attention" banner for unknown outcomes, source and updated line, metric cards, the payment-volume chart with a text alternative, and a "Recent payments" table (five rows, "View all payments" link, "No payments yet" empty state). Never sum different currencies into one unqualified volume; "No observations" replaces missing latency statistics.

States:

| Area | Loading | Error | Read-only role |
| --- | --- | --- | --- |
| Integration credentials | "Checking your role..." and "Loading key count..."; base URL, auth format and the `$KEY` curl stay visible | Generate failure: inline alert with the message and request ID, no key shown, button re-enabled (409 `api_key_limit` reports the cap) | Note that generating keys requires the operator role (you are signed in as read only); no generate button; the rest remains |
| Run a demo now | Clicked run: buttons disabled and a live progress label; before scenarios load a button is disabled with "Checking available scenarios..." | Start failure: inline alert with message and request ID; unreadable result: "The run result could not be read."; a failed run shows "Run failed: <reason>" | The buttons are not role-gated in the UI; a read-only caller's start request is rejected by the API and shown as the inline alert |
| Disabled scenario | Not applicable | A button whose scenario is missing or not implemented is disabled with "Disabled: backend scenario not available." (or "not implemented yet") | Same |
| Live sandbox metrics | Skeleton cards and chart | Per-section message with request ID and Retry; stale data keeps its timestamp | Visible (reads are allowed) |

Empty state: with no payments the metrics show "No observations" and Recent payments shows "No payments yet"; the credentials and demo cards have no empty state of their own (0 of 20 is displayed as a count).

The secret is held only in component state: reloading or hiding the panel discards it, and it is never written to storage or the URL.

## 10. Developer docs — `/docs`

```text
+--------------------------------------------------------------------------+
| RAILVANTA / Developer documentation                         [Sandbox]     |
|--------------------------------------------------------------------------|
| Getting started | Create a sandbox payment                               |
| Authentication  | [cURL] [TypeScript] [Go]                               |
| Payments        | Example request / safe example response                |
| Idempotency     | [Copy example]                                          |
| Webhooks        |                                                         |
| Routing         | Retry safety / unknown outcomes / error codes           |
| Errors          | [Open API specification] once generated                 |
| Limitations     | Capture/refunds: planned unless implemented              |
+--------------------------------------------------------------------------+
```

Mobile: collapsible section navigation above content. Copy actions announce success/failure. Examples use placeholders and sandbox paths; never embed secrets. Separate supported endpoints from roadmap sections.

## 10a. API reference — `/dashboard/api-reference`

```text
+--------------------------------------------------------------------------+
| DEVELOPERS                                                                |
| API reference                                                             |
| The interactive OpenAPI reference for the Railvanta sandbox API.          |
|--------------------------------------------------------------------------|
| [Sandbox] Generate a sandbox API key in Settings, click Authorize, paste  |
| it, then Try it out. Sandbox only; no real funds.                         |
| Requests go to this origin through the same-origin /v1 proxy. Writes need |
| a bearer key (cookie writes need a CSRF header).                          |
|--------------------------------------------------------------------------|
| [Authorize]                                                               |
| payments     POST /v1/payments            v                               |
| routing      POST /v1/routing/evaluate    v                               |
| ...          (Swagger UI body, the real OpenAPI document)                 |
+--------------------------------------------------------------------------+
```

The page embeds Swagger UI (`swagger-ui-dist`) over `/openapi.json`, a same-origin copy of the contract (no external fetch). While it loads the page shows "Loading the API reference..."; if it fails an alert says the interactive reference could not load and links to the OpenAPI document. The Settings link goes to the API keys section. Mobile: one column; the Swagger UI body scrolls inside the page without horizontal page scroll. Swagger UI internals are third-party markup, so their accessibility is not asserted. The page is reached from the sidebar ("API reference"), the dashboard hub and `/docs`.

## 11. Settings — `/dashboard/settings`

```text
+--------------------------------------------------------------------------+
| Settings                                                                 |
| Appearance: [System] [Light] [Dark]                                      |
| Workspace: Demo workspace                Environment: Sandbox (fixed)     |
| Provider configuration: Mock enabled; Stripe/Plaid not configured          |
| API keys: generator (ADR 0011). Roles / live activation: unavailable    |
+--------------------------------------------------------------------------+
```

Mobile retains the same sections in one column. Theme preference is local UI state. Key generation is real (ADR 0011); do not display fake membership management or live activation controls.

## Shared loading, empty, and error states

| Condition | Presentation | Action |
| --- | --- | --- |
| Initial read | Skeleton matching final layout; loading label | No duplicate query-trigger buttons |
| No records | “No sandbox payments yet” or screen-specific text | Create a sandbox payment when supported |
| Filters exclude results | “No payments match these filters” | Clear filters |
| Read failure | Safe message and request identifier | Retry safe read; retain filters |
| Stale cached result | Preserve last data; show stale timestamp and refresh error | Retry refresh |
| Unauthorized / forbidden | Explain session or access issue without leaking records | Sign in / return to allowed workspace |
| Mutation pending | Disabled submit; operation-specific progress text | Wait or inspect status |
| Unknown payment result | Persistent “Outcome unknown” explanation | Inspect/reconcile using backend contract |
| Unsupported capability | Explicit unavailable state and dependency | No simulated success action |

Verify keyboard order, focus restoration, contrast, text equivalents, reduced motion, and error announcements before each screen is considered complete.
