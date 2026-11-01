# Verification matrix evidence

Maps every row of the WBS verification matrix (`docs/wbs.md`, "Verification matrix") to a named automated test or a recorded manual result. Paths are relative to the repository root. "Not covered" means no test exists yet; nothing here claims coverage that was not run. Commands and actual results are recorded in `CODECHANGELOG.md`.

Conventions: `backend/tests/` = cross-package Go integration tests (file-backed WAL SQLite created by `openTestStore`, the shared WBS 5.1.1 helper in `helpers_test.go`, which uses `storage.Open` for the production connection string and all embedded migrations and then re-asserts every PRAGMA, the migration history, foreign-key integrity and the single-connection pool). `frontend/tests/components/` = Vitest + React Testing Library. Playwright rows belong to WBS 5.1.8/5.1.9 (separate work) and are listed as pending, not as covered.

## Idempotency

| Row | Evidence (file :: test) | Status |
| --- | --- | --- |
| Concurrent requests with the same key | `backend/tests/payment_acceptance_test.go :: TestConcurrentSameKeyCreatesExactlyOnePayment` (50 goroutines over real HTTP; one row each in payments, idempotency_records, transitions and outbox; identical 202 bodies and `Location`). Repeat proof: `go -C backend test -race ./tests -run '^TestConcurrentSameKeyCreatesExactlyOnePayment$' -count=100` | Covered |
| Same key, changed payload | `payment_acceptance_test.go :: TestChangedRequestWithSameKeyConflicts` (409 `idempotency_conflict`, original row unchanged, counts stay 1); `backend/internal/payment/idempotency_test.go :: TestCanonicalRequestHash` | Covered |
| Lost response (client retries after commit) | Server: `payment_acceptance_test.go :: TestChangedRequestWithSameKeyConflicts` (reordered-body replay returns the stored response, `Idempotent-Replayed: true`) and `TestConcurrentSameKeyCreatesExactlyOnePayment` (replays). Client: `frontend/tests/components/payments.test.tsx :: create payment dialog > resends a lost submission with the same key and only reports API-confirmed acceptance` | Covered |
| API restart between requests | `payment_acceptance_test.go :: TestReplayAfterReopeningDatabase` (closes every connection, reopens the file, replays byte-identical response). A real process restart is not exercised here | Covered at the database-reopen level; OS-process restart not covered |

## Dispatch

| Row | Evidence | Status |
| --- | --- | --- |
| Crash before provider call | `backend/tests/crash_harness_test.go :: TestCrashInjectionResolvesSuccessAtEveryPoint/after_prepare`; `dispatch_test.go :: TestCrashAfterSubmitNeverChargesTwice`; `recovery_test.go :: TestStartupScanMovesStaleSubmittedToLookup` | Covered |
| Crash after possible provider acceptance | `crash_harness_test.go :: TestCrashInjectionResolvesSuccessAtEveryPoint/before_record` and `/before_resolve`; `crash_harness_test.go :: TestCrashAfterProviderDeclineResolvesFailed`; `dispatch_test.go :: TestUnknownPaymentIsNeverSubmittedToAnotherProvider`; `recovery_test.go :: TestRecoveryNeverCreatesAttemptAtAnotherProvider`, `TestCentralDemoUnknownResolvesThroughLookup`, `TestNoRecordIsNotProofAndReplayReusesTheKey` | Covered |
| Stale lease | `dispatch_test.go :: TestExpiredLeaseCannotCompleteAfterReclaim`, `TestConcurrentClaimOfOneRowHasOneWinner`, `TestRacingDispatchersClaimEachRowOnce` | Covered |

## Accounting

| Row | Evidence | Status |
| --- | --- | --- |
| Duplicate success result | `backend/tests/ledger_posting_test.go :: TestDuplicateSuccessDeliveryPostsOneJournal`, `TestConcurrentFirstSuccessDeliveryPostsOnce`; `backend/internal/ledger/ledger_test.go :: TestDuplicateSuccessDeliveryPostsOnce`, `TestDuplicateKeyMismatchIsRejected` | Covered |
| Rollback during posting | `ledger_posting_test.go :: TestRollbackMidPostingLeavesNothing`; `ledger_test.go :: TestRollbackMidPostingLeavesNothing` | Covered |
| Mixed currency | `ledger_test.go :: TestServiceRejectsInvalidJournals/mixed_currency` (service) and `TestDatabaseTriggersRejectInvalidJournals/mixed_currency_entry` (composite foreign key, `FOREIGN KEY` error) | Covered |
| int64 overflow | `ledger_test.go :: TestServiceRejectsInvalidJournals/overflow_sum`; `backend/tests/ledger_explorer_test.go :: TestLedgerOverflowIsExplicitNeverRounded`; `backend/internal/storage/storage_test.go :: TestConstraintRejections/out_of_range_money`; `frontend/tests/components/ledger.test.tsx :: ledger explorer > groups accounts per currency and reports overflow instead of a number` | Covered |
| Unbalanced journal / edits to posted rows (WBS 5.1.4) | `ledger_test.go :: TestServiceRejectsInvalidJournals`, `TestDatabaseTriggersRejectInvalidJournals`, `TestPostedRowsAreImmutableInDatabase`, `TestVerifyDetectsViolations` | Covered |

## Isolation

| Row | Evidence | Status |
| --- | --- | --- |
| Forged workspace IDs | `backend/tests/isolation_test.go :: TestForgedWorkspaceIDsAreRejectedOrIgnored` (workspace ID in body fields, `X-Workspace-ID` header, query string, foreign `/v1/workspaces/{id}` path), `TestOtherWorkspaceCredentialsNeverReadOrChangeForeignRows` (every list and detail read for payments, attempts, history, ledger, discrepancies, simulations, providers, methods, events, dashboard and settings, with the other workspace's API key and its browser session cookie; foreign cancel, resolve and webhook change nothing), plus `payment_acceptance_test.go :: TestCrossWorkspaceAccessReturnsNotFound`, `ledger_explorer_test.go :: TestLedgerWorkspaceIsolationAndReadOnly`, `events_test.go :: TestEventsAndWebhooksAreWorkspaceScopedAndAuthorized`, `dashboard_test.go :: TestPaymentHistoryMasksKeyAndIsWorkspaceScoped`, `reconciliation_test.go :: TestReconciliationRunValidationAndScoping`. The Playwright suite cannot test this (its other key is revoked), so Go is the evidence | Covered |
| Cross-workspace foreign keys | `backend/internal/storage/storage_test.go :: TestConstraintRejections/cross_workspace_method` and `/cross_workspace_token`, `/session_without_membership`; `ledger_test.go :: TestDatabaseTriggersRejectInvalidJournals` | Covered |
| Cache switch between workspaces | `frontend/tests/components/workspace-cache.test.tsx :: workspace-keyed query cache > never renders workspace A rows after the session switches to workspace B` and `> prefixes every key with workspace and environment` | Covered (unit level; no browser test of an actual sign-in switch, the demo has one session workspace) |

## UI

| Row | Evidence | Status |
| --- | --- | --- |
| Double submit | `payments.test.tsx :: create payment dialog > resends a lost submission with the same key ...` (double click sends one request, button disabled while submitting, retry reuses the key); `payments.test.tsx :: idempotency keys > keeps one key per unconfirmed intent and replaces it when the intent is edited` | Covered |
| Reload during pending payment | `payments.test.tsx :: payment detail > shows an unknown outcome with a reconciliation explanation and no retry action` and the create dialog test show no optimistic success; the reload itself (state re-read from the API) has no test | Partially covered; Playwright reload spec pending (WBS 5.1.8) |
| API outage | `payments.test.tsx :: payment detail > shows a read error with the request ID and a Retry action`, `retry policy > never retries reads on client errors and bounds other read retries`; `workspace-cache.test.tsx :: payments explorer loading, empty and error states > shows an honest read error with the request ID ...`; `ledger.test.tsx :: ledger explorer > shows a read error with a request ID and retry` (mutations are never retried: `retry: false`) | Covered at component level; Playwright API-offline spec pending (WBS 5.1.8) |
| Empty results | `ledger.test.tsx :: ledger explorer > shows an empty state and keeps the filters in the form`; `workspace-cache.test.tsx :: ... > shows a loading state first, then the empty state with no invented rows` | Covered |
| Loading state (WBS 5.1.7) | `workspace-cache.test.tsx :: ... > shows a loading state first ...` | Covered |
| Money above MAX_SAFE_INTEGER (WBS 5.1.7) | `frontend/tests/components/money.test.ts :: exact minor-unit money formatting`, `ledger.test.tsx :: journal detail > renders amounts beyond Number.MAX_SAFE_INTEGER exactly`, `payments.test.tsx` (9007199254740993 round trip) | Covered |
| Unknown-outcome display (WBS 5.1.7) | `payments.test.tsx :: payment detail > shows an unknown outcome ...`; `shell.test.tsx :: sandbox shell > shows uncertainty without a retry action` | Covered |
| Mobile keyboard use | Component level only: `shell.test.tsx :: sandbox shell > traps drawer focus and restores the trigger when closed`. The 375 px Playwright keyboard-only run is WBS 5.1.9 | Not covered (Playwright pending) |

## Operations

| Row | Evidence | Status |
| --- | --- | --- |
| Disk full | `backend/tests/operations_test.go :: TestDiskFullFailsWritesBoundedWithoutCorruption` (SQLite `max_page_count` forces real `SQLITE_FULL`; acceptance returns 503 + `Retry-After`, never 500; all four acceptance tables stay equal; `integrity_check` is `ok`; service resumes when space returns). `TestWritableProbeReportsDegradedButNotBusy` (readiness probe `storage.CheckWritable` reports degraded on a read-only database, not on lock contention). Backend fix made for this row: write failures now map to 503 instead of 500 in `internal/transport/http/payments.go`, and `/health/ready` in `cmd/api/main.go` calls `storage.CheckWritable`. | Covered for the error path, probe and recovery. Not covered: the `/health/ready` HTTP handler itself (lives in `package main`, no test) and a genuine ENOSPC on a full filesystem (page-limit simulation used instead) |
| SQLite busy | `payment_acceptance_test.go :: TestBusyRetryIsBoundedAndRecovers` (bounded; exhaustion returns 503 and commits nothing); `operations_test.go :: TestBusyDuringOutcomeRecordingDoesNotRepeatProviderCall` (lock held during the post-provider outcome transaction; retried as DB-only; one provider call, one charge, one attempt); `dispatch_test.go :: TestNoTransactionIsOpenDuringProviderCall` | Covered |
| Migration failure | `operations_test.go :: TestMigrationFailureKeepsDatabaseUnchangedAndStartupRefused` (invalid SQL midway, duplicate version, empty migration: database dump identical, no partial column or table; tampered history makes `storage.Open` fail, which aborts `run()` in `cmd/api` before any listener binds, so `/health/*` is never served; refused startup changes nothing); `storage_test.go :: TestMigrationFailureRollsBack`, `TestMigrationUpgradeDriftAndCompatibility`, `TestMigrationRefusesUnmanagedDatabase` | Covered. "API stays unready" is shown by the process failing before listening, not by a probe against a running process |
| Restore | Backup and restore tooling and drill belong to WBS 5.2 | Not covered |

## Remaining gaps

- Playwright rows (reload during pending, API-offline UI, 375 px keyboard-only) and CI: WBS 5.1.8-5.1.11.
- `/health/ready` handler test and a real full-filesystem test.
- Restore drill: WBS 5.2.
