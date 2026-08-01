# Splitkauf — User Stories

Derived from [`GOAL.md`](../../GOAL.md). One story per file, named
`US-<area>.<n>-<slug>.md`. Story IDs are stable so plans and commits can reference
them. Areas: **L** lists, **S** real-time sync, **O** offline/PWA,
**A** authentication, **Q** API quality & operations, **B** branding & visual
polish.

Personas: **Member** — an authenticated user of the group's instance (single-tenant,
everyone belongs to the group). **Operator** — the person self-hosting the instance.

## UX guardrails

Every story with a UI surface additionally inherits the do's-and-don'ts
checklist (§6) of
[`docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md`](../agents/research/2026-07-31-mobile-first-shopping-list-ux.md)
as acceptance criteria. The core loop it protects: add items fast → share the
list → check items off one-handed in a store aisle. Story files below call out
only the criteria specific to that story; the checklist applies globally
(touch targets ≥44pt/48dp, 8pt spacing grid, ≥16px body text, WCAG 2.2 AA
contrast in light *and* dark mode, no confirmation dialogs or blocking
spinners in the core loop, every gesture has a tap alternative).

## Implementation order

Follows GOAL.md's walking-skeleton-first milestone sequencing. Within a milestone,
stories are ordered by dependency; stories listed on the same step are independent
of each other and may be built in either order (or in parallel).

### M1 — Walking skeleton

| # | Stories | Why here |
|---|---------|----------|
| 1 | [US-Q.1](US-Q.1-spec-first-api.md), [US-Q.2](US-Q.2-uniform-errors.md), [US-Q.3](US-Q.3-one-command-deploy.md) | Cross-cutting foundations: spec-first workflow, RFC 9457 error handling, buildable/deployable stack. Established first, enforced by every later story. |
| 2 | [US-A.1](US-A.1-dev-login.md) | Hardcoded dev auth unblocks every authenticated endpoint. |
| 3 | [US-L.1](US-L.1-create-a-list.md) | First real domain object end to end (API → Postgres → PWA). |
| 4 | [US-L.2](US-L.2-view-all-lists.md), [US-L.4](US-L.4-add-an-item.md) | Read path for lists; items need a list to live in. |
| 5 | [US-L.7](US-L.7-check-off-an-item.md), [US-L.8](US-L.8-uncheck-an-item.md) | Check/uncheck completes the M1 end-to-end slice. |
| 6 | [US-L.5](US-L.5-edit-an-item.md), [US-L.6](US-L.6-remove-an-item.md), [US-L.3](US-L.3-rename-and-delete-a-list.md) | Remaining CRUD; rounds out list management. |

### M2 — OIDC auth

| # | Stories | Why here |
|---|---------|----------|
| 7 | [US-A.2](US-A.2-log-in-via-oidc.md) | Replaces dev auth (US-A.1) with the BFF flow. |
| 8 | [US-A.3](US-A.3-stay-logged-in.md), [US-A.4](US-A.4-log-out.md), [US-A.5](US-A.5-membership-from-the-provider.md) | Session lifecycle and provider-derived membership. |

### M3 — Real-time sync

| # | Stories | Why here |
|---|---------|----------|
| 9 | [US-S.1](US-S.1-live-list-updates.md) | Establishes the transport (SSE/WebSocket) on the highest-value events. |
| 10 | [US-S.3](US-S.3-convergent-concurrent-edits.md) | Convergence rules formalized before offline builds on them. |

### M4 — Offline-first

| # | Stories | Why here |
|---|---------|----------|
| 11 | [US-O.1](US-O.1-install-on-my-phone.md) | Verified install experience (iOS Safari) before deep offline work. |
| 12 | [US-O.2](US-O.2-use-the-app-offline.md) | Offline read + core actions; depends on convergence rules (US-S.3). |
| 13 | [US-O.3](US-O.3-sync-when-back-online.md) | Queue replay closes the offline loop. |

### M5 — Hardening

| # | Stories | Why here |
|---|---------|----------|
| 14 | [US-Q.5](US-Q.5-request-body-limits.md), [US-Q.6](US-Q.6-durable-sessions.md), [US-Q.7](US-Q.7-concurrent-delete-correctness.md) | Targeted fixes for the deferred M1–M2 review findings; independent of each other. |
| 15 | [US-Q.4](US-Q.4-production-hardening.md) | Final audit pass once all capabilities exist. |

### M6 — Branding & quick-add polish

| # | Stories | Why here |
|---|---------|----------|
| 16 | [US-B.1](US-B.1-app-icon.md), [US-B.2](US-B.2-green-accent.md), [US-L.9](US-L.9-quantity-and-unit.md) | Visual identity (real icon, icon-green accent) and the quantity/unit quick-add upgrade; independent of each other. Deliberately after M4/M5 so those plans stay as written — US-L.9 retrofits `unit` into the offline pending-create payload. |

### M7 — Username/password auth

| # | Stories | Why here |
|---|---------|----------|
| 17 | [US-A.7](US-A.7-operator-provisions-accounts.md) | Operator-provisioned accounts (CLI) — the account store must exist before anyone can log in. |
| 18 | [US-A.6](US-A.6-log-in-with-username-password.md) | Password login as an alternative to OIDC for instances without an identity provider; reuses the M2 session layer. |

## Out of scope

No stories exist — and none should be written — for GOAL.md's non-goals: native
mobile apps, multi-tenancy/billing, in-app user management, or localization beyond
German and English.
