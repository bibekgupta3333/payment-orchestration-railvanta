# Presenter script: why Railvanta exists (about 10 minutes)

A spoken script for presenting the project with its context. It walks the eight sample runs the demo stack seeds at startup, in order, and for each one says what goes wrong in the real world, where to click, and what the evidence proves. For the short 90-second pitch and the Playground walkthrough, use the [demo runbook](README.md); for the design behind the samples, [ADR 0014](../decisions/0014-prepopulated-sandbox-samples.md).

> **Setup (do this 15 minutes before)**
>
> 1. `pnpm demo:up` (after code changes: `DEMO_BUILD=1 pnpm demo:up`, which builds first and is slow).
> 2. Open <http://localhost:3000> in **Chromium or Firefox**. Not Safari: the session cookie is `Secure`, and Safari does not store it on plain-http `localhost`, so the dashboard would stay signed out.
> 3. The demo stack sets `SANDBOX_SAMPLE_DATA=true`, so the API runs the eight sample scenarios once, by itself, within about a minute of startup (seeds `sample-success`, `sample-decline`, and so on). A restart does not run them again if they already exist; `pnpm demo:down -v` deletes them with the rest of the demo data.
> 4. Fallback if the samples are missing (older image, flag off, or the startup run failed): on the dashboard hub click **Load sample data** (about 20 seconds; it runs the same eight scenarios from the browser).
> 5. Everything here is a sandbox: one deterministic mock provider, no real money, every run labelled Simulated.

Conventions below: **Say** is what you tell the audience, **Show** is the click path, **Takeaway** is the one line to leave them with. The landing section, the hub panel and its links were checked on the Docker demo stack on 2026-11-01. The one item still marked *(unverified)* should be checked once on the live stack before presenting.

## 1. The problem (about 1 minute)

**Say:**

"When an application takes a payment, there is a gap between *we sent the charge* and *we know what happened*. Most of the hard bugs in payments live in that gap.

A request times out. Did the provider charge the card or not? If you retry and it did, the customer pays twice. If you don't retry and it didn't, you lose the sale. A client double-clicks, or a mobile app retries on a flaky network, and now two identical requests race each other. A provider goes down, and the obvious fix, sending the payment to a backup provider, can charge the customer twice if the first provider actually went through. Providers notify you with webhooks, and webhooks arrive twice, or late, or out of order. The internal ledger drifts away from what really happened when a success is recorded twice. And at the end of the day, the provider's settlement records disagree with yours, and someone has to find out why without making the numbers up.

None of this is exotic. These are ordinary consequences of talking to an external system over a network. What is hard is handling all of them together, and proving that you do."

## 2. What Railvanta is (about 30 seconds)

**Say:**

"Railvanta is a sandbox payment orchestrator that makes each of those failures reproducible on demand. For each failure it shows the guarantee that handles it, and the evidence: the provider attempts, the ledger journals, the events, and the discrepancy records, all read back from the database and checked by the backend itself.

It is a sandbox. There is one provider, a deterministic mock, and no real money moves. Real providers are deliberately out of scope. The point is the correctness of the orchestration, not an integration catalogue."

**Show:** the landing page (<http://localhost:3000>). Point at the "Why Railvanta exists" section (problem, guarantee, see it in the sandbox) and the lifecycle visual (Client, API, Outbox, Dispatcher and Router, Provider, Ledger, Reconciliation). Then click **Start sandbox** in the "Call the API in one step" panel (or **Explore sandbox** in the header if you are already signed in) and open the dashboard hub, `/dashboard`.

## 3. Walkthrough: the eight samples (about 8 minutes)

The six failure samples are on the hub in the "The story: what this sandbox proves" panel, each with a status pill, its invariant count and the links **Inspect run** (`/dashboard/simulator?run=<id>`) and **Payment**. The two baseline samples (`success`, `decline`) are not panel rows; find them on the Payments page.

The run page (verified in `frontend/components/simulator/SimulatorView.tsx`) shows the run status and the payment's status pill, an **Execution timeline**, and an **Inspector** with the tabs **Request**, **Response**, **Routing**, **Attempts**, **Events**, **Journal** and **Invariants**. The Invariants tab lists every check the backend ran, with Pass or Fail and the evidence it read from the database. The payment page (`/dashboard/payments/<id>`) has a **State history** and the tabs **Summary**, **Attempts**, **Events** and **Journal**.

Note that the run page does not print the Playground's summary lines ("No second charge", "Ledger rows changed: 0"); those appear only on Playground cards (`frontend/components/playground/RunResult.tsx`). On the run page, read the same facts from the Invariants tab, as described below.

### 3.1 Success (about 45 seconds)

**Say:** "First the normal path, so you know what good looks like. The payment and its dispatch command are written in one transaction; a dispatcher then calls the provider outside any transaction, and the success and its journal commit together."

**Show:** story panel, `success`, **Inspect run**. Execution timeline: run accepted, payment accepted through the real acceptance path, then the pipeline steps. **Attempts** tab: one attempt. **Journal** tab: one journal, `provider_clearing` debit and `merchant_payable` credit for the same amount. **Invariants** tab: all Pass.

**Takeaway:** "One payment, one attempt, one balanced journal, committed together."

### 3.2 Decline (about 45 seconds)

**Say:** "A decline is the easy failure, because the provider proves the charge had no effect. Even so, it has to end cleanly: failed, and nothing in the ledger."

**Show:** `decline`, **Inspect run**. Status pill: Failed. **Journal** tab: "No journal posted. A journal exists only for a succeeded payment." **Invariants** tab: "One balanced journal for a succeeded payment, none otherwise" passes with "0 posted journal(s) for a payment that is failed (expected 0)".

**Takeaway:** "A failure is only recorded as failed when the provider proves it; then no money is booked."

### 3.3 Duplicate request (about 1 minute)

**Say:** "A user double-clicks, or a client library retries. Here the backend sent ten concurrent requests with the same Idempotency-Key, then an eleventh with the same key and a different amount."

**Show:** `duplicate_request`, **Inspect run**. **Invariants** tab: "One first-accept, the rest replays, and a changed amount is rejected with 409", evidence "10 request(s) sent: 1 first-accept (202), 9 replay(s), 0 error(s); changed-amount request answered HTTP 409 idempotency_conflict". Then "One payment, one idempotency record, one attempt, one charge and one dispatch command", evidence `payments=1 idempotency_records=1 provider_attempts=1 mock_charges=1 outbox_dispatch_rows=1`. The Execution timeline also has the changed-amount request.

**Takeaway:** "Same key and same body replays the stored answer; same key with a different body is refused; ten racing requests are one payment."

### 3.4 Timeout after success: the lost response (about 1 minute 15 seconds, the central case)

**Say:** "This is the one the project was built around. The provider charges the card, but the response never comes back. A naive system either marks it failed, and loses track of a real charge, or retries, and charges twice. Railvanta marks the payment *unknown*, which is a real state, not an error. A recovery worker then asks the same provider, using the stored idempotency key, what happened. It never goes to a different provider, because the first one may already have taken the money."

**Show:** `timeout_after_success`, **Inspect run**. Status pill: Succeeded. **Attempts** tab: exactly one attempt. **Invariants** tab: "At most one succeeded provider charge per payment" passes with "1 succeeded provider charge(s) across 1 attempt key(s)"; "No cross-provider attempt after an unknown outcome" passes with "payment reached unknown: true; 1 attempt(s) across 1 provider(s)". Then follow the **Payment** link: the **State history** reads processing, then unknown, then succeeded. Because the samples ran at startup, the unknown window is already over; to show it live, click **Run default demo** at the top of this same page (about 6 seconds of unknown on the demo stack) or Playground demo 3.

**Takeaway:** "A timeout is not a failure. Unknown is resolved only by evidence from the original provider, so there is no second charge."

### 3.5 Provider outage (about 1 minute)

**Say:** "Now the provider is down: submissions and lookups both fail. The tempting move is to fail over to another provider. Railvanta refuses, because it cannot prove the first one did nothing. The payment stays unknown, and visible, until evidence arrives."

**Show:** `outage`, **Inspect run**. Status pill: Unknown. **Invariants** tab: "No cross-provider attempt after an unknown outcome" passes with one provider; "Payment reached the scenario's expected status" passes with "payment is unknown; scenario outage expects unknown". The **Routing** tab shows an "Unsafe failover blocked" notice when the backend reports a blocked failover for this run *(unverified for the startup sample)*. The payment page shows an unknown-outcome notice and offers no retry button.

**Takeaway:** "We never fail over an unknown outcome; an honest unknown is better than a possible double charge."

### 3.6 Duplicate ledger post (about 45 seconds)

**Say:** "Ledgers drift when the same success is booked twice, for example when a completion message is processed again after a crash. Here the backend posts the success journal two extra times with the same posting key."

**Show:** `duplicate_posting`, **Inspect run**. Execution timeline: "Posted the success journal again twice". **Journal** tab: still one journal, debits equal credits. **Invariants** tab: "One balanced journal for a succeeded payment, none otherwise" passes with "1 posted journal(s)". Optionally open `/dashboard/ledger`.

**Takeaway:** "Posting is keyed, so a replayed success adds nothing; journals are balanced and immutable."

### 3.7 Duplicate webhook (about 45 seconds)

**Say:** "Providers deliver webhooks at least once, so the same event arrives several times. Each delivery must not be treated as a new fact."

**Show:** `duplicate_webhook`, **Inspect run**. **Events** tab, "Webhook inbox" table: one row for the provider event, with **Repeat deliveries** showing the repeats (the event is delivered 3 to 5 times depending on the seed, so this column reads 2 to 4). **Invariants** tab: "Exactly one inbox row per provider event", "Inbox duplicate count equals the repeat deliveries" and "Exactly one financial effect despite repeated events" ("1 journal(s), 1 terminal transition(s), 0 late outcome(s)"). Optionally `/dashboard/events`.

**Takeaway:** "Duplicates are counted, not applied: one inbox row, one financial effect."

### 3.8 Settlement mismatch (about 1 minute)

**Say:** "Finally, the end-of-day question: does the provider agree with us? Here the provider-side record says the charge succeeded, while our payment is still unknown. Reconciliation finds the disagreement and records it with the evidence. It does not quietly edit the ledger to make the numbers match."

**Show:** `settlement_mismatch`, **Inspect run**. Status pill: Unknown. **Invariants** tab: "Reconciliation recorded a discrepancy with simulated provider evidence" (kind `unknown_outcome`, provider charge succeeded, simulated=true); "The reconciliation run changed no journal, entry, payment transition or attempt", evidence starting "ledger rows changed: 0 required; journals 0->0, entries 0->0 ..."; "The investigation was recorded as evidence only, with no adjusting entry". Then open `/dashboard/reconciliation`: the discrepancy is listed with status investigating and its evidence. Say clearly that the provider-side record is simulated and labelled so.

**Takeaway:** "Reconciliation records evidence and changes nothing on its own: ledger rows changed, zero."

## 4. Closing (about 1 minute)

**Say:** "So: every failure from the first minute has a reproducible run here, and every guarantee is checked against stored evidence, by the backend and by the automated test suite, including tests that kill the API mid-payment."

**Deliberately out of scope** (from the [README](../../README.md) and [known limitations](../known-limitations.md)):

- Real providers. There is one deterministic mock; the routing demo's extra providers are simulated profiles of it. Stripe test mode, Plaid and real rails are not built.
- Scale and availability. One API process, one SQLite writer, no replicas, no high availability, no RPO or RTO. No throughput claims.
- Money operations beyond create-and-settle: refunds, disputes, captures, payouts, bank settlement, FX.
- Real webhook security. The webhook endpoint is a sandbox mock, unsigned.
- Production auth. Auth is demo-grade; the public sandbox access on the demo stack makes any visitor an operator of a shared workspace (ADR 0013).
- An operator "force decision" for a payment stuck in unknown.

## 5. Likely questions, honest answers

**Is this data real?** It is real output of the real pipeline, not inserted rows: each sample went through the same acceptance, dispatch, recovery, ledger and reconciliation code as any API call, against the mock provider. It is simulated in the sense that the provider is a mock and the faults are chosen on purpose. Everything is labelled Simulated.

**Why should I believe the green checks?** The invariants are evaluated by the backend from persisted rows (payments, attempts, mock charges, journals, inbox rows), not computed in the browser. The same properties are covered by Go tests and Playwright specs listed in the [demo runbook](README.md#tests-that-back-each-demo).

**What happens if the provider never answers?** The payment stays unknown and visible; lookups continue at the same provider. There is no automatic resolution, retry or failover, and no operator override yet.

**Why not just retry on another provider?** Because the first provider may already have charged. Only evidence from the original provider resolves an unknown outcome.

**Does it scale?** Not claimed. It is correct at the concurrency tested (see `docs/measurements.md`), on a single node.

**Does reconciliation fix mismatches?** No. It records them with evidence and leaves the decision to a person; in sample 8 the payment stays unknown and no ledger row changes.

**Why SQLite?** Real transactions, constraints and triggers with zero operations, which suits a correctness demo. It is also the scale ceiling.

**Is the CI green?** Check the pull request before saying so. Do not claim "always green".

**Can I try a scenario myself?** Yes: the Simulator page runs any scenario with a seed you choose, and the Playground runs the six headline demos with one click each.
