# ADR 0001: Go, GORM, SQLite, and Next.js Sandbox

Status: selected for this documentation baseline by the latest user request; implementation pending.

## Context

Earlier planning drafts described Kotlin/Spring Boot with PostgreSQL, an optional Go backend, and a Next.js frontend. The consolidated [project brief](../../idea.md) records the selected baseline. The current repository has no application to preserve. The latest request explicitly names GORM, SQLite, Next.js, Tailwind UI, and Kubernetes diagrams.

## Decision

Design one Go modular monolith using GORM and versioned SQL migrations against SQLite. Keep an outbox dispatcher inside that process. Use a separate Next.js App Router frontend with TypeScript and Tailwind CSS. Use Tailwind UI components only with an appropriate license; custom accessible Tailwind components are the fallback.

Keep the initial environment mock-backed and sandbox-only. SQLite is the local and single-node reference persistence layer. Kubernetes is an optional later sandbox deployment, with one API StatefulSet replica and a persistent volume. Do not provision external services as part of design documentation.

Preserve financial correctness requirements from the brief: durable idempotency, exact monetary arithmetic, auditable state changes, immutable balanced postings, explicit unknown outcomes, and evidence-based reconciliation.

## Consequences

- Go replaces the proposed Kotlin application; Gradle/Flyway instructions in the original brief are not implementation requirements for this baseline.
- SQLite replaces PostgreSQL for the sandbox. Database-specific locking and migration behavior require dedicated concurrency tests.
- One database writer and a single API owner simplify local execution but limit throughput and availability.
- Outbox leases and stable provider keys provide recoverability; external effects still require provider-specific duplicate protection and reconciliation.
- Horizontal backend scaling, real integrations, capture/refund lifecycle, and production availability remain separate future decisions.
- Tailwind UI licensing must be resolved before copying its commercial components. Documentation and original wireframes require no purchase.

## Alternatives

Kotlin/PostgreSQL remains a valid larger-system approach but conflicts with the latest stack selection. Go/PostgreSQL is the natural future option if concurrent writers or multi-node operation become necessary. Separate microservices and a broker add operational work without improving the first mock payment's correctness.

See [architecture](../architecture.md), [persistence](../backend/persistence.md), and [deployment](../operations/deployment.md) for the resulting boundaries.
