# Frontend Wireframes

Low-fidelity layouts per route. IDs, amounts, times and metrics are illustrative; the real app shows only API data or explicitly labeled seeded/simulated data. Routes, API dependencies and accessibility rules are in [frontend design](design.md).

## Implementation status

All routes below exist in `frontend/`, as does `/dashboard/playground` (see the [demo runbook](../demo/README.md)). The wireframes remain design intent, not a specification of pixels or copy. Differences found by comparing them with the code (features, not a pixel review):

- **Shell:** there is no workspace switcher (the topbar shows a fixed "Sandbox workspace" label and the Sandbox badge), no notification control, and the account menu is a static panel with no actions. A "Sandbox build / Sandbox infrastructure. No real funds." note sits in the sidebar. See [ADR 0010](../decisions/0010-original-frontend-components.md).
- **Icons:** navigation uses Unicode glyphs, not an icon set.
- **Landing page:** section 1 was redesigned on 2026-11-01 and is **not implemented yet**; the live page still has the previous hero, lifecycle, why, API, architecture and capabilities sections. No GitHub link (no destination is configured).
- **Payments:** filters and cursor live in the URL as designed. The detail page has Attempts, Events and Journal tabs. There is **no cancel control** and **no recovery panel**, although `POST /v1/payments/{id}/cancel` and `GET /v1/payments/{id}/recovery` exist in the API.
- **Routing:** rules are inspect-only, as drawn; there is no rule editor.
- **Simulator:** `duplicate_webhook` and `reordered_events` are listed but disabled.
- **Settings:** workspace, providers and appearance are read-only or local. Operators also get a sandbox API key generator (name, role, expiry; the secret is shown once; list and two-step revoke, ADR 0011). Member roles and live activation appear as unavailable capabilities, not controls.
- **Dashboard hub and API reference:** `/dashboard` is the hub drawn in section 2 and `/dashboard/api-reference` the Swagger page in section 10a; the sidebar gains an "API reference" item. Nothing is minted or run on page load. The hub's demo buttons run real simulations and are labelled Simulated. Swagger UI internals are third-party markup, so their accessibility is not asserted by the sweep (ADR 0012). The hub and demo cards are verified against the code at the time of writing; the metrics section below them is the previous overview content (including the Recent payments table). The provider-distribution chart drawn in the earlier overview wireframe was never built.
- **Default demos:** every dashboard page carries the shared Default demo card described in the "Default demo panel" section below, and the hub has a "Load sample data" card (ADR 0013). Nothing runs or mints on page load; the one exception is the Routing page's single read-only evaluation. The landing page's one-step "Start sandbox" flow needs `SANDBOX_PUBLIC_ACCESS` (on in the Compose files, off elsewhere).
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

Redesigned on 2026-11-01. This replaces the earlier landing wireframe and the separate "Why Railvanta exists" addendum. Status: **wireframe only, not implemented yet**. The live page still follows the previous layout until this is built.

### 1.1 Design brief

| | |
| --- | --- |
| **Who lands here** | (1) A reviewer or hiring manager opening a portfolio link: 30–60 seconds of attention, may never scroll. (2) An engineer who wants to see whether it is real: will try the API and read the decisions. (3) The presenter during a live demo, who needs a clean narrative to talk over. |
| **Job of the page** | In 5 s: what this is, and that it is a sandbox. In 30 s: which problem it solves and why that problem is hard. In 2 min: proof, meaning a real API call and real evidence, not claims. |
| **One-sentence message** | Payment systems fail in the gap between "we sent it" and "we know what happened"; Railvanta makes each of those failures reproducible and shows the guarantee that handles it. |
| **Primary action** | Start a sandbox (session, one-time key, a real `POST /v1/payments`). **Secondary:** open the dashboard, already populated with sample runs. |
| **Success signal** | A first-time visitor reaches a 202 response or the hub story panel without reading docs. |
| **Hard constraints** | Honest scope (mock provider, no real money, real providers not built); nothing is created on page load; WCAG 2.2 AA; no horizontal scroll at 375 px; three.js loaded lazily and never required for meaning. |

### 1.2 Audit of the current page (why it is redesigned)

1. **The headline contradicts the product.** "Payments, intelligently routed. One API. Every rail. Complete control." is a generic marketing claim, and "every rail" is not true: there is one mock provider plus simulated profiles. The real story ("payments fail in the gaps") is in the second section, below the fold.
2. **The primary action is diluted.** The hero has three calls to action (Start sandbox, Explore the dashboard, Open API reference), the header has a fourth (Explore sandbox), and Start sandbox appears again further down. The visitor has no single obvious next step.
3. **Content is duplicated.** The lifecycle visual and the "Architecture" flow strip show the same seven steps twice. The "Capabilities" cards repeat the problem cards in feature language and lead with "Planned" badges, which advertises what is missing.
4. **The order is mechanism first, evidence last.** The page explains how before it earns attention with why, and it never shows proof that the guarantees hold, although the sandbox has that proof (seeded sample runs with passing invariants).
5. **It is long and flat.** Seven stacked sections of similar visual weight give no rhythm and no clear end.

### 1.3 Principles

- **Problem → proof → try → depth.** Each section answers the question the previous one raises.
- **One primary action per viewport.** Start sandbox is the only filled button on the page; everything else is a quiet button or a link.
- **Show evidence, not adjectives.** Every guarantee links to the run or page that proves it; numbers are only shown when the backend produced them.
- **Scope honesty is a feature.** "What is real and what is simulated" gets its own small section instead of scattered Planned badges.
- **Motion is optional.** The three.js lifecycle illustrates; the ordered list carries the meaning.

### 1.4 Information architecture (top to bottom)

| # | Section | Question it answers | Replaces |
| --- | --- | --- | --- |
| 0 | Header | Where am I, where can I go? | Same; drops the duplicate CTA |
| 1 | Hero: problem headline + lifecycle visual | What is this, and why should I care? | Hero + lifecycle section |
| 2 | Scope strip | Is this real money? What is real? | Sandbox notice + Planned badges |
| 3 | Failure modes ("The gaps") | Which problems exactly, and what is the guarantee? | "Why Railvanta exists" |
| 4 | Try it in one step | Can I verify it myself, now? | "Call the API in one step" |
| 5 | Engineering decisions | How is it built, and why this way? | Architecture strip + Capabilities |
| 6 | Closing CTA + footer | What next? | Footer |

### 1.5 Desktop (1440 px, content max-width 1120 px)

```text
+--------------------------------------------------------------------------------+
| ◇ Railvanta                  Docs   API reference         [Open dashboard →]    |  quiet button
|================================================================================|
| SANDBOX PAYMENT ORCHESTRATION · PORTFOLIO PROJECT          (eyebrow)           |
|                                                                                |
| Payments fail in the gaps.                 +---------------------------------+ |
| Railvanta makes the gaps visible.  (h1)    |  three.js lifecycle (aria-hidden)| |
|                                            |  Client→API→Outbox→Router→       | |
| A sandbox orchestration API that           |  Provider→Ledger→Reconciliation  | |
| reproduces retries, lost responses,        |  token travels the path          | |
| outages and duplicate events, and          |                                  | |
| proves each payment is charged at most     |  [Happy path][Lost response]     | |
| once and recorded exactly once.            |  [Duplicate request]  (segmented)| |
|                                            |  Caption: "Response lost; parked | |
| [Start sandbox →]  Open the dashboard →    |  as unknown; looked up at the    | |
|  (only filled btn)  (text link)            |  SAME provider; never a second." | |
|                                            +---------------------------------+ |
|                                            ▸ Show the steps as text (details) |
|--------------------------------------------------------------------------------|
| ● Mock provider only — no real money, no card or bank data   ● Real: API,      |
|   idempotency, outbox, ledger, reconciliation   ○ Not built: real providers  |  scope strip
|--------------------------------------------------------------------------------|
| THE GAPS                                                                       |
| Six ways a payment goes wrong, and what Railvanta guarantees.          (h2)    |
|                                                                                |
|  Failure                      Guarantee                          Evidence       |
|  ─────────────────────────────────────────────────────────────────────────────  |
|  01 A retry charges twice     One Idempotency-Key: 1 accept,     Duplicate     →|
|                               9 replays, changed body → 409      request run    |
|  02 Response lost after       Marked unknown, not failed;        Lost response →|
|     the charge                resolved by lookup at SAME provider                |
|  03 Provider outage           Stays unknown; never failed over   Outage run    →|
|  04 Webhook delivered 3–5×    Inbox keyed by event id; one effect Webhook run  →|
|  05 Ledger posted twice       Posting key: exactly one balanced  Journal       →|
|                               journal                                           |
|  06 Provider disagrees        Discrepancy recorded with evidence; Reconciliation→|
|                               no ledger row changed                             |
|  (rows expand on click to "What goes wrong" detail; table on desktop)          |
|--------------------------------------------------------------------------------|
| TRY IT IN ONE STEP                                                             |
| +----------------------------------+  +--------------------------------------+ |
| | 1  Start a sandbox session       |  | $ curl -X POST .../v1/payments \     | |
| | 2  Copy your one-time key        |  |   -H "Authorization: Bearer rvk_…" \ | |
| | 3  Run it — then run it again    |  |   -H "Idempotency-Key: landing-…"    | |
| |    with the same key             |  |                          [Copy]      | |
| |                                  |  |--------------------------------------| |
| | [Start sandbox]  (anchor target  |  | HTTP 202  { "id": "…", "status": … } | |
| |  of the hero button)             |  | Replay: same body, no second payment | |
| +----------------------------------+  +--------------------------------------+ |
|  [Run it] [Run it again]   Open this payment in the dashboard →                 |
|--------------------------------------------------------------------------------|
| ENGINEERING DECISIONS                                                          |
| +-----------------+ +-----------------+ +-----------------+ +-----------------+ |
| | Outbox + leases | | Unknown ≠ failed| | Double-entry    | | Evidence-only   | |
| | Provider I/O    | | Ambiguous charge| | ledger, posting | | reconciliation  | |
| | outside the DB  | | never retried   | | keys            | | never edits the | |
| | transaction     | | elsewhere       | |                 | | books           | |
| | ADR →           | | ADR →           | | ADR →           | | ADR →           | |
| +-----------------+ +-----------------+ +-----------------+ +-----------------+ |
|  Go · GORM · SQLite · Next.js · Playwright        Architecture doc →            |
|--------------------------------------------------------------------------------|
| See every failure, already run.   [Open the dashboard →]  (sample data ready)  |
| Railvanta · Sandbox only / No real funds · Docs · API reference                 |
+--------------------------------------------------------------------------------+
```

Above the fold at 1440×900: the header, the whole hero with the lifecycle visual and its controls, and the scope strip. Everything a 30-second reviewer needs.

### 1.6 Mobile (375 px, 16 px gutters, single column)

```text
+-----------------------------------+
| ◇ Railvanta          [Dashboard]  |
|-----------------------------------|
| SANDBOX PAYMENT ORCHESTRATION     |
| Payments fail in the gaps.        |
| Railvanta makes the gaps visible. |
| Short description (3 lines)       |
| [Start sandbox →]  (full width)   |
| Open the dashboard →              |
| +-------------------------------+ |
| | lifecycle canvas (16:10)      | |
| +-------------------------------+ |
| [Happy][Lost resp.][Duplicate]    |  wraps, 44px targets
| Caption                           |
|-----------------------------------|
| ● Mock provider · no real money   |
|-----------------------------------|
| THE GAPS                          |
| 01 A retry charges twice      [+] |  disclosure rows:
|    One key: 1 accept, 9 replays   |  failure + guarantee visible,
|    Evidence →                     |  detail on expand
| 02 ...                            |
|-----------------------------------|
| TRY IT IN ONE STEP                |
| 1·2·3 steps                       |
| [Start sandbox]                   |
| curl (scrolls inside its block)   |
| [Run it] [Run it again]           |
| response                          |
|-----------------------------------|
| ENGINEERING DECISIONS (stacked)   |
| Closing CTA · footer              |
+-----------------------------------+
```

### 1.7 Behaviour and states

- **Hero Start sandbox** scrolls to "Try it in one step" and starts the session there (one implementation, two entry points). The focus moves to the step list, and the result is announced with `role="status"`.
- **Try it, states:**
  - Checking: the button is disabled with "Checking sandbox access…".
  - Public access off: the existing off message and a static curl with placeholders.
  - Starting: the button is disabled.
  - Error: `role="alert"` plus Try again.
  - Started: the key is shown once and lives only in component state. The curl uses the real key and method. Run it sends the bearer key, not the cookie. Run it again replays the same Idempotency-Key. Afterwards "Open this payment in the dashboard" deep-links to `/dashboard/payments/<id>`.
- **Lifecycle:** this keeps the current rules for `aria-hidden` canvas, reduced motion (static frame), off-screen and hidden-tab pause, the WebGL fallback (SVG/CSS) and the three use cases. The seven-step text list moves into a `details` element ("Show the steps as text"), so it no longer competes visually but stays one click and one screen-reader landmark away. Height is fixed (`aspect-ratio`), so no layout shift occurs when three.js loads.
- **The gaps:** content comes from `frontend/lib/why-story.ts` (shared with the hub story panel). On desktop it is a semantic `table` (caption "Failure modes and guarantees") or a `dl` grid; on mobile, disclosure rows. Evidence links go to the dashboard page that shows the proof. The dashboard auto-starts a sandbox session (SandboxSessionGuard), so the links work for a first-time visitor.
- **Engineering decisions:** four cards. Each has a one-line decision, a one-line reason and a link to its ADR in `docs/decisions/`. There are no Available/Planned badges; scope lives in the scope strip.
- **Closing CTA:** one sentence and one button, ending the page with the same next step as the hero.

### 1.8 Visual system

- **Type scale:** h1 56/60 desktop and 36/40 mobile; h2 32; body 17/28; eyebrow 12 uppercase tracking 0.12em. The h1 is smaller than today's 80 px so the visual fits beside it.
- **Rhythm:** sections are separated by 96 px (desktop) and 56 px (mobile); the content max-width is 1120 px with the hero as a 6/6 two-column grid.
- **Colour:** existing tokens only. The accent is used for the single primary button, the active use case and the evidence links, and nothing else. The scope strip uses the existing notice tokens, never warning red.
- **Density:** at most one card style per section. The gaps use a table rather than six heavy cards, which reduces visual weight by half.

### 1.9 What is removed, and why

| Removed | Reason |
| --- | --- |
| "Payments, intelligently routed. One API. Every rail. Complete control." | Inaccurate ("every rail") and says nothing about the problem. |
| Header "Explore sandbox" button | A second primary action; replaced by a quiet "Open dashboard". |
| "Architecture" flow strip | Duplicates the lifecycle's seven steps. |
| "Capabilities" grid with Planned badges | Repeats the gaps in feature language; scope moves to the scope strip. |
| Hero "Open API reference" button | Kept as a header link; three hero CTAs dilute the primary action. |

### 1.10 Acceptance criteria (for implementation)

1. At 1440×900 the h1, the primary button, the lifecycle visual and the scope strip are visible without scrolling. At 375 px the h1 and the primary button are visible without scrolling.
2. Exactly one filled (primary) button is visible in each viewport-height band of the page.
3. No layout shift from the canvas (reserved aspect ratio); Lighthouse CLS under 0.05 on the landing page.
4. axe: no serious or critical violations; heading order h1 → h2 → h3; every evidence link has a unique accessible name.
5. With JavaScript or WebGL unavailable, every message on the page is still readable (the text steps, table and scope strip are server-rendered).
6. No session, key or payment is created until a button is clicked.
7. **Test impact:** `e2e/ui/shell.spec.ts` and `e2e/ui/landing.spec.ts` assert the old heading text ("Payments,") and a "Planned" badge, and `frontend/tests/components/landing.test.tsx` and `why-story.test.tsx` assert the current sections. These are updated in the same change.

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

### 2a. Story panel "The story: what this sandbox proves" (hub, right after the heading)

Reads `GET /v1/sandbox/samples` (the latest run per `sample-<scenario>` seed, seeded at startup when `SANDBOX_SAMPLE_DATA` is on) and never starts a run. One row per problem from `frontend/lib/why-story.ts`.

Desktop:

```text
+--------------------------------------------------------------------------+
| The story: what this sandbox proves (h2)           [Simulated / sandbox] |
| Payment systems fail in the gaps between "we sent it" and "we know what  |
| happened". Each row is a failure, its guarantee and a sample run.        |
|--------------------------------------------------------------------------|
| A retry charges the customer twice (h3)      | [✓ Completed] 6/6 inv.    |
| Every create carries an Idempotency-Key ...  | Inspect run   Payment     |
|----------------------------------------------+---------------------------|
| The provider charged, but the response ...   | [◔ Running]               |
| Marked unknown, resolved by lookup at ...    | Inspect run   Payment     |
|----------------------------------------------+---------------------------|
| The provider goes down mid-payment           | [✕ Failed] 2/4 inv. pass  |
| ...                                          | Inspect run               |
|----------------------------------------------+---------------------------|
| The same webhook arrives again and again     | [○ Not loaded]            |
| ...                                          | Events                    |
| ... ledger double posting, settlement mismatch                           |
+--------------------------------------------------------------------------+
```

Mobile (375 px): each row stacks the text above its status and links.

```text
+-----------------------------------+
| The story: what this sandbox      |
| proves            [Simulated]     |
| Framing line                      |
|-----------------------------------|
| A retry charges the customer twice|
| guarantee                         |
| [✓ Completed] 6/6 invariants pass |
| Inspect run   Payment             |
|-----------------------------------|
| ... five more rows ...            |
+-----------------------------------+
```

States:

- **Status pill:** Completed (with "n/m invariants pass"), Failed (with the count), Running, or Not loaded (`missing`). Icon plus text, never colour alone.
- **Links:** "Inspect run" → `/dashboard/simulator?run=<run_id>` and "Payment" → `/dashboard/payments/<payment_id>` only when present; a row without a run links to the page that shows the evidence instead.
- **All missing:** note "No sample runs on this server yet. Use Load sample data below to run every scenario once." linking to `#hub-sample-data` (the Load sample data card, which now uses the same `sample-<scenario>` seeds).
- **Running:** "Sample runs in progress… refreshing every few seconds." and the query refetches every 3 s; polling stops when no row is running.
- **Loading / error:** "Loading sample runs…"; on error a note that sample status is unavailable while the guarantees still show. The query key `["railvanta", ws, env, "sandbox", "samples"]` is refreshed when Load sample data finishes.

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

## Default demo panel (shared card)

One component, `frontend/components/demo/DefaultDemo.tsx`, used on every dashboard page. A click starts a real $100.00 USD sandbox simulation (the same backend scenarios as the Playground) and refreshes the page's lists when the run settles. Nothing runs on page load.

```text
+--------------------------------------------------------------------------+
| Default demo                                   [Simulated / sandbox]     |
| One or two sentences: what the run does and why it matters on this page. |
| [Run the default demo]   Creates a posted journal.                        |
|   (button reads "Running..." while pending; a disabled button explains    |
|    why, e.g. "Disabled: backend scenario not available.")                 |
| Live status: Payment unknown, waiting for the provider lookup...          |
|--------------------------------------------------------------------------|
| Result:  [Succeeded]  6 of 6 invariants passed       Open in Playground  |
| Error:   message  Request ID: req_...    (role="alert")                   |
+--------------------------------------------------------------------------+
```

The result appears only after the run settles. A failed run shows "Run failed: reason". Completion is announced through a polite live region, and the live status line is not re-announced on every poll. Placement and preconfiguration per page:

| Page | Placement | Scenario and preconfiguration |
| --- | --- | --- |
| Payments | Above the filters | `success`; the list refreshes with the new payment |
| Routing | Above the evaluate form | Outage demo (the default); a routing-demo method is preselected and one read-only evaluation runs on first load; after the demo the payment id is prefilled so "Unknown outcome: failover blocked" appears |
| Providers | Above the provider list | `success`; the health figures update |
| Simulator | Above the scenario form | The default scenario is preselected; a **Run default demo** button next to the form |
| Ledger | Above the journals | `duplicate_posting`; one balanced journal appears |
| Reconciliation | Above the runs | `settlement_mismatch` (takes about 6 seconds); a discrepancy appears |
| Events | Above the event list | `duplicate_webhook`; inbox and outbox rows appear |
| Settings | In the API keys section | **Generate default demo key** shortcut; the secret is shown once from component state |
| Hub | "Load sample data" card | Runs the 8 headline scenarios one after another (about 20 seconds); progress per scenario |

Mobile: the card is one column and the button spans the width. The unknown-outcome wait (about 6 seconds) is intentional and visible in the live status.

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
