# Code Changelog

A record of every change to code, configuration or design docs, newest first. Each entry says what changed, which [WBS](docs/wbs.md) item it serves, and what was actually run to verify it. Only record results you observed. If a check was skipped or failed, say so.

## Entry template

```md
## YYYY-MM-DD — Short imperative summary

**WBS:** 2.1.3, 2.1.4 · **PR/commit:** <link or hash>

**Changed**
- `path/to/file` — what and why

**Verification**
- `pnpm check` — pass / fail (details)
- `pnpm test:e2e` — N passed, M failed (spec names); include earlier packages' specs
- Manual check — what was observed

**Notes:** follow-ups, known limits, deferred items
```

---

## 2026-10-31 — Add the ledger explorer (WBS 4.1) and reconciliation (WBS 4.2)

**WBS:** 4.1.1–4.1.10, 4.2.1–4.2.10, 4.1 and 4.2 gates and E2E (two 4.2 gates left open, see Notes) · **PR/commit:** worktree branch worktree-agent-ac5ed560caef50576

**Changed**
- `backend/migrations/0004_reconciliation.sql` (new) — run count columns (`compared/matched/unmatched/mismatch/evidence_consumed`, `requested_by`), append-only `reconciliation_resolution_events`, and triggers: evidence and run windows immutable, no deletes, `resolved` final, blank resolution note/resolver rejected, discrepancies inserted `open`. The `kind` CHECK already existed in 0001 and is relied on. `0003_placeholder_local.sql` exists only in this worktree so the numbering is consecutive and is **not committed**; the routing branch supplies the real 0003.
- `backend/internal/ledger/{explorer.go,read.go}` — checked `AddChecked/SubChecked/Totals`, `ListAccounts` (balances from posted entries, per-account `ErrOverflow` instead of a rounded value), `GetPosted` (totals, reversed-by), currency filter and `ReversedBy` on lists.
- `backend/internal/transport/http/{ledger_views.go,reconciliation.go}` (new), `ledger.go`, `handler.go` (two routing hooks) — `GET /v1/ledger/accounts`, `GET /v1/ledger/transactions/{id}`, currency filter, `reversed_by`; `GET/POST /v1/reconciliation/runs`, `GET /v1/reconciliation/discrepancies[/{id}]`, `POST /v1/reconciliation/discrepancies/{id}/resolve`. Writes are operator-only (the existing write guard plus a second role check) and CSRF-checked for cookie sessions, so `POST runs` is implemented.
- `backend/internal/reconciliation/{service,query,resolve}.go` (new) and `backend/internal/provider/mock/evidence.go` (read-only charge lookup) — the comparison service, rules for the five kinds, late-outcome consumption (marks rows delivered, never touches payments), run idempotency (`run_key`, `(run_id, fingerprint)`), resolution with history.
- `backend/internal/storage/{models.go,storage_test.go}` — new columns/model; table count 23 to 24 and an explicit-column run insert in the constraint test.
- `backend/api/openapi.yaml`, `frontend/lib/api/schema.d.ts` (generated) — new paths and schemas; the stub reconciliation paths replaced.
- `frontend/{app/dashboard/ledger,app/dashboard/reconciliation}`, `frontend/components/{ledger,reconciliation}/`, `frontend/lib/{api/ledger-schemas.ts,hooks/ledger.ts,hooks/reconciliation.ts,ledger-format.ts}` — ledger page (Accounts and Journal tabs, URL filters, per-currency groups, 375px cards), journal detail ("Posted / immutable", computed balance check, reversal links, payment link), reconciliation page (run selector, count cards, filters, evidence, history, related journal, confirm dialog, resolve form, all evidence labelled Simulated). `lib/hooks/payments.ts` only exports `useScopedRead`; `PaymentDetail.tsx` links the journal; `schemas.ts` accepts `account_code`.
- `backend/tests/{ledger_explorer_test.go,reconciliation_test.go}`, `frontend/tests/components/ledger.test.tsx`, `e2e/ui/{ledger,reconciliation}.spec.ts`; `e2e/ui/shell.spec.ts` lists the two routes as implemented.
- `docs/backend/flows.md` (reconciliation contract), `docs/wbs.md` (ticks, status rows).

**Verification**
- `go -C backend test -race -count=1 ./...` — pass, all packages; 225 PASS lines with `-v`, 0 FAIL. New: 13 top-level tests in `backend/tests` for ledger and reconciliation (each discrepancy kind, rerun idempotency, no invented adjustment via a before/after table dump, late-outcome consumption, resolution permissions and database triggers, overflow).
- `pnpm check` — pass (Vitest 69 passed in 5 files, 11 new in `ledger.test.tsx`; lint, generated contracts, typecheck and build clean).
- `pnpm test:e2e` — 58 passed, 0 failed (9 new in `ledger.spec.ts` and `reconciliation.spec.ts`, including axe at 375px and 1440px).
- Fixtures: `unknown_outcome` comes from the real pipeline (the seeded outage method). Forged rows (an `amount_mismatch`, `status_mismatch`, `missing_posting` and `missing_at_provider`) are inserted directly in Go tests only. No E2E forging.

**Notes**
- A timeout-after-success payment is resolved by the recovery lookup before reconciliation runs, so the E2E `unknown_outcome` uses the outage scenario (no provider charge) rather than "provider confirmed success"; that case is covered by the Go `status_mismatch` fixture and the late-outcome test.
- Cross-workspace 404s are asserted in Go only: the E2E `otherKey` is revoked by `auth-contract.spec.ts`.
- Account balance overflow is reported per account (`balance_error: amount_overflow`, null amounts, HTTP 200) so one overflowing KWD account does not hide other currencies. The shared E2E database really overflows KWD (several max-int64 KWD payments), which the E2E asserts.
- Open 4.2 gates: keyboard navigation of the 1440px table is not explicitly tested, and the reconciliation page's empty and error states are not yet covered by a test.
- On merge: the storage test table count and the routing/events migrations (0003, 0005) need reconciling, and `e2e/ui/shell.spec.ts` placeholder list will conflict with the other packages.


## 2026-10-31 — Merge the WBS 3.1 payment screens into the M2 backend

**WBS:** 3.1, 2.2–2.4 · **PR/commit:** feat/m2-m4-payment-correctness

**Changed**
- Merged the 3.1 worktree branch (dashboard API, payment explorer/detail/create dialog, hooks, money helpers, UI E2E) into the branch holding the 2.1–2.4 backend. Conflicts in `CODECHANGELOG.md`, `docs/wbs.md`, `backend/api/openapi.yaml` and `frontend/lib/api/schema.d.ts` were resolved. OpenAPI was merged structurally (base/ours/theirs) so both the 3.1 history path and the 2.4 cancel and recovery paths are present; the type file was regenerated.
- `backend/internal/transport/http/ledger.go` — 3.1 gave the cursor helpers a sort-direction argument. Ledger pages now pass `false`, because journals are always newest first.
- `e2e/ui/payments.spec.ts`, `e2e/support/{state.ts,global-setup.ts}` — the unknown-payment test no longer forges an `unknown` row in a stopped database. It now creates a payment with the seeded outage method, which really stays unknown, and waits for that status. The `mark-unknown` harness hook is removed.

**Verification**
- `pnpm check` — pass (Vitest 58 passed, lint and build clean).
- `go -C backend test -race ./...` — pass.
- `pnpm test:e2e` — 49 passed, 0 failed. The first run after the merge failed 1 test, the forged-unknown fixture described above.

**Notes:** the 3.1 gates for slow-API states, a terminal-state UI check against a live dispatcher, and CI remain open.

## 2026-10-31 — Add recovery lookup worker, startup scan, cancellation and late-outcome recording

**WBS:** 2.4.1–2.4.10, 2.4 acceptance gate and E2E, 2.3 gates (invariant checker after every 2.2/2.4 scenario; canceled payments have no journal) · **PR/commit:** uncommitted

**Changed**
- `backend/internal/payment/recovery.go` (new) — `Recovery.Handle` for `payment.lookup`: `Lookup` on the original provider with the stored key outside any transaction; success resolves `unknown → succeeded` with the journal (`ApplySuccess`), a recorded decline resolves `unknown → failed`, anything else (unavailable, still processing, no record without replay) stays `unknown` with jittered backoff and no parking. Same-key same-provider replay only when `Capabilities.IdempotentReplay`. `RecoverStale` startup scan. `RecordLateOutcome` writes `payment.late_outcome` outbox rows. Test hook `RecoveryHooks.BeforeResolve`.
- `backend/internal/payment/recovery_metrics.go` (new) — lookups, unavailable, replays, still-unknown, stale recovered, resolutions by result, unknown-age histogram, plus `Recovery.Snapshot` gauges (unresolved count, oldest unresolved payment and age).
- `backend/internal/payment/cancel.go` (new) — `Service.Cancel` (`created → canceled`, version-guarded, audit, settles the pending dispatch command), `ErrNotCancelable`, `Service.Recovery` and `RecoveryView`.
- `backend/internal/payment/dispatch.go` — re-reads after a stale-version loss in prepare (a canceled payment completes the command with no attempt); a late result at record time is recorded as `payment.late_outcome` and the command completed.
- `backend/internal/payment/outcome.go` — `Outcome.LookupDelay`.
- `backend/internal/event/dispatcher.go`, `outbox.go` — `RetryAfterUnbounded` (exempt from MaxAttempts parking), `SkipPending`.
- `backend/internal/transport/http/payments.go` — `POST /v1/payments/{id}/cancel` (200, 404, 409 `not_cancelable`) and `GET /v1/payments/{id}/recovery`.
- `backend/internal/config/config.go`, `backend/cmd/api/main.go`, `backend/.env.example` — `RECOVERY_BASE_BACKOFF`, `RECOVERY_MAX_BACKOFF`, `RECOVERY_LOOKUP_DELAY`; main runs the startup scan before the dispatcher starts and registers the lookup handler.
- `backend/api/openapi.yaml`, `frontend/lib/api/schema.d.ts` (generated) — `cancelPayment`, `getPaymentRecovery`, `PaymentRecovery`.
- `backend/tests/recovery_test.go`, `cancel_test.go`, `crash_harness_test.go`, `recovery_helpers_test.go` (new) — central demo, provider outage for lookup, no parking, no-record-is-not-proof and replay-under-same-key, no replay without the contract, startup scan, no other provider, late result via dispatch and lookup, terminal states, cancel behavior and responses, 100 cancel-vs-dispatch races on separate connections, and a crash harness (after prepare, before record, before resolve; decline variant). Every test ends with `ledger.Check`.
- `e2e/api/recovery.spec.ts` (new); `e2e/api/dispatch.spec.ts`, `ledger-posting.spec.ts`; `e2e/support/{global-setup,state}.ts`, `e2e/README.md` — five recovery specs and a canceled-journal spec; harness adds quick recovery backoff, audit `transitions` in `inspectPayment()` and `seedCreatedPayment()`. Two `dispatch.spec.ts` tests changed because lookups now resolve what they used to leave `unknown`: the kill test now ends `succeeded` with one charge, and the never-another-provider test uses the outage method, which stays `unknown`.
- `docs/backend/flows.md`, `docs/wbs.md` — recovery and cancellation contract, late-result record, limits; 2.4 and the two 2.3 gates ticked.

**Verification**
- `go -C backend test -race -count=1 ./...` — pass (all packages; 90 top-level and subtest PASS lines with `-v`, 42 in `backend/tests`). `go -C backend test -race -count=3 ./tests/` — pass.
- `pnpm check` — exit 0: golangci-lint 0 issues, Vitest 23 passed, generated contracts current, build passed.
- `pnpm test:e2e` — 41 passed, 0 failed (6 new plus all earlier specs).
- `pnpm generate:api` regenerated; `pnpm lint:openapi` valid (1 existing warning).

**Notes**
- Late results are stored as `payment.late_outcome` outbox rows (pending, with no handler) until the WBS 4.2 reconciliation consumer exists; `GET /v1/payments/{id}/recovery` lists them.
- Unknown payments retry lookups without a limit; a provider with neither lookup nor replay leaves them unknown until reconciliation.
- "Kill after provider acceptance" is exercised with the delayed-success method (the mock holds the charge as processing); the exact instant between a success commit at the provider and the result commit is covered by the Go crash harness.
- Metrics are in-process only.

## 2026-10-31 — Add outbox dispatcher, mock execution, atomic success posting and ledger reads

**WBS:** 2.2.6–2.2.13, 2.2 acceptance gate and E2E, 2.3.5, 2.3.8, 2.3.9, 2.3 acceptance (partial) and E2E (partial) · **PR/commit:** uncommitted

**Changed**
- `backend/internal/event/outbox.go`, `dispatcher.go`, `metrics.go`, `doc.go` — `Enqueue` (dedup with `ON CONFLICT DO NOTHING`), `ClaimDue` (compare-and-swap on id/status/previous `lease_owner`, fresh 32-byte `crypto/rand` token, bounded `lease_until`, reclaims expired leases), and token-checked `Renew`/`Complete`/`Release`/`Fail` (`ErrLeaseLost`). `Dispatcher` claims only registered types and renews leases every lease/3. It retries with full-jitter backoff or `RetryAfter`, parks a row after 10 acquisitions, and has `Start`, `Notify`, `RunOnce` (tests) and bounded `Stop` drain-then-cancel. Lag/claim metrics.
- `backend/internal/payment/dispatch.go` — `Executor.Handle` for `payment.dispatch`. The prepare transaction routes, guards, moves `created → processing`, and inserts attempt `rv_{payment}_{n}` as `submitted` with `submitted_at`. The `Submit` call runs outside every transaction under `PROVIDER_TIMEOUT`. A new busy-retried transaction records the outcome. Unroutable payments: `created → processing → failed` with no attempt. A reclaimed stale `submitted` attempt goes to `unknown` plus lookup and is never resubmitted. Test hooks `AfterPrepare`/`BeforeRecord`.
- `backend/internal/payment/outcome.go` — `ApplySuccess` (2.3.5: guarded payment and attempt success, `ledger.PostPaymentSuccess`, audit, outbox completion in one transaction), `ApplyDecline`, `ApplyUnknown` (schedules `payment.lookup`, payload `LookupCommand`, dedup `payment.lookup:{attempt_id}`), `ErrLateOutcome`, `GuardNewAttempt`/`ErrUnsafeFailover` (2.2.11).
- `backend/internal/payment/router.go` — `Router.Decide`, shared by evaluation and dispatch. It excludes providers with no adapter.
- `backend/internal/payment/execution_metrics.go` — attempts started, outcomes by provider, unknown count, stale reclaims, no-route, blocked failovers, `completed_at − submitted_at` histogram.
- `backend/internal/storage/attempt_repository.go` — `InsertAttempt`, `GetAttempt`, status-guarded `UpdateAttemptStatus`, `FindMethodToken`, `FindProvider`.
- `backend/internal/ledger/read.go` — `ListPosted`: workspace-scoped posted journals with keyset pagination and entries joined to account code and provider.
- `backend/internal/transport/http/routing.go`, `ledger.go`, `handler.go`, `payments.go` — `POST /v1/routing/evaluate` (read-only; 400/403/404/415/422 `no_eligible_provider`) and `GET /v1/ledger/transactions` (`cursor`, `limit`, `payment_id`; decimal-string amounts). `Config.Router` and `Config.OnAccepted` (wakes the dispatcher). Both routes are removed from the 501 list.
- `backend/internal/config/config.go`, `config_test.go`, `backend/.env.example` — `DISPATCHER_POLL_INTERVAL`, `DISPATCHER_LEASE`, `DISPATCHER_DRAIN_TIMEOUT`, `DISPATCHER_MAX_IN_FLIGHT`, `PROVIDER_TIMEOUT`, `MOCK_DELAYED_SUCCESS_DELAY` and `MOCK_TIMEOUT_DELAY`, with bounds.
- `backend/cmd/api/main.go` — builds the mock adapter, executor and dispatcher after migrations and seeding, and starts it. On shutdown it stops claiming and drains (bounded) alongside the HTTP shutdown, then closes the database.
- `backend/internal/auth/seed.go` — demo seed adds scenario methods `…0121`–`…0126` (tokens `…0321`–`…0326`, `mock_<scenario>_<id>`) and one priority-0 `mock` routing rule per currency per workspace (`…0701`–`…0705`, `…0711`–`…0715`). Existing IDs are unchanged.
- `backend/api/openapi.yaml`, `frontend/lib/api/schema.d.ts` (generated) — evaluate and ledger operations documented as implemented. `RoutingDecision` adds `provider_code` and `priority`. `LedgerJournal` adds `posting_key`, `reversal_of`, timestamps, and entry `id`, `account_code` and `provider_id`.
- `backend/tests/dispatch_helpers_test.go`, `dispatch_test.go`, `ledger_posting_test.go` — 16 new top-level file-backed WAL integration tests: every scenario's states, plus `ledger.Check`, audit and metrics. Also covered:
  - a write succeeds during a blocked submit;
  - two dispatchers on separate connections give 20 claims, 20 attempts and 20 charges;
  - 16 concurrent `ClaimDue` calls give one winner;
  - an expired lease cannot renew, complete or release, and a stale owner's late success rolls back after a reclaim;
  - timeout-after-success with a second eligible provider plus redelivery gives no second-provider attempt or charge;
  - guard unit cases;
  - a crash before record leads to reclaim, `unknown`, and a same-key resubmit with one charge;
  - evaluate leaves all table hashes unchanged and matches dispatch;
  - lifecycle drain;
  - 3× sequential plus 3× concurrent success delivery, and 3 concurrent first deliveries, each give one journal;
  - rollback after entries are inserted leaves all tables unchanged;
  - a late success after failed is rejected;
  - the ledger endpoint (int64 max, debit/credit accounts, workspace scope, pagination, 400s).
- `e2e/api/dispatch.spec.ts`, `e2e/api/ledger-posting.spec.ts`, `e2e/support/payments.ts` (new); `e2e/support/global-setup.ts`, `state.ts` — fast dispatcher env, scenario fixtures, harness control ops `inspectPayment` (read-only SQLite URI) and `redeliverDispatch`. `e2e/api/payment-acceptance.spec.ts` now allows 0–1 attempts right after acceptance, because dispatch now runs. `e2e/api/database.spec.ts` expects the seeded scenario methods.
- `docs/backend/flows.md` — implemented dispatch contract, outcome table, lookup command, guard, lifecycle, config and metrics. `docs/wbs.md` — verified boxes ticked and status updated. `e2e/README.md`, `README.md` — harness inspection and timing knobs.

**Verification**
- `go -C backend test -race -count=3 -v ./tests/` — pass: all 28 top-level tests (12 earlier plus 16 new) passed on each of 3 runs.
- `pnpm check` — exit 0: golangci-lint 0 issues, all Go packages ok, Vitest 23 passed, build passed, generated contracts current.
- `pnpm test:e2e` — **35 passed, 0 failed**: 9 new (5 dispatch, 4 ledger-posting) plus all 26 earlier. The first full run failed 2. `database.spec` expected only one seeded method; it now lists the scenario methods. A ledger assertion used the other-workspace key, which `auth-contract.spec` revokes earlier in the run; it now uses the reader key, and cross-workspace isolation is covered in Go.
- `pnpm lint:openapi` — valid (1 existing warning); `pnpm generate:api` — regenerated.

**Notes:**
- Not verified: "canceled payments have no journal" (no cancel endpoint until 2.4) and "invariant checker passes after the 2.4 suite".
- `payment.lookup` rows stay `pending` until the 2.4 worker registers a handler.
- An `unknown` attempt keeps `completed_at` NULL until it is resolved.
- E2E cannot exercise a second provider, because providers are unique per `(workspace, code)` and only the mock adapter exists. The cross-provider guard is proven in Go with a second adapter under code `stripe`.
- Metrics are in-process only; there is no exporter.

## 2026-10-31 — Ignore agent worktrees and generated Next.js types in the docs hook

**WBS:** process · **PR/commit:** feat/m2-m4-payment-correctness

**Changed**
- `.gitignore` — ignores `.claude/worktrees/`, where isolated agents run.
- `.claude/hooks/require-docs.sh` — skips `frontend/next-env.d.ts`, which `next dev` and `next build` rewrite.

**Verification**
- Hook run by hand with only these files changed → allowed.
## 2026-10-31 — Add payment-facing screens and dashboard read models

**WBS:** 3.1.1–3.1.13, 3.1 acceptance gate (partial), 3.1 E2E · **PR/commit:** worktree branch `worktree-agent-a4654110c4e35ccbc` (based on `fa41bac`)

**Changed**
- `backend/internal/transport/http/dashboard.go` (new) — `GET /v1/dashboard/summary` and `GET /v1/dashboard/payment-volume` (`window=24h|7d|30d`): workspace-scoped, per-currency `big.Int` sums as decimal strings (may exceed int64, never cross-currency), status counts, success rate with integer arithmetic, nearest-rank p95 of acceptance-to-terminal time; both statistics are `null` without succeeded/failed observations; `data_source: observed`. `GET /v1/payments/{id}/history`: append-only transitions, outbox events and the idempotency key masked to `****` + last four.
- `backend/internal/transport/http/handler.go` — one hook (`serveReadModels`) before payment routing.
- `backend/internal/transport/http/payments.go`, `backend/internal/storage/payment_repository.go` — list filters `q` (exact ID or merchant reference), `currency`, `rail`, `provider_id`, `created_from`/`created_to`, `amount_min`/`amount_max`, and `sort=created_desc|created_asc`; cursors encode their direction and are rejected for another sort.
- `backend/tests/dashboard_test.go` (new) — empty-window nulls, exact per-currency sums beyond int64, success rate/p95, workspace isolation, hourly buckets, masked history key, list filters and directional cursors.
- `backend/api/openapi.yaml`, `frontend/lib/api/schema.d.ts` (generated) — the new paths, list parameters and schemas (`MetricSummary`, `VolumePoint`, `CurrencyVolume`, `AggregateAmount`, `PaymentHistory`, `DataSource`, `MetricWindow`).
- `frontend/lib/api/client.ts` — `apiRequestWithMeta` (request/correlation IDs, client-generated `X-Request-ID`, timeout, network failures as status-0 `ApiError`, contract-mismatch errors, CSRF header from the readable `railvanta_csrf` cookie); `shouldRetryRead` also skips 501. `frontend/lib/api/schemas.ts` (new) — Zod validators typed against the generated contract.
- `frontend/lib/hooks/payments.ts` (new) — workspace+environment-scoped query keys; payments, payment, attempts, history, journal (501/404 → "not available"), methods, summary and volume reads; 3 s polling only while non-terminal.
- `frontend/lib/hooks/useCreatePayment.ts` (new) — one Idempotency-Key per intent fingerprint, kept in `sessionStorage` while unconfirmed (survives reload), replaced when the intent changes, cleared only on an API-confirmed 202; synchronous double-submit lock; mutation `retry: false`; accepted/unconfirmed/rejected outcomes.
- `frontend/lib/money/index.ts` — `formatAggregateMoney`, `parseMajorAmount`, `minorToMajorInput`, `compareMinor`, `shareOfPeak` (BigInt only).
- `frontend/app/page.tsx` — landing page with architecture strip, available/planned capability cards, copyable API example and sandbox notice. `frontend/app/dashboard/page.tsx`, `payments/page.tsx`, `payments/[id]/page.tsx` — overview, explorer and detail.
- `frontend/components/dashboard/Overview.tsx`, `charts/PaymentVolumeChart.tsx`, `payments/{PaymentExplorer,PaymentTable,CreatePaymentDialog,PaymentDetail}.tsx`, `ui/{query-state,tabs,copy-code}.tsx`, `ui/index.tsx`, `app/globals.css` — metric cards ("No observations"), needs-attention banner (links only), Recharts volume chart plotting a BigInt per-mille index per currency with text summary and exact data table, URL-driven filters/sort/cursor, labelled table scroll region, React Hook Form + Zod dialog (no card/bank fields), Summary/Attempts/Events/Journal tabs, state timeline, masked key, latest-read correlation ID, unknown-outcome notice without retry, loading/empty/filtered-empty/error (request ID + Retry)/stale states.
- `frontend/package.json`, `pnpm-lock.yaml` — `react-hook-form`, `@hookform/resolvers`, `recharts`.
- `frontend/tests/components/payments.test.tsx` (new), `money.test.ts` — unknown outcome (mocked API), journal unavailable, read error, key reuse/replacement, field errors, lost-response resend with the same key, exact table/chart values, retry policy; money parsing/formatting beyond 2^53.
- `e2e/ui/payments.spec.ts` (new), `e2e/support/browser.ts` (new), `e2e/support/{global-setup,state}.ts` — demo-session cookies; harness `stop`/`start` and an offline `mark-unknown` fixture. `e2e/ui/shell.spec.ts` — overview/payments are no longer placeholders; landing assertion updated.
- `README.md`, `docs/wbs.md` — CSRF cookie convention and new endpoints; verified 3.1 boxes ticked with open items noted.

**Verification**
- `go -C backend test ./tests/ ./internal/transport/...` — pass (4 new dashboard/list tests).
- `pnpm lint:openapi` — valid (existing `no-server-example.com` warning); `pnpm generate:api` — regenerated.
- `pnpm --filter @railvanta/frontend test` — 58 passed (4 files).
- `pnpm check` — exit 0 (lint, generated contracts, typecheck, all Go packages, 58 Vitest tests, Next.js build).
- `pnpm test:e2e` — 34 passed, 0 failed (8 new `ui/payments.spec.ts` tests plus all earlier api/ui specs).

**Notes:** Without the 2.2 dispatcher every payment stays `created`, so the E2E verifies the pending state and that the detail page mirrors the API, not a reached terminal state. `unknown` is produced only by the offline E2E fixture and a mocked component test. The correlation ID shown is the latest read's `X-Correlation-ID`; payments do not persist one. The table's rail comes from the payment-method list; provider appears only on attempts. Amount filters take minor units. Slow-API states and CI are unverified.

## 2026-10-31 — Add idempotent payment acceptance and payment reads

**WBS:** 2.1.1–2.1.12, 2.1 acceptance gate, 2.1 E2E · **PR/commit:** uncommitted

**Changed**
- `backend/internal/payment/status.go` — `Status` enum, `Statuses`, `ParseStatus`, `Terminal`, and pure `ValidateTransition(from, to)` (empty from = initial `created` only; `unknown` never returns to `processing`).
- `backend/internal/payment/money.go` — `ParseAmount`, `ValidateCurrency`, `CurrencyExponent`, sentinel `ErrInvalidAmount`/`ErrUnsupportedCurrency`; `ParseMinor`/`FormatMinor` keep their signatures.
- `backend/internal/payment/idempotency.go` — `OperationCreatePayment = "payment.create"`; `CanonicalRequest.Canonical()/Hash()`: versioned, length-prefixed fields (operation, amount, currency, method, merchant reference) hashed with SHA-256; field order and normalization documented.
- `backend/internal/payment/service.go` — `NewService(db, Options)` and `Accept`: one transaction that replays, rejects with 409, or validates the method and its token, reserves the key, inserts the payment, writes the NULL→`created` transition, appends `payment.dispatch` (`DispatchCommand{payment_id, workspace_id}`, dedup key `payment.dispatch:{id}`), and stores the 202 body. A lost unique race re-reads the winner. `SQLITE_BUSY` retries the whole transaction up to 4 times with full jitter, then returns `ErrBusy`. Test-only `FaultInjector` stages. `Get`/`List`/`Attempts` reads.
- `backend/internal/payment/transition.go` — `ApplyTransition(tx, Transition)`: validates the graph, compare-and-swap on status+version, appends the audit row (for 2.2–2.4).
- `backend/internal/payment/metrics.go`, `view.go` — in-process acceptance counters and a latency histogram (`Metrics.Snapshot`); `PaymentView`/`AttemptView` API DTOs with decimal-string money and RFC 3339 times.
- `backend/internal/storage/payment_repository.go` — workspace-scoped repository functions on the caller's `*gorm.DB`: payments (insert/get/keyset list/CAS status update), transitions, idempotency records, outbox insert, attempts list, method/token lookup; `ErrNotFound`, `ErrStaleVersion`, `IsUniqueViolation`, `IsBusy`.
- `backend/internal/transport/http/payments.go`, `handler.go` — `POST /v1/payments` (replaces the 501 stub; 202 + `Location`, `Idempotent-Replayed` on replay, 400/404/409/422/503 envelopes), `GET /v1/payments` (cursor, `limit`, `status`), `GET /v1/payments/{id}`, `GET /v1/payments/{id}/attempts`; `Config.Payments` lets tests inject a service. `handler_test.go` now expects 202 and uses distinct keys.
- `backend/api/openapi.yaml`, `frontend/lib/api/schema.d.ts` (generated) — payment operations documented as implemented; new `PaymentStatus`; `Payment` adds `merchant_reference`, `created_at` and `updated_at`; `Attempt` adds attempt number, rule, provider reference, error code and timestamps.
- `backend/internal/payment/*_test.go` — table-driven tests of all 42 (from, to) pairs, plus vocabulary, hash stability/sensitivity/boundary, and amount/currency tests.
- `backend/tests/helpers_test.go`, `backend/tests/payment_acceptance_test.go` — file-backed WAL integration tests for every acceptance-gate bullet, plus untokenized method (422), auth/header failures that write nothing, pagination/status filter, guarded transitions, and busy retry recovery/exhaustion with a real competing write lock.
- `e2e/api/payment-acceptance.spec.ts` — new spec for the 2.1 E2E block; `e2e/api/auth-contract.spec.ts` now expects 202 where M1 expected 501.
- `docs/backend/flows.md` — implemented acceptance contract table; `docs/wbs.md` — 2.1 boxes ticked and status updated.

**Verification**
- `go -C backend test -count=1 -v ./tests/ ./internal/payment/ ./internal/storage/ ./internal/transport/...` — pass, 62 PASS lines (12 top-level integration tests in `backend/tests`, 5 top-level payment unit tests, plus subtests).
- `go -C backend test -race -count=3 -run 'Concurrent|InitialTransition' ./tests/` — pass.
- `pnpm lint:openapi` — valid (1 existing `no-server-example.com` warning); `pnpm generate:api` — regenerated.
- `pnpm check` — exit 0 (golangci-lint 0 issues; all Go packages, including other agents' provider/routing/ledger at that moment, passed; frontend 23 tests passed; build passed).
- `pnpm test:e2e` — 26 passed, 0 failed (includes 6 new `payment-acceptance.spec.ts` tests and all earlier specs).

**Notes:** No migration was needed. The actor reference is `api_key:<role>` or `session:<role>` because `auth.Principal` has no user/key ID. Metrics are in-process only; no exporter or endpoint exists yet. The seed already tokenizes the demo methods (`mock_reference_*`); scenario-specific seeded methods for 2.2 are not added here. The `pending` idempotency branch returns `409 idempotency_in_progress` but cannot be observed through the single-transaction path.

## 2026-10-31 — Add provider interface, durable mock adapter and routing engine

**WBS:** 2.2.1, 2.2.2, 2.2.3, 2.2.4, 2.2.5 · **PR/commit:** uncommitted

**Changed**
- `backend/internal/provider/provider.go` (replaces `doc.go`) — `Provider` interface (`Capabilities`, `Submit`, `Lookup`), typed outcomes `succeeded`/`declined`/`ambiguous`, `SubmitRequest.Validate`, `ErrUnavailable` distinct from lookup "not found"; no payment-domain imports.
- `backend/internal/provider/mock/mock.go` — durable adapter over `mock_provider_charges` (local `Charge` model, own `*gorm.DB`, short own transactions, pauses outside any transaction). Scenario comes from the persisted token reference prefix (`mock_success`, `mock_decline`, `mock_delayed_success`, `mock_timeout_before_execution`, `mock_timeout_after_success`, `mock_outage`; anything else = success) and is persisted on the charge. Same key replays the original charge; injectable `Sleep`/`Now`; `SetAvailable` simulates whole-provider outage.
- `backend/internal/routing/routing.go`, `load.go` (replace `doc.go`) — pure `Decide`/`Eligible`/`Explain` over a `Snapshot` (token ∩ enabled provider ∩ enabled rule matching rail, currency, inclusive amount bounds; priority ascending, rule ID tie-break; caller exclusions reported as `excluded`), plus read-only workspace-scoped `Load`.
- `backend/internal/provider/provider_test.go`, `backend/internal/provider/mock/mock_test.go`, `backend/internal/routing/routing_test.go` — table-driven and file-backed SQLite tests.

**Verification**
- `go -C backend test -race -count=1 ./internal/provider/... ./internal/routing/...` — pass (3 packages, 54 test/subtest PASS lines with `-v`): every scenario, resubmit → one charge row, 8 concurrent submits → one row, blocked delayed submit allows a concurrent write (no open transaction), deadline honoured, restart durability, unavailable vs not found, routing ordering/bounds/eligibility, `Load` writes nothing and is workspace-scoped.
- `go -C backend vet ./internal/provider/... ./internal/routing/...` — pass; `gofmt -l` on both packages — clean.
- Not run: `pnpm check`, full `go test ./...`, `pnpm test:e2e` (other packages were being edited concurrently).

**Notes:** Not yet wired into a dispatcher or `POST /v1/routing/evaluate` (2.2.6+). Seeded tokens (`mock_reference_<method>`) map to `success`; scenario-specific seeded methods are for the seed/e2e owner. A `processing` delayed-success charge settles to `succeeded` on its next lookup or replay.

## 2026-10-31 — WBS 2.3 ledger service (2.3.1–2.3.4, 2.3.6, 2.3.7)

**WBS:** 2.3.1, 2.3.2, 2.3.3, 2.3.4, 2.3.6, 2.3.7, ledger-only parts of 2.3.9 · **PR/commit:** uncommitted

**Changed**
- `backend/internal/ledger/posting.go` — `Post` builds a draft journal, inserts entries, re-checks persisted totals with overflow-checked int64 sums and marks the journal posted, only on a caller-owned transaction (`ErrNoTransaction` otherwise). `PostPaymentSuccess` debits `provider_clearing:{provider_id}` and credits `merchant_payable` for the payment's own currency and amount, with posting key `payment:{id}:success`. A repeated key returns the stored journal (`AlreadyPosted`) only when currency, payment, reversal link and lines match; otherwise `ErrPostingMismatch`. `Reverse` posts a mirrored journal with `reversal_of` and key `journal:{id}:reversal`, checks the exact offset and rejects a second reversal (`ErrAlreadyReversed`).
- `backend/internal/ledger/accounts.go` — lazily creates per-workspace, per-currency accounts (`INSERT … ON CONFLICT DO NOTHING` on `(workspace_id, code, currency)`); the provider must belong to the workspace. Posting rejects accounts from another workspace or currency (`ErrAccountMismatch`).
- `backend/internal/ledger/verify.go` — `Verify`/`Check` report committed draft journals, unbalanced or under-two-entry posted journals, entry/journal workspace or currency drift, reversals that do not mirror their original, succeeded payments without exactly one matching success journal, and failed/canceled payments with one. Payments are read through a local struct.
- `backend/internal/ledger/ledger_test.go` — file-backed SQLite tests: success posting and account scheme; duplicate success delivered 3x sequentially and 3x concurrently over three connections → one journal, two entries; key mismatch; injected failure after entries are inserted → no journal, entries, accounts or payment change; service rejects unbalanced, single/no entry, mixed currency, cross-workspace, overflow, zero/negative amounts and bad sides; raw SQL shows the triggers/FKs reject unbalanced, single-entry, mixed-currency, cross-workspace, overflow-sum (SQLite `integer overflow`) and direct-posted journals; UPDATE/DELETE/INSERT on posted journals/entries fail as `immutable`; reversal nets accounts to zero, second reversal is rejected by both the service and `UNIQUE (reversal_of)`; verifier detects seeded violations.
- `backend/internal/ledger/doc.go` — package description.

**Verification**
- `go -C backend vet ./internal/ledger/...` — pass.
- `gofmt -l backend/internal/ledger` — no output.
- `go -C backend test -count=1 ./internal/ledger/...` — pass. `go -C backend test -race -count=3 -v ./internal/ledger/...` — all 9 tests and subtests pass on every run.
- Not run: `pnpm check`, `pnpm test:e2e` and the full Go suite (other agents are editing the tree). 2.3.5, 2.3.8 and the E2E spec are not part of this change.

**Notes:** No migration was needed; existing triggers and composite FKs cover the required rejections. SQL still allows a committed draft journal and does not check a reversal's economic meaning; `Verify` reports both.

## 2026-10-31 — Require docs/changelog updates via Claude Code hooks

**WBS:** process · **PR/commit:** uncommitted

**Changed**
- `.claude/settings.json` — `Stop` and `SubagentStop` hooks run `.claude/hooks/require-docs.sh`.
- `.claude/hooks/require-docs.sh` — if files outside `docs/` and `*.md` changed but `CODECHANGELOG.md` did not, blocks the agent from finishing and asks for a changelog entry plus updates to the affected docs. Skips re-runs when `stop_hook_active` is set, so it can't loop.
- `AGENTS.md` — documents the hook.

**Verification**
- Script run by hand: code changed without a changelog edit → `decision: block` listing the files. `stop_hook_active: true` → allowed. Code and changelog both changed → allowed.

## 2026-10-31 — Resolve dependencies and run the full M1 verification

**WBS:** 1.1–1.4, 5.2 (prep) · **PR/commit:** #1 (uncommitted follow-up)

**Changed**
- `pnpm-lock.yaml`, `backend/go.mod`, `backend/go.sum` — real dependency resolution from `pnpm install` and `pnpm deps:api`.
- `pnpm-workspace.yaml` — `allowBuilds: unrs-resolver: false`. Its prebuilt binary ships through optional deps, so no install script runs.
- `frontend/lib/api/schema.d.ts` — generated by `pnpm generate:api`.
- `frontend/**`, `e2e/**` — formatted by `pnpm format` (Prettier 3.9.10). `next build` updated `next-env.d.ts`.
- `frontend/postcss.config.mjs` — named default export, fixing an ESLint warning.
- `frontend/tests/components/shell.test.tsx` — dispatches a typed `cancel` Event (`fireEvent.cancel` isn't typed, so `tsc` failed).
- `frontend/components/layout/app-shell.tsx`, `frontend/app/globals.css` — fixes the axe `label-content-name-mismatch` violation (WCAG 2.5.3, serious):
  - The account `<summary>` had `aria-label="Account menu"` but visible text "Account". The aria-label is removed, so the name comes from the visible text.
  - On mobile the label is now visually hidden instead of `display: none`.
- `.gitignore` — ignores `*.tsbuildinfo`.

**Verification**
- `pnpm install` / `pnpm install --frozen-lockfile` — pass.
- `pnpm deps:api` — pass; `go.sum` created.
- `pnpm generate:api` — pass.
- `pnpm check` — **pass**:
  - Go tests: auth, config, payment, storage and transport/http all ok.
  - Vitest: 3 files, 23 tests passed.
  - Next build passed, and the client-bundle secret sentinels are clean.
  - The first runs failed on Prettier formatting, the ESLint warning and the `tsc` error, all fixed above.
- `pnpm test:e2e` — **20 passed, 0 failed**. The first run failed 1 test: the shell axe check at 1440px, fixed above.
- `pnpm compose:build` — both images built (Docker 28.5.1).
- `docker compose up --wait`:
  - api, web and proxy are healthy.
  - `GET /` → 200, `GET /dashboard` → 200, `GET /v1/session` → 401 (unauthenticated, as expected).
  - After `down`/`up` the `railvanta-data` volume is retained and the stack is healthy again.
- Not done: backup/restore and E2E against Compose (WBS 5.2), and GitHub Actions CI on the PR.

## 2026-10-31 — Commit the foundation work and open the M1 PR

**WBS:** 1.1–1.4, 5.2 (prep) · **PR/commit:** #1 (`feat/m1-foundation`), not merged

**Changed**
- Git history: an empty root commit on `main` serves as the PR base. The previously uncommitted work is split into chronological commits on `feat/m1-foundation` (docs, WBS, monorepo, backend storage, auth/API, frontend, E2E, CI, containers, project docs). Commit dates follow the dates of the entries below.

**Verification**
- `docker compose config -q` — exit 0. No other checks were rerun. The dependency-backed results in the entries below still apply, and CI on the PR is the first full run.

## 2026-10-31 — Add required backend/frontend Docker images and Compose workflow

**WBS:** 1.1.16, 5.2.1–5.2.4 · **PR/commit:** uncommitted

**Changed**
- `infrastructure/docker/{api.Dockerfile,web.Dockerfile,nginx.conf}` — separate multi-stage non-root API/frontend images, private API health check, Next.js standalone assets, and unprivileged same-origin proxy.
- `docker-compose.yml` — image/build definitions, health-ordered startup, host-loopback proxy port, sandbox-only API configuration, and a persistent SQLite volume mounted only by the API. Compose down retains the volume.
- `.dockerignore` — exclude credentials, local database files, dependencies and build/test output from image contexts.
- `frontend/next.config.ts` — standalone output with monorepo tracing root; Docker supplies a nonsecret API rewrite destination at build time.
- `package.json` — `compose:config`, `compose:build`, `compose:up`, `compose:down`, `compose:logs`; `pnpm check` includes daemon-independent Compose validation.
- `idea.md`, `README.md`, `AGENTS.md`, `docs/{architecture.md,README.md,wbs.md,operations/deployment.md}` — require separate images with Compose, explain native development/testing as an alternative, and document unresolved image/runtime gates. M1 now has 51/53 source tasks checked; backup/restore, container E2E, digest pinning and all runtime acceptance gates remain open.

**Verification**
- `pnpm compose:config` — PASS (Docker Compose 2.40.0).
- Parsed resolved Compose JSON — PASS: both separate image/build definitions, no host app/health ports, one API-owned data volume, sandbox settings, health dependency ordering, and loopback-only public origin.
- Local document links, WBS source-task count, and `git diff --check` — PASS.
- `docker info` — FAIL: sandbox denies access to the Docker daemon socket.
- `pnpm compose:build` — FAIL before building: sandbox denies writes to Docker Buildx activity metadata under `~/.docker`. No image-build, Nginx runtime, proxy, persistence or container-E2E pass is claimed.

**Remaining:** finish M1 dependency lock/checksum files, build and start images with Docker access, verify health/routing/volume permissions/persistence, and complete WBS 5.2 recovery and release gates. No Docker settings were changed and no resources were deployed.


## 2026-10-31 — Check implemented M1 tasks while retaining verification gates

**WBS:** 1.1–1.4 checklist · **PR/commit:** uncommitted

**Changed**
- `docs/wbs.md` — at the user's request, task checkboxes now track implementation separately from acceptance/E2E outcomes. Checked 50 of 52 M1 source tasks. Kept 1.1.15 open for passing root checks and 1.3.13 open for missing generated contracts. Added per-package totals; all package verification/merge gates remain open. M2 and later checklists are unchanged.

**Verification**
- Checklist consistency — PASS: 50 implemented M1 tasks; two partial tasks open; no M1 acceptance/E2E outcome marked passed.
- Local WBS links and `git diff --check` — PASS.
- Application tests — not rerun for this documentation-only update.


## 2026-10-31 — Detail M1 implementation and verification status in the WBS

**WBS:** 1.1–1.4 tracking · **PR/commit:** uncommitted

**Changed**
- `docs/wbs.md` — added task-level implementation evidence, recorded pass/fail/blocker results, the remaining root-command completion sequence, and distinct status descriptions for each M1 package. Kept original task/acceptance boxes unchecked because runtime, CI and merge gates remain unmet.

**Verification**
- Local file links in `docs/wbs.md` — PASS.
- `git diff --check` — PASS.
- Application tests — not rerun for this documentation-only update; the WBS labels previous results as historical observations.


## 2026-10-31 — Add M1 foundation source and verification harness

**WBS:** 1.1–1.4 · **PR/commit:** uncommitted; no PR, merge, or green CI claimed

**Authorization**
- The user explicitly requested M1 and all listed schema-review revisions. Items 1.2.1–1.2.8 are authorized for this implementation; no additional approval is needed to finish them. Five specialist agents contributed storage, auth/contracts, frontend, E2E, and CI/tooling, followed by parent integration review.

**Changed**
- `backend/go.mod`, `cmd/api/main.go`, `internal/config/`, and domain package documentation — Go module, explicit sandbox configuration, separate loopback health listener, readiness after migration, and bounded SIGTERM shutdown.
- `backend/internal/storage/`, `backend/migrations/`, and `docs/backend/{schema.sql,data-model.md,persistence.md}` — two embedded migrations, checksummed history, 23 STRICT tables, provider-neutral methods/tokens, durable mock/simulation storage, auth storage, immutable financial/audit records, connection verification, UUIDv7 IDs, explicit GORM models, transaction helper, and file-backed tests.
- `backend/internal/auth/`, `internal/transport/http/`, `internal/payment/`, and `backend/api/openapi.yaml` — hashed scoped session/API-key auth, expiry/revocation, roles, CSRF/origin checks, request/correlation IDs, bounded bodies/rate limits, safe errors/logging, exact money parsing/serialization, workspace/method reads, and self-revocation. Demo bootstrap refuses prefix collisions or silently changed credentials. Valid payment intents return honest `501` until M2.
- `backend/{.env.example,.golangci.yml,Makefile}`, `backend/tests/README.md` — placeholder configuration and formatting/vet/lint/build/test targets.
- `frontend/` — strict Next.js/React/TypeScript/Tailwind workspace, local Inter font, all planned routes plus detail placeholders, responsive accessible shell/dialog navigation, persisted themes, unavailable states, UI primitives, exact BigInt money helpers, Zod client, path-preserving API proxy, and Vitest/RTL tests. No invented financial metrics or enabled payment execution.
- `e2e/` — Playwright workspace with temporary on-disk database, runtime-generated credentials, free loopback ports, private harness-only restart/SIGKILL helpers, HTTP contract checks, controlled stopped-database corruption/durability fixtures, and scaffold/database/auth/responsive/keyboard/axe specs. Assertions use HTTP/browser interfaces; Go exposes no test-control endpoints.
- `package.json`, `pnpm-workspace.yaml`, `scripts/`, `redocly.yaml`, `.github/workflows/ci.yml`, `.gitignore` — root package commands, explicit installs, Go/OpenAPI lint, real contract-generation tooling, required-workspace/tracked-file checks, client-bundle secret sentinels, CI and E2E artifact upload. Ignore local pnpm cache and backend data.
- `README.md`, `AGENTS.md`, `idea.md`, `docs/{README.md,roadmap.md,wbs.md,frontend/design.md,operations/deployment.md}` — actual setup, implementation status, authorization record, checks and limits. WBS packages remain in progress and boxes remain unchecked until verified and merged.

**Verification observed**
- `GOCACHE=/tmp/railvanta-go-cache pnpm test:api:foundation` — PASS: configuration and exact-money test packages, including int64 serialization.
- `pnpm format:api` — PASS: Go source formatted.
- Independent Python/SQLite file-backed execution — PASS: all 22 rejection cases from storage tests, valid seed paths, migration 1→2 preserving existing data, 23 STRICT reference tables, `integrity_check=ok`, and no foreign-key violations. This is not a Go migration-runner pass.
- Go parser — PASS: 20 Go source files parse. TypeScript parser — PASS: 46 TypeScript/TSX/JavaScript source files parse. These are syntax checks, not dependency-backed compilation/typechecking.
- `node --check scripts/*.mjs` (each file checked separately) — PASS. Workspace and tracked-file policy scripts — PASS. OpenAPI YAML parses and internal references resolve across 20 paths; real Redocly lint remains unexecuted.
- Local documentation file links and `git diff --check` — PASS.
- `pnpm install --offline` — FAIL: missing cached Tailwind/PostCSS dependency metadata. Normal registry/Go downloads fail DNS resolution; the workspace permission policy rejected download escalation.
- `pnpm install --no-frozen-lockfile --config.fetchRetries=0` — FAIL: registry fetch for `openapi-typescript` is blocked; no updated lockfile was produced.
- `pnpm install --frozen-lockfile` — FAIL: existing lockfile lacks the new workspace/root dependencies. It must be regenerated with a real dependency install.
- `GOCACHE=/tmp/railvanta-go-cache GOMODCACHE=/tmp/railvanta-go-mod pnpm test:api` and `pnpm check` — FAIL: missing Go dependencies/checksums. The format, required-workspace and tracked-file steps passed before the dependency failure. An initial check without the temporary cache override also hit the sandbox's home-cache write restriction.
- `pnpm test:e2e` — FAIL before suite execution: workspace dependencies are not installed; the available global `playwright` executable does not provide the test runner. No browser test or axe result is claimed.
- Standard API type generator — BLOCKED: `openapi-typescript` is not installed. `frontend/lib/api/schema.d.ts` is not fabricated and remains to be generated.

**Remaining before M1 acceptance**
Enable dependency-download network access; run `pnpm install`, `pnpm deps:api`, `pnpm generate:api`, install Chromium, run `pnpm format`, then `pnpm install --frozen-lockfile`, `pnpm check` and the full `pnpm test:e2e`. Review/fix actual failures, retain the resulting lockfile/Go checksums/generated contracts, and record PR/CI evidence before checking WBS gates. No containers, cluster, real funds, or M2 execution were implemented.

## 2026-10-31 — Set up a pnpm monorepo workspace and per-step E2E gates

**WBS:** 1.1 (prep) · **PR/commit:** uncommitted (staged)

**Changed**
- `package.json` — root workspace manifest (private, `pnpm@11.3.0`, Node ≥ 22) whose scripts orchestrate both stacks:
  - Go: `dev:api`, `build:api`, `lint:api`, `test:api`, all via `go -C backend`
  - JS workspaces: `dev:web`, `build:web`, `lint:web`, `typecheck`, `test:web`, `test:e2e`
  - `check` runs lint, typecheck, test and build
- `pnpm-workspace.yaml` — workspaces `frontend` (`@railvanta/frontend`), `e2e` (`@railvanta/e2e`) and `packages/*`. Go stays outside the npm workspace graph.
- `pnpm-lock.yaml` — generated by `pnpm install` (no dependencies yet).
- `.gitignore` — ignores dependencies, build and test output, `.env*` except `.env.example`, and SQLite databases with their WAL/SHM sidecars.
- `.nvmrc` — Node 22.
- `docs/wbs.md`:
  - Every work package now ends with an **E2E test** block, a black-box spec under `e2e/`, and requires the full `pnpm test:e2e` suite to pass as a cumulative regression gate.
  - Standalone Playwright tasks were folded into those blocks.
  - 1.1 now creates the `e2e/` harness and switches its commands to pnpm.
- `AGENTS.md`, `docs/architecture.md`, `docs/frontend/design.md`, `docs/README.md` — updated to the monorepo layout, with E2E specs now in `e2e/`.
- `CODECHANGELOG.md` — this file.

**Verification**
- `pnpm install` — succeeded; created the lockfile and `node_modules/`.
- `pnpm run lint:web && pnpm run typecheck && pnpm run test:web` — exit 0. "No projects matched", as expected, since no JS workspaces exist yet.
- `pnpm run test:api` — exit 1 (`go: chdir backend: no such file or directory`). Expected: the Go module arrives in WBS 1.1.
- Link check across all Markdown files — 0 broken links.

**Notes:** `pnpm check` and `pnpm test:e2e` won't pass until WBS 1.1 scaffolds `backend/`, `frontend/` and `e2e/`.

## 2026-10-31 — Add the WBS checklist

**WBS:** planning · **PR/commit:** uncommitted (staged)

**Changed**
- `docs/wbs.md` — new checklist covering work packages 1.1–5.4 and the verification matrix. Schema-review items are marked as pending approval.
- `docs/roadmap.md`, `docs/README.md` — now link to the WBS instead of repeating its tables.

**Verification**
- Link check — 0 broken links. Every box is unchecked, since no implementation exists.

## 2026-10-30 — Reorganize the design docs

**WBS:** planning · **PR/commit:** uncommitted (staged)

**Changed**
- `docs/` — grouped into `backend/`, `frontend/` and `operations/`.
- Removed `progress.md` (its content merged into the README and roadmap) and `railvanta.sqlite` (rebuildable from `schema.sql`).
- Removed duplicated sections and per-file status disclaimers, and trimmed `idea.md` to a brief.

**Verification**
- Rebuilt `docs/backend/schema.sql` into a throwaway database with Python's SQLite 3.51.0: 15 tables, `integrity_check` returned `ok`, and `foreign_key_check` returned no rows.
- Link check — 0 broken links.
