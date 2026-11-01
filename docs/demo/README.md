# Demo runbook: Payment Failure & Recovery Playground

The one page to read before presenting. Everything runs locally in a sandbox: a deterministic mock provider, one Go API with SQLite, a Next.js dashboard. No real funds move.

The Playground lives at `/dashboard/playground`. It has six cards, one per demo, each with a **Run demo N** button (demo 1 also has **Create $100 payment**, demo 3 also has **Contrast: provider outage**, demo 4 has an **Evaluate routing** form) and a **Run all in order** button at the top that runs demos 1, 2, 3, 5 and 6 and stops at the first failure. Every run is a real run against the real API and shows the payment state history, counts (attempts, charges, journals), and each invariant the backend checked with its evidence. Everything is badged **Simulated / sandbox**. Every Playground run is a **$100.00 USD** payment.

## Commands

```sh
pnpm demo:up          # start the stack (no build), create throwaway credentials, seed, print key + curl examples
pnpm demo:open        # headed Chromium, already signed in, opened at /dashboard/playground
pnpm demo:down        # stop (data kept); `pnpm demo:down -v` DELETES the demo data and credentials
DEMO_BUILD=1 pnpm demo:up   # only after code changes: builds first (slow, heavy on the laptop)
```

How it works:

- `docker-compose.demo.yml` is an override of `docker-compose.yml`: fast dispatcher polling, `MOCK_DELAYED_SUCCESS_DELAY=4s`, and `RECOVERY_RESOLVE_DELAY=6s` so the `unknown` state stays on screen long enough to explain. Measured on the real stack (2026-11-01): the payment is `unknown` from about 0.5 s to about 6.7 s after the run starts, then resolves to `succeeded`. If an image predates the knob the variable is ignored and `unknown` resolves in about 0.2 s. No extra ports; the proxy stays on `127.0.0.1:3000` (`RAILVANTA_PORT` changes it).
- Containers run with `DEMO_MODE` off and the session cookie is HttpOnly, so it cannot be typed into DevTools. `scripts/demo-up.sh` generates random keys and tokens with `openssl` into `.demo/credentials.env` (git-ignored) and runs `railvanta-api seed-demo` inside the API container; `scripts/demo-open.mjs` installs the same two cookies Playwright uses in `e2e/support/browser.ts` (`__Host-railvanta_session`, `railvanta_csrf`).
- Re-running `pnpm demo:up` reuses the stored file. If the volume is seeded but `.demo/credentials.env` is gone, seeding refuses; run `pnpm demo:down -v && pnpm demo:up`.

## Pre-demo checklist (15 minutes before)

1. Quit heavy apps (IDE indexers, other Docker stacks, browsers with many tabs). Docker Desktop running.
2. `pnpm demo:up`. Wait for the credentials banner.
3. `pnpm demo:open`. A Chromium window opens already signed in at `/dashboard/playground` (if the cards show an error with a request ID instead, see "If something breaks").
4. Click **Run demo 3** once and watch it: the payment badge goes Created, then Unknown for about 6 seconds, then Succeeded, and the card ends with "6 of 6 invariants passed". Confirms the stack, the timings and your cursor path. (Each run adds a payment, which is harmless.)
5. Optionally run the first curl example from the banner so you can show the 409 replay on request.
6. Have these tabs ready: this runbook, `docs/case-study.md`, the GitHub repo, a terminal in the repo root.

## 90-second script

| Time | Say | Do (Playground) |
| --- | --- | --- |
| 0:00-0:15 | "Railvanta is a sandbox payment orchestrator. The hard case is the request that times out after the provider already took the money: a naive retry charges twice. I built it so that ends with one charge and one balanced journal." | Playground open, nothing clicked. |
| 0:15-0:30 | "First the normal path: I create a $100 payment. Acceptance writes the payment and an outbox command in one transaction; a dispatcher then calls the provider." | Click **Run demo 1**. It finishes in about a second: created, processing, succeeded; 1 attempt, 1 charge, 1 journal. |
| 0:30-0:45 | "Now I inject the failure: the provider charges the card, but the response is lost." | Click **Run demo 3**. |
| 0:45-1:05 | "The payment goes to `unknown`, not `failed`. The recovery worker looks the attempt up at the same provider with the stored idempotency key. It never goes to a second provider, because that could double charge." | Watch the payment badge: Created, Unknown (about 6 seconds, keep talking), then Succeeded. |
| 1:05-1:20 | "One attempt, one charge, one journal: debit provider clearing, credit merchant payable, equal totals. The backend checked every invariant itself." | Scroll the card: counts show 1 attempt, 1 charge, 1 journal; all 6 invariants Pass; the state history reads `processing → unknown → succeeded`. |
| 1:20-1:30 | "Every guarantee here is an automated test, including ones that kill the API mid-flight. Code and case study are on GitHub." | Switch to the repo or `docs/case-study.md`. |

## 5-minute walkthrough (six demos)

Run in this order (about 5 minutes). Each demo: click, say, then the proof sentence. All of it is real backend output, labelled Simulated / sandbox.

1. **Live payment processing (about 40 s).** Click **Run demo 1**. Say: "States are `created`, `processing`, `succeeded`, `failed`, `canceled` and `unknown`; every move is an audited transition, and `unknown` is a real state, not an error." Point at the state history and the counts. Optional: **Create $100 payment**, then follow it on the payment detail page (provider, latency, journal).
2. **Duplicate payment protection (about 50 s).** Click **Run demo 2**. The backend sends 10 concurrent requests with the same Idempotency-Key, then one more with a changed amount. Read the invariants aloud: "10 requests sent: 1 first-accept (202), 9 replays, the changed-amount request answered 409 idempotency_conflict", and "payments=1, idempotency_records=1, provider_attempts=1, mock_charges=1, outbox_dispatch_rows=1". Say: "Same key and body replays the stored response; a changed body is a 409; concurrent duplicates collapse to one payment."
3. **Provider succeeds, response lost (about 70 s, the signature demo).** Click **Run demo 3** and narrate the 6-second `unknown` window as in the 90-second script. Then click **Contrast: provider outage**: "If the provider never answers, the payment stays `unknown` and visible, and we never fail over an unknown outcome."
4. **Intelligent routing (about 60 s).** In the demo 4 panel pick a payment method; the routing demo ones are named "Routing demo card", "Routing demo ACH", "Routing demo outage at preferred" and "Routing demo economy only" (id suffixes `...0131` to `...0134`). The simulated providers are all profiles of the same deterministic mock (labelled Simulated): `mock_fast` (card and ACH, 50 ms), `mock_economy` (card, 400 ms, lower priority), `mock_ach` (ACH, 250 ms).
   - "Routing demo card" (`...0131`) then **Evaluate routing**: chosen `mock_fast`; `mock_economy` is listed "Eligible, lower priority"; the reason names the rule and priority.
   - "Routing demo ACH" (`...0132`) at $25: chosen `mock_ach`.
   - The failover guard: run demo 3's outage button or create a payment on "Routing demo outage at preferred" (`...0133`; its preferred provider's token selects the outage scenario, so the payment ends `unknown`). Paste that payment's ID into "Existing payment ID" and evaluate: "Unknown outcome: failover blocked". `mock_fast` is Pinned (lookup only) and `mock_economy` is Blocked: unsafe failover. Say: "The router had another eligible provider and still refuses, because the first provider may already have charged."
5. **Double-entry ledger (about 50 s).** Click **Run demo 5**. Show the journal lines (debit provider clearing, credit merchant payable), "Debits = credits" computed from the lines, and "posted twice more, still one journal". Say: "One journal per success, posted in the same transaction as the state change, immutable via database triggers, keyed so a re-delivered completion adds nothing."
6. **Reconciliation (about 50 s).** Click **Run demo 6** (takes about 6 seconds). It creates a payment that is still `unknown` internally while the simulated provider record says it succeeded, runs a reconciliation, and shows the discrepancy (`unknown_outcome`, status `investigating`) with the provider evidence and an evidence-only note, plus the line "ledger rows changed: 0". Say: "Reconciliation compares provider charges, payment state and journals, records the discrepancy with its evidence, and never repairs anything on its own (ADR 0008). The provider-side record here is simulated, and the card says so."

## What each demo proves to an interviewer

| Demo | Signal |
| --- | --- |
| Go APIs and state machines | Idiomatic Go service with explicit state transitions, audit trail, typed contracts (OpenAPI), exact money in integer minor units. |
| Idempotency and concurrency | You understand at-least-once semantics, request fingerprints, and single-writer database behaviour under concurrent requests. |
| Distributed recovery | You model ambiguity (`unknown`) instead of guessing; outbox, leases, stable provider idempotency keys, crash recovery. |
| Strategy and adapters | Provider logic is behind an interface; scenario and provider profiles are data, not code branches; routing explains its decision and is separate from execution. |
| ACID and double-entry | Money invariants are enforced in the database (balanced journals, immutability) and verified by `railvanta-api check`. |
| Data consistency and reconciliation | Evidence-only reconciliation, duplicate and reordered webhook handling, late outcomes stored not applied. |

## Honest claims (say these, do not stretch them)

- Single-node, single-writer SQLite sandbox. No high availability, no replicas, no defined RPO or RTO.
- One provider implementation: a deterministic mock. The extra providers in the routing demo (`mock_fast`, `mock_economy`, `mock_ach`) are simulated profiles of that same mock with different rails and fixed latencies, not separate integrations. Stripe test mode, Plaid and real rails are not implemented.
- The "provider settlement report" in the reconciliation demo is a simulated provider record written by the simulator, labelled Simulated. There is no real settlement file import.
- The simulation is deterministic and scripted; "failures" are chosen by payment method, not observed in the wild.
- No real funds, no real credentials. Auth is demo-grade.
- No throughput, latency-at-scale or multi-node claims. Measured numbers are small local runs (`docs/measurements.md`).
- The ledger accounts are `provider_clearing` and `merchant_payable`. Do not say "settlement receivable". Say: "A successful payment debits the provider clearing account and credits merchant payable; bank settlement and payouts are not modelled."
- Refunds, disputes, payouts, FX and capture/authorize splits are out of scope.
- CI: the workflows have run on the pull requests. Do not say "always green": one intermittent E2E timing race (a test assumed the provider charge existed as soon as the attempt showed `submitted`) failed once in CI and is fixed on its own branch; check the PR status before quoting CI.

## Anticipated questions

**Why SQLite?** ADR 0001: a correctness demo needs real transactions, constraints and triggers with zero ops. It is a ceiling for scale (all writes serialise). Earlier drafts planned PostgreSQL; a move is not built or tested, so make no migration-effort claim.

**What if the lookup never answers?** The payment stays `unknown` and visible (ADR 0009). There is deliberately no retry button for an unknown payment and no operator force-decision yet; that is a listed limitation.

**Why not retry on another provider?** The first provider may already have charged. Only evidence from the original provider resolves an unknown outcome (ADR 0009, case study point 5).

**How is a double charge prevented end to end?** Four idempotency layers: client key, outbox command, a provider key stored before the call, and keyed ledger posting (`docs/case-study.md`).

**What happens if the API crashes mid-payment?** Startup re-drives in-flight attempts; an expired lease is reclaimed; recovery looks up the attempt. Tested with a real kill: `killApi()` in `e2e/api/recovery.spec.ts` and `e2e/api/dispatch.spec.ts`.

**Webhooks?** Mock webhook only, unsigned, sandbox-only (ADR 0005). Duplicates count once, reordered events never regress a terminal payment (`e2e/api/events.spec.ts`).

**Is the ledger really double-entry?** Yes, per currency in integer minor units; database triggers reject edits to posted journals; `railvanta-api check` re-verifies balance and one journal per success; late outcomes that contradict a terminal payment are stored for review, not applied (ADR 0004).

**Reconciliation fixes mismatches?** No, evidence only (ADR 0008). In demo 6 the payment deliberately stays `unknown` while the discrepancy is investigated, and the ledger row count does not change.

**Are the three routing providers real?** No. They are labelled Simulated profiles of the deterministic mock with different rails and fixed latencies. The point of the demo is the decision and its explanation, not provider integrations.

**Why does the lost-response demo show `unknown` for six seconds?** The demo deployment sets `RECOVERY_RESOLVE_DELAY=6s` so the state is visible. Without it the same lookup resolves in about 200 ms (`docs/measurements.md`).

**Migrations?** Forward-only and additive (ADR 0002). Backups use `VACUUM INTO` (ADR 0006).

**Scale?** "Not claimed." Correct at the tested concurrency (up to 32 concurrent same-key requests, see `docs/measurements.md`), single node only.

## Tests that back each demo

| Demo | Evidence |
| --- | --- |
| State machines / acceptance | `e2e/api/payment-acceptance.spec.ts`, `e2e/ui/payments.spec.ts`, `backend/tests/payment_acceptance_test.go` |
| Idempotency / concurrency | `e2e/api/payment-acceptance.spec.ts` (20 concurrent same-key requests; replay and 409), `backend/tests/payment_acceptance_test.go :: TestConcurrentSameKeyCreatesExactlyOnePayment` |
| Distributed recovery | `e2e/api/recovery.spec.ts` (central demo, `killApi()`), `e2e/api/dispatch.spec.ts`, `backend/tests/crash_harness_test.go`, `node scripts/measure-central-demo.mjs` |
| Strategy / adapters | `e2e/ui/simulator.spec.ts`, `e2e/ui/routing.spec.ts`, ADR 0003 |
| ACID / double-entry | `e2e/api/ledger-posting.spec.ts`, `e2e/ui/ledger.spec.ts`, `backend/tests/ledger_posting_test.go` |
| Consistency / reconciliation | `e2e/api/events.spec.ts`, `e2e/ui/events.spec.ts`, `e2e/ui/reconciliation.spec.ts` |

Full matrices: `docs/case-study.md` (scenario table with reproduce commands) and `docs/verification-matrix.md`. To show tests passing live without a full run: `pnpm test:e2e api/recovery.spec.ts --grep "central demo"` (starts its own API; do not run it on top of the demo stack if the laptop is struggling).

## If something breaks

1. Page shows errors or empty data: the session expired or the stack was recreated. Close the browser and run `pnpm demo:open` again.
2. Stack unhealthy: `pnpm demo:down -v && pnpm demo:up` (deletes demo data, fresh credentials; a couple of minutes).
3. Images stale or missing: `DEMO_BUILD=1 pnpm demo:up` (slow; avoid right before presenting).
4. Playground misbehaves but the API works: use the curl examples from the `pnpm demo:up` banner, or `node scripts/measure-central-demo.mjs` for a narrated central demo with assertions.
5. Nothing works: show the recorded video / screenshots (below), `docs/case-study.md`, and the passing Playwright specs listed above.

## Recording plan (90-second walkthrough, record the night before)

Capture at 1440x900, browser zoom 100%, notifications off. Sequence: Playground idle, create $100 payment, inject lost response, `unknown` with the dwell visible, resolved `succeeded`, journal with balanced totals, then 5 seconds of the repo and the test list. Take stills of the `unknown` state and the balanced journal as fallback screenshots.

Optional automation: `node scripts/demo-record.mjs` (stack up first) records a Playwright video to `.demo/recordings/`. Set `DEMO_BUTTONS="Run demo 1,Run demo 3"` (the real button names). It was written but not run against the shipped Playground; check it once before relying on it. A manual screen recording (QuickTime, Cmd+Shift+5) is the safe default.
