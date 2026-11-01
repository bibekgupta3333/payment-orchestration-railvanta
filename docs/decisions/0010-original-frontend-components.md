# ADR 0010: Original Tailwind components, no icon library, read-only shell

Status: accepted (implemented in `frontend/`, WBS 1.4, 3.x, 4.x).

## Context

The frontend design listed Tailwind Plus components "where available", Lucide React for icons, and a shell with a workspace switcher, notifications and an account menu. The repository has no Tailwind Plus license, and the API has no workspace-listing, notification or account-management endpoints.

## Decision

Build original accessible components in `components/ui` and feature folders, styled with Tailwind CSS 4 (`@import "tailwindcss"`, theme tokens) plus global and feature stylesheets. Use short Unicode glyphs for navigation icons, and Recharts for the volume chart with a text alternative. Do not add `lucide-react`.

The shell shows a fixed "Sandbox workspace" label and Sandbox badge, a theme selector and a static account panel that says account actions are unavailable. There is no workspace switcher and no notifications control. Pending payment intents are kept in `sessionStorage` so a lost response is resent with the same idempotency key; query keys include the workspace and environment, and mutations never retry automatically.

## Consequences

- No third-party commercial source to license or audit; styling is less utility-first than the design implied.
- The wireframes' workspace switcher, account menu actions and notification bell are design intent, not implemented. [Wireframes](../frontend/wireframes.md) notes each difference.
- Icons are decorative glyphs marked `aria-hidden`; status is always conveyed in text.
- `packages/*` is declared in the workspace but the directory does not exist; generated types live in `frontend/lib/api/schema.d.ts`.
