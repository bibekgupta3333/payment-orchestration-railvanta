# Known limitations

What Railvanta does not do, and what has not been verified (WBS 5.4.6). Derived from the code, the unticked [WBS](wbs.md) items and the gaps reported in [CODECHANGELOG.md](../CODECHANGELOG.md). Read it alongside [measurements.md](measurements.md); a capability is claimed only where a linked test or recorded run backs it.

## What this is

A sandbox. It moves no real funds, talks to no real payment provider, and holds no real payment credentials. Do not point it at production money.

## Architecture limits

- **Single SQLite writer.** One API process owns one SQLite database file in WAL mode. All writes serialise on it. Concurrency tests and measurements ([measurements.md](measurements.md)) show correct behavior at the tested levels, not scale.
- **No high availability.** There is no replica, failover or second writer. The API is a single process; a restart is recoverable (startup re-drives in-flight attempts, see the recovery specs) but the service is down while it restarts. Never run two API processes on one volume.
- **No defined recovery targets.** No RPO or RTO exists and none has been measured. The backup/restore runbook and its one recorded drill are in [operations/deployment.md](operations/deployment.md#implemented-container-backup-restore-and-drill-wbs-52); the last successful drill was 2026-11-01 on the local `m5-wip` build (600 payments, restore checks passed). That drill ran locally only. Off-volume, encrypted copies and retention are the operator's job; nothing here schedules backups, ships them or defines retention. The in-cluster drill was not run.
- **Rate limit is crude.** Each client address gets 600 requests per fixed one-minute window, kept in process memory and not configurable (`backend/cmd/api/main.go`). It resets on restart, is keyed on the remote address, and is not suitable behind a shared proxy without further work. Excess requests get `429 rate_limited`.
- **Metrics are in process only.** Dispatcher, recovery and provider-health counters exist as in-memory structures; no metrics endpoint or exporter, no tracing backend, no alerting. Provider health (`GET /v1/providers/health`) is computed from stored attempt timings and reports `no_observations` or `insufficient_data` instead of inventing values.

## Provider and payment-scope limits

- **Mock provider only.** The one provider is the deterministic in-repo mock; its scenarios are selected by the payment method's token reference. Stripe test mode, Plaid sandbox and any real rail are deferred and need their own contracts, webhook security and reconciliation evidence.
- **Failover is not automatic for unknown outcomes, by design.** An `unknown` attempt is only resolved by lookups at the original provider with the stored key. The mock `outage` and `timeout_before_execution` scenarios therefore stay `unknown` in the measured windows ([measurements.md](measurements.md)); an operator-facing way to force a decision is not built (the payment page shows no retry action).
- **Deferred money operations.** Capture/authorization splits, refunds and partial refunds, disputes, bank settlement and returns, payouts, and fee or FX accounting are not implemented. Payments are a single create-and-settle flow with a success journal.
- **No multi-currency totals.** Amounts are integer minor units per currency; ledger accounts and overview metrics are per currency and never summed across currencies. There is no FX. An account whose total overflows int64 is reported as an explicit error rather than a rounded number.
- **Routing is read-mostly.** Routing rules are listed and evaluated (`POST /v1/routing/evaluate` creates nothing), but there is no rule editor; with one mock provider the router has little to choose between. Provider and key management are not built.
- **Webhooks are mocked.** `POST /v1/sandbox/webhooks/mock` is an unsigned, operator-only test endpoint processed synchronously in the request. There is no real provider webhook endpoint, signature verification (inbox rows carry `signature_status = not_applicable`) or asynchronous inbox worker. The simulator's `duplicate_webhook` and `reordered_events` scenarios are listed but disabled and return 422 `scenario_not_implemented`; duplicate and reordered delivery are covered by `api/events.spec.ts` and Go tests instead.
- **Reconciliation compares the mock only.** Runs compare mock charges, payment state and journals for a window; they record evidence and never post journals or change payments.
- **Auth is demo-grade.** Workspace API keys and a demo cookie session exist; there is no user management, SSO, key rotation UI or fine-grained audit of operators. `DEMO_MODE` is refused unless bound to loopback. Cross-workspace isolation is covered by Go tests, not by E2E (the E2E other-workspace key is revoked by an earlier spec).

## Verification gaps

- **No CI result exists.** The GitHub Actions workflows (`ci.yml`, `containers.yml`, `k8s.yml`) were authored and linted but have not run on GitHub. WBS 5.1.10, 5.1.11, the "green on main" and "three consecutive CI runs" gates, 5.2.10 and 5.2.11 (image digests per release, CI secret scan) stay open. The roadmap CI rows read "pending first run on this PR".
- **Kubernetes is optional and partly unverified.** Manifests validate with kubeconform and pass a k3d smoke test. Egress NetworkPolicy enforcement was not observed in k3d (a default-deny pod still reached other addresses; cause undiagnosed), so WBS 5.3.6 is open. Not run: server-side dry-run apply, `pnpm test:e2e` through the Ingress, the in-cluster backup drill, pinned image digests for the cluster images (5.3.10), and resource sizing (initial values are unmeasured).
- **Containers.** Image builds, startup, persistence, backup/restore and a container E2E run were recorded locally (see the changelog). The `ui/payments.spec.ts` double-click-submit test failed intermittently in two earlier container runs and passed in later ones; the cause was not found.
- **UI checks not done.** The 375 px labelled-scroll-region and keyboard check of the simulator timeline with a live run, a screen-reader pass on the run-acceptance announcement, explicit keyboard navigation of the 1440 px reconciliation table, empty and error states of reconciliation and the events pages at both widths, and dark-theme contrast checks are recorded as unverified in the changelog.
- **Measurements are local and small.** See the caveats in [measurements.md](measurements.md); the 100-repeat same-key gate was not run (5 rounds per level), and nothing was measured under Docker, Kubernetes or on another machine.
- **Stale WBS boxes.** Several early WBS acceptance and E2E checkboxes (work packages 1.1 to 1.3) are still unticked although later changelog entries record the full suite passing; they have not been re-audited line by line. Trust the changelog entry and the test run, not the box.
