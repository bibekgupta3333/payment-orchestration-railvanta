# ADR 0007: API-owned health on a private loopback listener

Status: accepted (implemented in `backend/cmd/api/main.go`, `config`, Dockerfiles and Kubernetes probes).

## Context

The design proposed `/health/startup`, `/health/live` and `/health/ready` probes and said to keep health endpoints private, without saying where they listen. Serving them on the public `HTTP_ADDR` would expose them through the proxy and share the API's connection limits and rate limiter, and a saturated or draining public listener would also hide health.

## Decision

The API process serves health on its own listener, `HEALTH_ADDR` (default `127.0.0.1:8081`). Config rejects any non-loopback value. The Nginx proxy returns 404 for `/health/*`, no health port is published, and container health checks and Kubernetes exec probes run `wget` against the loopback port inside the container.

Readiness fails when the process is draining, when the database cannot be pinged, or when `storage.CheckWritable` fails; a merely busy database stays ready. Liveness and startup return 200 once the process serves. On SIGTERM the API becomes unready, stops claiming outbox work, drains bounded in-flight handlers, shuts both servers and closes SQLite.

## Consequences

- Health is invisible to the public origin, and probes need container exec (no `httpGet` probe, no external monitor without a sidecar).
- The Compose and Kubernetes images must include `wget`.
- Readiness checks the database only; provider state never makes the API unready.
- The `/health/ready` handler lives in `package main` and has no unit test; it is covered by container and E2E checks only.
