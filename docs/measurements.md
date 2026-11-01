# Measurements

Measured results for the Railvanta sandbox (WBS 5.4.5). Every number below came from a real run of the commands shown; nothing is projected. Rerun the scripts to compare, and treat the numbers as a local sandbox baseline, not a performance claim.

## Environment

| Item         | Value                                                                                                                                                                                                                                   |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Date         | 2026-11-01 UTC (evening of 2026-10-31 local)                                                                                                                                                                                            |
| Commit       | `5b33a29` (tip of `feat/m5-m6-verification-delivery`), plus the uncommitted measurement scripts in `scripts/measure-*.mjs`                                                                                                              |
| Machine      | Apple M4 Pro, 12 cores, 24 GiB RAM, Darwin 25.4 (macOS 26.4), arm64                                                                                                                                                                     |
| Toolchain    | Go 1.26.2, Node v22.18.0, pnpm 11.3.0, Playwright Chromium                                                                                                                                                                              |
| Run mode     | Native processes (no Docker): the Go API binary on a temporary file-backed SQLite database (WAL), mock provider, sandbox demo mode. Client and server share one machine over loopback.                                                  |
| API settings | `DISPATCHER_POLL_INTERVAL=50ms` (default 250ms), `DISPATCHER_MAX_IN_FLIGHT` default 4, `MOCK_DELAYED_SUCCESS_DELAY=2s`, `MOCK_TIMEOUT_DELAY=200ms`, `PROVIDER_TIMEOUT=30s`, `RECOVERY_BASE_BACKOFF=100ms`, `RECOVERY_MAX_BACKOFF=500ms` |
| Not measured | Docker Compose, Kubernetes, other machines or operating systems, real providers, network latency, multiple processes                                                                                                                    |

## Caveats that apply to every number

- One SQLite writer: all writes (accept, dispatch, outcome, ledger posting) serialise on one database file, so results describe a single-writer design on a fast local SSD. They say nothing about other storage, other machines or a networked database.
- The mock provider answers in-process with a database write and no network, so "provider time" is close to zero except where a scenario adds a deliberate delay.
- The API limits each client address to 600 requests per fixed one-minute window (`RequestsPerMinute: 600` in `backend/cmd/api/main.go`, not configurable). The measurement harness stays under it (540 requests per 61 s) by waiting between phases, and reads payment status from the database instead of polling the API. A real client above this rate gets `429 rate_limited`; the first attempt at these measurements did hit it.
- Server-side durations come from the audit transition timestamps, which have 1 ms resolution, so values of 1–3 ms carry roughly +/-1 ms error.
- Sample sizes are small (N=200 accepts per phase, 5 rounds per same-key level). Percentiles from 200 samples are indicative; p99 is the second-largest sample.

## (a) API latency and concurrency

Reproduce: `node scripts/measure-latency.mjs 200` (builds the API, boots it on a temp database, takes about 4 minutes because of the request budget, prints JSON; script and harness: `scripts/measure-latency.mjs`, `scripts/measure-lib.mjs`).

Acceptance latency is the client-observed round trip of `POST /v1/payments` with a fresh `Idempotency-Key` (the API returns 202 after one database transaction that inserts the payment, its first audit transition and the dispatch command).

| Measurement                                                                              | n   | p50     | p95     | p99     | max     |
| ---------------------------------------------------------------------------------------- | --- | ------- | ------- | ------- | ------- |
| Acceptance, sequential (1 client)                                                        | 200 | 1.3 ms  | 2.5 ms  | 2.9 ms  | 3.4 ms  |
| Acceptance, concurrency 1                                                                | 200 | 1.3 ms  | 2.9 ms  | 3.9 ms  | 6.0 ms  |
| Acceptance, concurrency 8                                                                | 200 | 3.9 ms  | 10.5 ms | 13.5 ms | 13.7 ms |
| Acceptance, concurrency 32                                                               | 200 | 11.7 ms | 30.7 ms | 38.1 ms | 47.2 ms |
| Time to `succeeded`, idle API, one payment at a time (server-side, created to succeeded) | 100 | 1 ms    | 2 ms    | 3 ms    | 3 ms    |
| Time to `succeeded` when 200 payments were accepted at concurrency 1 (server-side)       | 200 | 3 ms    | 6 ms    | 9 ms    | 12 ms   |
| Time to `succeeded` when 200 payments were accepted at concurrency 8 (server-side)       | 200 | 1002 ms | 2145 ms | 2246 ms | 2248 ms |
| Time to `succeeded` when 200 payments were accepted at concurrency 32 (server-side)      | 200 | 1148 ms | 2257 ms | 2362 ms | 2408 ms |

Accept throughput during the 200-request bursts (client side, includes the client's own overhead): 684/s at concurrency 1, 1646/s at concurrency 8, 2231/s at concurrency 32. All 200 payments in each burst reached `succeeded`; the last one did so 433 ms, 2405 ms and 2528 ms after the burst began.

Reading it: acceptance is far faster than execution under bursts, so payments queue behind the dispatcher (at most 4 in flight, each a few database writes on the one SQLite writer). That queueing is why time-to-terminal climbs to 1–2 s at concurrency 8 and 32 while acceptance stays in milliseconds. This is the intended decoupling, but it also shows the dispatcher, not the HTTP layer, is the throughput limit in this configuration.

### Concurrent requests with the same idempotency key

Each round sends the same `Idempotency-Key` and body from `concurrency` simultaneous clients, waits for the payment to finish, then counts provider charges and journals from the database. 5 rounds per level, `success` mock method.

| Concurrency | Requests | Distinct payment IDs | HTTP statuses | Max provider charges | Journals per round |
| ----------- | -------- | -------------------- | ------------- | -------------------- | ------------------ |
| 2           | 10       | 1 per round          | 202 x 10      | 1                    | 1                  |
| 8           | 40       | 1 per round          | 202 x 40      | 1                    | 1                  |
| 32          | 160      | 1 per round          | 202 x 160     | 1                    | 1                  |

Every duplicate got the same 202 response and the same payment; none created a second payment, charge or journal. The e2e spec `api/payment-acceptance.spec.ts` ("20 concurrent same-key requests produce exactly one payment") and the Go tests cover the same property with their own assertions; this table is an additional measured run, not a replacement. The 100-repeat gate in the WBS verification matrix is not met by this measurement (5 rounds per level here).

## (b) Recovery outcomes by mock scenario

Reproduce: `node scripts/measure-recovery.mjs` (prints JSON). One payment of 31000 minor units per scenario through the real API, dispatcher, recovery worker and ledger. "Time to resolution" is the server-side gap between the `created` and the terminal audit transition. Journal and charge counts are read from the database after settling.

| Scenario                 | Final payment | Attempt(s)    | Provider charges          | Journals | Time to resolution               | Notes                                                                                                                        |
| ------------------------ | ------------- | ------------- | ------------------------- | -------- | -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| success                  | `succeeded`   | 1 `succeeded` | 1                         | 1        | 2 ms                             | created, processing, succeeded                                                                                               |
| decline                  | `failed`      | 1 `failed`    | 1 (a declined charge row) | 0        | 2 ms                             | no journal, no further attempt                                                                                               |
| delayed success          | `succeeded`   | 1 `succeeded` | 1                         | 1        | 2003 ms                          | provider answers after the 2 s mock delay                                                                                    |
| timeout before execution | `unknown`     | 1 `unknown`   | 0                         | 0        | not terminal after a 30 s window | 51 lookups, all pending; the provider never executed, so the outcome stays unknown; no retry offered (`retry_allowed=false`) |
| timeout after success    | `succeeded`   | 1 `succeeded` | 1                         | 1        | 205 ms                           | created, processing, unknown, succeeded; resolved by 1 lookup at the original provider (`lookup_status=resolved`)            |
| outage                   | `unknown`     | 1 `unknown`   | 0                         | 0        | not terminal after a 15 s window | 40 lookups, all pending; the stack waits and never switches provider                                                         |

The two `unknown` rows are the designed behavior, not a failure to finish: a payment whose provider never reports an outcome stays `unknown` and visible to an operator rather than being retried elsewhere. The windows were chosen by the script defaults (30 s and 15 s); a longer window was not tried, so "never resolves" is only shown for those windows. Whether an outage payment resolves once the provider returns was not measured here.

The central demo (timeout after success) is also scripted end to end with assertions: `node scripts/measure-central-demo.mjs`. Its recorded run: accepted 202; `created -> processing -> unknown -> succeeded` (unknown at +202 ms, resolved at +205 ms); 1 attempt; 1 provider charge; 1 journal with debits 31000 = credits 31000; recovery `lookup_status=resolved`, 1 lookup, `retry_allowed=false`.

## (c) Test counts

All on the same commit and machine as above, native, run sequentially after the measurements.

| Command                                                         | Result                                                                                                          |
| --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `go -C backend test -race -count=1 ./... -v`                    | exit 0; 12 packages `ok`; 260 `--- PASS` lines (134 top-level tests plus 126 subtests); 0 FAIL, 0 SKIP          |
| `pnpm test:web` (Vitest)                                        | 8 files, 95 tests passed                                                                                        |
| `CI=1 pnpm run test:e2e:ci` (Playwright, `--retries=0`, native) | 88 passed, 5 skipped (the container-only `e2e/ops` specs, which skip without `E2E_BASE_URL`), 0 failed, 3.3 min |
| `pnpm test:e2e <10 case-study spec files> --retries=0`          | 51 passed, 0 failed (see [case-study.md](case-study.md))                                                        |

Not run by this task: the same E2E suite against the Docker Compose stack (recorded earlier in [CODECHANGELOG.md](../CODECHANGELOG.md): 92 passed, 1 skipped), and any GitHub Actions workflow. No CI result is claimed here.
