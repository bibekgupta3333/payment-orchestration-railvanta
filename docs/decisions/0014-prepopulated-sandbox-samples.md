# ADR 0014: Prepopulated sandbox samples on the demo stack

Status: accepted (the startup seeding, `GET /v1/sandbox/samples` and the hub story panel are implemented alongside this ADR; the presenter script is [docs/demo/story.md](../demo/story.md)).

## Context

- A presenter needs the dashboard to tell the project's story from the first click: why payment orchestration is hard and what each guarantee proves. An empty sandbox shows "No payments yet" and leaves that context to slides.
- ADR 0013 added one-click default demos and the hub's **Load sample data**, but they run only when someone clicks, take about 20 seconds, and depend on the browser session working during the presentation.
- The data must stay honest. The point of the sandbox is evidence (attempts, journals, inbox rows, discrepancies) produced by the real pipeline, not screenshots or fixtures.

## Decision

1. **Startup seeding behind a flag.** A new flag `SANDBOX_SAMPLE_DATA` (default `false`) makes the API run eight sample scenarios once at startup: `success`, `decline`, `duplicate_request`, `timeout_after_success`, `outage`, `duplicate_posting`, `duplicate_webhook`, `settlement_mismatch`.
2. **Demo workspace and demo stack only.** The samples are created in the shared demo workspace, and only `docker-compose.demo.yml` (`pnpm demo:up`) turns the flag on. The API binary, `docker-compose.yml`, the E2E setups and the Kubernetes manifests leave it off. The API refuses the flag unless `DEMO_MODE` or `SANDBOX_PUBLIC_ACCESS` is also on (the Compose demo keeps `DEMO_MODE` off and creates the demo workspace afterwards with `railvanta-api seed-demo`), and the seeder waits until that workspace's scenario payment methods exist before it starts.
3. **Idempotent by seed.** Each sample uses the seed `sample-<scenario>`. If a run with that seed already exists, it is skipped, so restarts do not add more payments. Deleting the volume (`pnpm demo:down -v`) removes them and the next start seeds them again.
4. **Genuine evidence.** Seeding calls the real simulation runner, which accepts payments through the real acceptance path and lets the live dispatcher, recovery worker, ledger and reconciliation produce the results. No rows are written directly. Runs and their payments are labelled Simulated like any other simulation.
5. **Discoverable.** `GET /v1/sandbox/samples` lists the sample runs; the dashboard hub's "The story: what this sandbox proves" panel links each one to its run (`/dashboard/simulator?run=<id>`) and payment, and the landing page explains the problem each one illustrates.

## Alternatives

- **SQL fixtures or a seeded database file.** Rejected: hand-written rows would only look like evidence. The invariants on the run page would be checking data that no code path produced, and fixtures would drift from the schema and the pipeline.
- **Client-only button (the existing Load sample data).** Kept as the fallback. It needs a click, a working session and about 20 seconds during the presentation, so on its own it does not give a populated sandbox on first open.
- **Seeding on every startup in every environment.** Rejected: it would add payments to developer databases and E2E runs that assert exact counts.

## Consequences

- Starting the demo stack creates about eight payments, with their attempts, journals, inbox rows and one reconciliation discrepancy, in the demo workspace, within about a minute of startup (two scenarios intentionally wait about 6 seconds for the recovery delay). They are visible to every visitor of the shared workspace.
- E2E and other environments are unaffected because the flag is off there; the specs keep their exact-count assertions.
- If the samples are missing (an older image, the flag off, or a failed startup run), the presenter can still use **Load sample data**, which runs the same eight scenarios from the browser.
- Seeds are fixed, so the sample amounts and webhook repeat counts are the same on every fresh stack.
