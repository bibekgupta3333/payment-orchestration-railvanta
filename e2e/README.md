# Black-box M1 tests

Run `pnpm test:e2e` from the repository root after installing dependencies and Chromium (`pnpm --filter @railvanta/e2e exec playwright install chromium`). Go with CGO/C compiler support and Python 3 are required.

Global setup builds the Go binary, creates a fresh temporary SQLite database, generates ephemeral credentials, and starts the API and Next.js on free loopback ports. API specs also use Next.js to verify the `/v1` proxy. Teardown stops both process groups and deletes only the harness temporary directory. No existing local database is used.

The authenticated loopback control server belongs to the test harness, not the application. `restartApi()` sends SIGTERM; `killApi()` sends SIGKILL. Both reopen the same database. A durability fixture inserts an unseeded payment method while the API is stopped, so startup reseeding cannot mask lost data. The checksum test prepares controlled corruption while the API is stopped, observes startup failure, restores the fixture, and restarts the API. Test assertions use HTTP or the browser, not database reads.

The temporary state file contains generated secrets and is created with mode `0600`. Do not copy it into reports or commit it. Tests use one worker and no retries because lifecycle tests deliberately restart shared processes. API responses are checked against the OpenAPI schemas. Payment execution is outside M1; a valid payment intent must currently return the documented `501` unavailable response.
