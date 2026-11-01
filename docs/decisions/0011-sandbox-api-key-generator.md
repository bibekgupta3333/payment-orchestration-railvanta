# ADR 0011: Sandbox API key generator in Settings

Status: accepted (implemented in `backend/internal/transport/http/apikeys.go` and `frontend/components/settings/ApiKeys.tsx`).

## Context

Until now API keys could only be provisioned out of band (the demo seed, or `railvanta-api seed-demo`). The design docs and a WBS acceptance gate said the Settings page must not offer key generation, because an unbacked "Generate key" button would be a fake control. For a portfolio demo that rule gets in the way: a reviewer who opens the sandbox has no way to obtain a key for the curl examples without the operator's terminal.

## Decision

Make key generation real, not decorative, with a small and bounded design:

- `POST /v1/api-keys` (operator) mints a `prefix.secret` key. The 4-byte public prefix and 24-byte secret come from `crypto/rand`. Only the SHA-256 hash is stored, exactly like seeded keys; the secret is returned once in the 201 response (`Cache-Control: no-store`) and never again.
- `GET /v1/api-keys` (operator) lists metadata only (name, prefix, role, status, timestamps). `DELETE /v1/api-keys/{id}` (operator) revokes a key of the caller's workspace, idempotently; another workspace's key answers 404.
- A minted key is workspace-scoped, has role `read` or `operator`, and always expires (default 24 hours, at most 30 days). A workspace may hold at most 20 active keys (409 `api_key_limit`). Names are restricted to a safe character set.
- The existing write rules apply unchanged: only an operator can mint, and cookie sessions also need the CSRF token and origin.
- The Settings page gets a generator form, a one-time "copy this now" panel with a curl example, and a table with a two-step revoke. The secret exists only in component state. Member roles and live activation stay unavailable.

## Consequences

- The WBS gate "no fake key generation" still holds in spirit (the control is real); the wording that key generation is absent or unavailable is superseded and annotated in `docs/wbs.md`.
- An operator key can mint further operator keys. That adds no authority beyond what an operator already has, but it means a leaked operator key can outlive its own revocation by minting siblings; the 20-key cap, expiry and the workspace list bound that, and there is no per-operator audit trail beyond a log line with the key ID and prefix.
- There is no key rotation, scoping beyond read/operator, or per-key rate limit. The unauthenticated landing page cannot mint keys; a visitor needs an operator session first (for the demo, `pnpm demo:open`).
- Sandbox only: the keys authorize the sandbox API and no provider credentials or real funds are involved.
