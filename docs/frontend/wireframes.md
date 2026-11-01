# Frontend Wireframes

Low-fidelity layouts per route. IDs, amounts, times and metrics are illustrative; the real app shows only API data or explicitly labeled seeded/simulated data. Routes, API dependencies and accessibility rules are in [frontend design](design.md).

## Implementation status

All eleven routes below exist in `frontend/`. The wireframes remain design intent, not a specification of pixels or copy. Differences found by comparing them with the code (features, not a pixel review):

- **Shell:** there is no workspace switcher (the topbar shows a fixed "Sandbox workspace" label and the Sandbox badge), no notification control, and the account menu is a static panel with no actions. A "Sandbox build / Sandbox infrastructure. No real funds." note sits in the sidebar. See [ADR 0010](../decisions/0010-original-frontend-components.md).
- **Icons:** navigation uses Unicode glyphs, not an icon set.
- **Landing page:** no GitHub link (no destination is configured); Stripe and Plaid are shown as planned and not configured.
- **Payments:** filters and cursor live in the URL as designed. The detail page has Attempts, Events and Journal tabs. There is **no cancel control** and **no recovery panel**, although `POST /v1/payments/{id}/cancel` and `GET /v1/payments/{id}/recovery` exist in the API.
- **Routing:** rules are inspect-only, as drawn; there is no rule editor.
- **Simulator:** `duplicate_webhook` and `reordered_events` are listed but disabled.
- **Settings:** workspace, providers and appearance are read-only or local. Operators also get a sandbox API key generator (name, role, expiry; the secret is shown once; list and two-step revoke, ADR 0011). Member roles and live activation appear as unavailable capabilities, not controls.
- **Not compared line by line:** the overview, providers, ledger, reconciliation, events and docs layouts and the shared loading, empty and error states. The Playwright specs and the 375/1440 px accessibility sweep (`e2e/ui/a11y-routes.spec.ts`) cover their behavior, not their resemblance to these drawings.

## Shared desktop and mobile shells

Desktop target: 1440 pixels. Content uses a bounded readable width, a persistent sidebar, and stacked sections when space becomes tight.

```text
+----------------+----------------------------------------------------------+
| RAILVANTA      | [Demo workspace v] [SANDBOX]       [Theme] [Account v]    |
|----------------+----------------------------------------------------------|
| Overview       | Breadcrumb / Page title                  [Primary action]|
| Payments       | Description + data source + last updated                 |
| Routing        |                                                          |
| Providers      | Page content                                             |
| Simulator      |                                                          |
| Ledger         |                                                          |
| Reconciliation |                                                          |
| Events         |                                                          |
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

## 2. Overview — `/dashboard`

```text
+--------------------------------------------------------------------------+
| Overview                                            [Last 24 hours v]    |
| Simulated sandbox data / Updated 14:30 UTC                                |
| [Volume by currency] [Success rate] [Payment count] [Observed p95]         |
|--------------------------------------------------------------------------|
| Payment volume                         | Provider distribution            |
| [Line chart + table alternative]       | [Chart + text counts]            |
|--------------------------------------------------------------------------|
| Attention needed: 1 unknown outcome                 [Inspect payment]    |
| Recent payments                                                          |
| Payment      Amount       Rail       Provider       Status                |
| pay_demo_1   USD 250.00   Card       Mock           Outcome unknown       |
|                                                     [View all payments]  |
+--------------------------------------------------------------------------+
```

Mobile: two metric cards per row, then full-width charts, attention banner, and payment cards. Never sum different currencies into one unqualified volume. “No observations” replaces missing latency statistics.

## 3. Payments — `/dashboard/payments`

```text
+--------------------------------------------------------------------------+
| Payments                                         [Create sandbox payment]|
| [Search ID/reference] [Status v] [Rail v] [Provider v] [Date] [Amount]    |
| [Clear filters]                       Sort: [Created descending v]       |
|--------------------------------------------------------------------------|
| Payment       Created        Amount       Rail     Provider   Status      |
| pay_demo_1    14:30 UTC       USD 250.00   Card     Mock       Unknown  > |
|--------------------------------------------------------------------------|
| [Previous]                                               [Next page]     |
+--------------------------------------------------------------------------+
```

Creation opens a labeled dialog: amount, currency, rail, an existing sandbox payment-method reference, optional reference, sandbox notice, field errors, and `[Create sandbox payment]`. The initial mock demo may preselect a seeded method; its opaque ID is still required in the request. Do not collect card numbers or banking credentials. The backend validates the method's workspace and provider compatibility, then selects an eligible provider. Submission shows pending state and retains the idempotency key; uncertain outcomes lead to inspection, not a second payment.

Detail route: `/dashboard/payments/[id]`.

```text
+--------------------------------------------------------------------------+
| < Payments       pay_demo_1                  [Copy ID] [Outcome unknown] |
| USD 250.00 / Card / Mock / SANDBOX                                        |
| [Summary] [Attempts] [Events] [Journal]                                   |
|--------------------------------------------------------------------------|
| State history                 | Selected attempt                         |
| Created -> Processing         | Provider reference: mock_demo_1           |
|          -> Outcome unknown   | Result: timeout; execution uncertain      |
|                               | Next: provider status reconciliation     |
|--------------------------------------------------------------------------|
| Masked idempotency metadata    Correlation ID      [View related events] |
+--------------------------------------------------------------------------+
```

Mobile: summary, status explanation, then vertically stacked sections. Detail links preserve list filters. Raw credentials and complete idempotency keys are absent.

## 4. Routing — `/dashboard/routing`

```text
+--------------------------------------------------------------------------+
| Routing                                       Rules: inspection only     |
| Priority    Condition           Eligible provider    Enabled              |
| 1           Sandbox / Card      Mock                 Yes                  |
| Stripe and Plaid integrations: not configured                             |
|--------------------------------------------------------------------------|
| Evaluate without creating a payment                                      |
| [Amount] [Currency v] [Rail v]                [Evaluate routing]          |
| Request -> Matching rule -> Capability check -> Mock                      |
| Explanation: selected configured sandbox provider                        |
+--------------------------------------------------------------------------+
```

Mobile: rule cards above evaluator. Evaluation uses backend validation and has no money-moving effect. Rule creation/reordering remains disabled until a mutation contract exists. An uncertain prior attempt blocks unsafe failover and explains why.

## 5. Providers — `/dashboard/providers`

```text
+--------------------------------------------------------------------------+
| Providers                  Observation window [24 hours v]               |
| [Mock: simulated]        [Stripe: not configured] [Plaid: not configured]|
| [Capabilities]           No observations         No observations         |
| [Success / p95 / errors]                                                 |
| [View details]                                                          |
|--------------------------------------------------------------------------|
| Selected provider detail: /dashboard/providers/[id]                       |
| Capabilities | health observations | recent safe diagnostics              |
| Last checked timestamp | Circuit state: unavailable unless implemented   |
+--------------------------------------------------------------------------+
```

Mobile: one provider card per row; detail sections stack. “Not configured,” “No observations,” and “Unavailable” describe different conditions. No green health badge without evidence.

## 6. Simulator — `/dashboard/simulator`

```text
+--------------------------------------------------------------------------+
| Payment simulator                         [SIMULATED / SANDBOX ONLY]     |
|--------------------------------------------------------------------------|
| Configuration                       | Execution timeline                 |
| Amount [250.00] Currency [USD v]    | 1 Request accepted                  |
| Rail [Card v] Provider [Mock]      | 2 Idempotency recorded              |
| Scenario [Timeout after success v] | 3 Attempt persisted                 |
| [Run simulation]                   | 4 Provider outcome uncertain        |
|                                    | 5 Reconciliation pending            |
|--------------------------------------------------------------------------|
| [Request] [Response] [Routing] [Attempts] [Events] [Journal] [Invariants]  |
| Backend-derived result; inspect current state and correlation ID          |
+--------------------------------------------------------------------------+
```

Provide deterministic scenarios: success, decline, timeout before execution, timeout after success, duplicate request, duplicate webhook, reordered events, outage, unknown-outcome recovery, and duplicate ledger-post attempt. Enable only scenarios implemented by the backend. Show invariant outcomes as pending until evidence arrives, then passed/failed with details.

Mobile: configuration above timeline; inspector tabs scroll within a labeled region. Announce run acceptance once. Poll the backend while active and expose a manual refresh; do not animate invented progress or promise background recovery while the backend is unavailable.

```text
+-----------------------------------+
| [Menu] Simulator       [SANDBOX]   |
| Simulated mock-provider execution |
| Amount [250.00]                   |
| Currency [USD v] Rail [Card v]    |
| Scenario [Timeout after success v]|
| [Run simulation]                  |
|-----------------------------------|
| Timeline                          |
| 1 Request accepted                |
| 2 Attempt submitted               |
| 3 Outcome unknown                 |
| Awaiting reconciliation           |
| [Refresh status]                  |
|-----------------------------------|
| [Request] [Response] [Events] ->   |
| Selected inspector content        |
+-----------------------------------+
```

## 7. Ledger — `/dashboard/ledger`

```text
+--------------------------------------------------------------------------+
| Ledger                                      [Currency v] [Payment ID]   |
| [Accounts] [Journal transactions]                                        |
| Journal       Payment        Currency       Debit        Credit          |
| jrn_demo_1    pay_demo_1      USD            250.00       250.00        > |
|--------------------------------------------------------------------------|
| Detail: /dashboard/ledger/[id]              [POSTED / IMMUTABLE]          |
| Account                   Side             Amount                        |
| Provider clearing         Debit            USD 250.00                    |
| Merchant payable          Credit           USD 250.00                    |
| Balance verification: debits equal credits                               |
+--------------------------------------------------------------------------+
```

Mobile: journal summaries with detail links, then readable entry cards. No edit/delete action for posted entries. Display reversals as linked new journals when implemented. Never aggregate balances across currencies.

## 8. Reconciliation — `/dashboard/reconciliation`

```text
+--------------------------------------------------------------------------+
| Reconciliation                              [Run sandbox reconciliation] |
| Run history: [Latest run v]         Status / started / finished           |
| [Matched] [Unmatched] [Amount mismatch] [Status mismatch]                 |
|--------------------------------------------------------------------------|
| Payment       Provider evidence        Difference        Resolution       |
| pay_demo_1    Confirmed success        Local unknown     Awaiting review >|
|--------------------------------------------------------------------------|
| Selected discrepancy: evidence -> resolution history -> related journal   |
+--------------------------------------------------------------------------+
```

The rerun control stays unavailable until an authorized, idempotent backend command exists. Once implemented, show confirmation, active run status, and duplicate-run handling. Mobile stacks counts and discrepancy cards; preserve exact amounts and explanatory text.

## 9. Events — `/dashboard/events`

```text
+--------------------------------------------------------------------------+
| Events                       [Payment / correlation ID] [State v] [Date] |
| [Refresh]       Source: simulated mock events / updated timestamp          |
| Event        Provider      Signature          Processing       Duplicate  |
| evt_demo_1   Mock          Not applicable     Applied          No       > |
|--------------------------------------------------------------------------|
| Event detail: safe fields | provider ID | attempts | related payment       |
| [Open trace] only when a configured trace destination exists              |
+--------------------------------------------------------------------------+
```

Mobile: event cards and full-page detail. Mock events must not claim cryptographic signature verification. Future external webhooks show verified/rejected status from backend processing, never a client inference. Redact sensitive payload fields and avoid rendering arbitrary JSON as HTML.

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

## 11. Settings — `/dashboard/settings`

```text
+--------------------------------------------------------------------------+
| Settings                                                                 |
| Appearance: [System] [Light] [Dark]                                      |
| Workspace: Demo workspace                Environment: Sandbox (fixed)     |
| Provider configuration: Mock enabled; Stripe/Plaid not configured          |
| API credentials / roles / live activation: unavailable in initial demo    |
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
