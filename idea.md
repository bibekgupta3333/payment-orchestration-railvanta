# Railvanta — Multi-Rail Payment Orchestration Platform

**Tagline:** One API. Every rail. Complete control.

**Status:** M1 scaffold, storage/auth foundation, and frontend shell source are added; full verification is pending. Payment execution is not implemented. See [setup](README.md), [docs](docs/README.md), and [actual results](CODECHANGELOG.md).

## Idea

Railvanta is a sandbox payment platform. It:

- routes payments through provider adapters
- prevents a payment from running twice
- records balanced double-entry accounting
- resolves payments whose outcome is uncertain

A developer API and an operations dashboard make all of this visible. The project demonstrates payment-infrastructure engineering: financial correctness, fault recovery, domain boundaries and evidence-based testing. It starts with a deterministic mock provider. Authorized external sandbox providers may follow later.

## Users

- **Developers** integrate one API and inspect idempotency, request errors and provider outcomes.
- **Payment operators** inspect routing, attempts, provider health, events, journals and discrepancies.
- **Technical reviewers** run repeatable failure scenarios and inspect the resulting state and accounting evidence.

**Central demo:** a provider succeeds but its response is lost.

1. Railvanta accepts a sandbox payment.
2. The provider's response is lost, so the payment shows an `unknown` outcome.
3. Railvanta resolves the outcome against the original provider.
4. The result is exactly one balanced journal and no second charge.

## Stack

| Layer | Choice |
| --- | --- |
| Backend | Go modular monolith: HTTP API plus an in-process outbox dispatcher |
| Persistence | GORM, SQLite (WAL), explicit versioned SQL migrations |
| Frontend | Next.js App Router, React, TypeScript, Tailwind CSS |
| Client data and forms | TanStack Query, React Hook Form, Zod |
| Charts and icons | Recharts, Lucide React |
| Testing | Go tests with file-backed SQLite, Vitest, React Testing Library, Playwright |
| Contracts | Versioned REST API (`/v1`) with an OpenAPI specification |
| Delivery | Separate backend and frontend Docker images, run together with Docker Compose. Kubernetes is optional later |

The stack rationale and rejected alternatives are in [ADR 0001](docs/decisions/0001-sandbox-stack.md). Module boundaries are in [architecture](docs/architecture.md).

## Required container workflow

Build the Go backend and Next.js frontend as separate images (`railvanta-api` and `railvanta-web`). Docker Compose is the standard local sandbox runtime; native processes remain available for development and tests. Compose must provide one browser origin through a proxy, preserve `/v1` paths, wait for health checks, and retain SQLite in a named volume mounted only by the single API container. Run both application containers as non-root users. Exclude credentials and local database files from image build contexts. Successful image builds, startup, proxy routing and data persistence must be verified before declaring the container workflow complete.

## Core guarantees

- **Idempotent acceptance.** `POST /v1/payments` requires `Idempotency-Key`.
  - The same key with the same request replays the stored `202` response.
  - The same key with a changed request returns `409`.
- **Provider calls never run inside a database transaction.** Each attempt and its stable provider request key are saved before submission.
- **A timeout is not a failure.**
  - Uncertain attempts become `unknown` and are resolved only against the original provider.
  - There is no automatic cross-provider retry.
- **Exact money.** Amounts are int64 minor units, sent over the API as decimal strings. Floating point is never used for money.
- **Balanced, immutable ledger.**
  - Each journal is balanced and in one currency.
  - A success commits together with its journal.
  - Corrections use reversal journals.
- **Honest reconciliation.** Discrepancies are recorded with evidence and never hidden by invented adjustments.

The details are in [flows](docs/backend/flows.md) and the [data model](docs/backend/data-model.md).

## Proposed API

| Area | Endpoints |
| --- | --- |
| Payments | `POST /v1/payments`, `GET /v1/payments`, `GET /v1/payments/{id}`, `GET /v1/payments/{id}/attempts` |
| Dashboard | `GET /v1/dashboard/summary`, `GET /v1/dashboard/payment-volume` |
| Routing | `GET /v1/routing/rules`, `POST /v1/routing/evaluate` |
| Providers | `GET /v1/providers/health` |
| Ledger | `GET /v1/ledger/transactions` |
| Reconciliation | `GET /v1/reconciliation/runs`, `GET /v1/reconciliation/discrepancies` |
| Events and simulation | `GET /v1/events`, `POST /v1/sandbox/simulations` |

Pagination, errors, authentication and authorization must be specified in OpenAPI before dependent screens are built. Capture, refunds, provider webhooks, rule editing and reconciliation commands each need their own contract before they are enabled.

## Screens

| Route | Purpose |
| --- | --- |
| `/` | Product explanation, architecture, API example, sandbox entry |
| `/dashboard` | Volume by currency, success rate, latency, recent payments, alerts |
| `/dashboard/payments` | Search, filters, pagination, sandbox creation, detail |
| `/dashboard/routing` | Rule inspection and non-executing evaluation |
| `/dashboard/providers` | Capabilities, observations, safe diagnostics |
| `/dashboard/simulator` | Backend-driven fault injection and invariant inspection |
| `/dashboard/ledger` | Accounts, journals, debit/credit lines |
| `/dashboard/reconciliation` | Runs, discrepancies, evidence, resolutions |
| `/dashboard/events` | Event processing, deduplication, correlation IDs |
| `/docs` | API examples, authentication, idempotency, errors, retry safety |
| `/dashboard/settings` | Theme and supported sandbox configuration |

The Sandbox badge is always visible. Metrics come from real API observations or are labelled as seeded/simulated. Capabilities that aren't built yet show an "unavailable" state. A payment is never shown as successful before it is, and the UI never offers a retry on an unknown outcome. See the [frontend design](docs/frontend/design.md) and [wireframes](docs/frontend/wireframes.md).

## Roadmap

1. **Foundation:** scaffolding, contracts, workspace authorization, migrations, UI shell.
2. **First correct payment:** idempotent create/read, persisted routing, mock provider, durable dispatch, atomic ledger posting, restart recovery.
3. **Payment operations UI:** overview, payments, routing, providers and simulator, all wired to real APIs.
4. **Financial operations:** ledger, reconciliation, events, developer docs, settings.
5. **Delivery:** correctness and browser tests, containers, CI, backup/restore drill.
6. **Optional:** Kubernetes, external test-mode providers, richer payment lifecycles.

Work packages and gates are in the [roadmap](docs/roadmap.md).

## Done when

The initial sandbox is done when it:

- runs with documented commands
- creates one logical payment under duplicate requests
- recovers uncertain outcomes safely
- produces one balanced immutable journal
- exposes discrepancies
- passes the relevant automated tests

Every screen must reflect actual backend behavior.

Portfolio deliverables: the working sandbox, OpenAPI contract, reproducible tests and demo scenarios, diagrams and ADRs, CI and containers, measured results, and a short case study. Claim only behavior that has been implemented and verified.

## Concepts practiced

Modular monoliths, provider adapters, idempotency, ACID transactions, state machines, transactional outbox/inbox, optimistic concurrency, durable leases, at-least-once delivery, double-entry accounting, reconciliation, observability and operational recovery.
