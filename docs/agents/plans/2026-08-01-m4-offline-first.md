---
date: 2026-08-01T05:45:40+00:00
git_commit: 19cdedfd9160059dc9208d4a2d7d5b5cba7a7523
branch: main
topic: "M4: offline-first (persisted cache, paused-mutation outbox, soft delete)"
tags: [plan, m4, offline, pwa, react-query, soft-delete, ios]
status: ready
---

# PLAN: M4 — Offline-First

Deliver the M4 slice: the PWA installs verifiably on iOS Safari (US-O.1), lists
and the core actions (view, add, check, edit, delete) work with no network
(US-O.2), and queued offline changes replay automatically on reconnect,
converging per the US-S.3 rules (US-O.3).

Builds on the M1–M3 stack: React Query with optimistic `onMutate`/`onError`
everywhere (`frontend/src/queries.ts`), the SSE live layer with its synthetic
`reconnect` event (`frontend/src/live.ts`), LWW convergence (US-S.3), and the
precache-only PWA shell from project-setup Phase 3.

## Product decisions (from the user)

1. **React Query persistence, not Dexie.** The offline layer is
   `persistQueryClient` over IndexedDB plus React Query's paused-mutation
   outbox — one state layer, reusing the existing optimistic code. This is a
   recorded **deviation** from `docs/research/collaborative-lists.md` §4
   (which recommends Dexie + a custom `offline_queue` and pre-dates the React
   Query frontend). `docs/architecture.md` §3 is updated accordingly.
2. **Full offline core loop on just-added items.** Items created offline get a
   visible "unsynced" badge and a needs-internet disclaimer; check/edit on such
   an item **merge into its still-queued create** (a delete cancels it) — no
   ID remapping, no blocked controls.
3. **Soft delete for items (migration 000004).** `items.deleted_at`; item
   delete becomes an update that works offline like any mutation, and the undo
   snackbar becomes a server-backed **restore**. List delete stays a hard
   cascade. No tombstone purge yet (single-household scale; noted as a future
   operational item).
4. **US-O.1 is manifest verification only.** The planned custom iOS install
   banner stays a deferred architecture item.
5. **Ordering vs the M5 plan.** M4 lands before M5 (milestone order). Both
   plans touch `adapters/db/lists.go` `AddItem`: this plan extends its
   signature with `checked`; the M5 plan
   (`2026-08-01-m5-hardening-fixes.md`) later removes its `requireList`
   pre-check. The M5 plan's line references are against the shared base
   commit and must be re-anchored after M4.

## Acceptance Criteria

- **US-O.1**: the PWA installs from iOS Safari with correct icon, title,
  standalone display, and splash behavior per
  `docs/agents/research/2026-07-21-pwa-ios-support.md` (manual, real device).
- **US-O.2**: with the network cut, a previously-visited lists overview and
  list detail render from the persisted cache; add/check/uncheck/edit/delete
  apply instantly to local state; a quiet offline indicator shows (no blocking
  spinner or modal); offline-created items carry an "unsynced" badge and a
  needs-internet disclaimer, and check/edit/delete on them merge into (or
  cancel) the queued create.
- **US-O.3**: queued mutations replay in order on `online`/`visibilitychange`
  (no Background Sync API on iOS); a replay hitting 404 is dropped with a
  refetch and a quiet notice; convergence follows US-S.3 (LWW, server
  authoritative).
- Item deletes are soft (`deleted_at` set, row kept); deleted items never
  appear in any read or count; undo restores server-side via
  `POST /api/v1/lists/{listId}/items/{itemId}/restore` and works offline.
- `make check` green; `go test -race` clean on touched Go packages.

## Technical Key Decisions and Tradeoffs

1. **Persist the React Query cache to IndexedDB** via
   `PersistQueryClientProvider` + `@tanstack/query-async-storage-persister`
   over `idb-keyval`.
   - Why: views read from the cache today; persisting it makes every visited
     view available offline with zero component changes.
   - Impact: query `gcTime` must be raised (persistence cannot outlive garbage
     collection — set `gcTime: 7 days` to match `maxAge`); a cache `buster`
     tied to the app build invalidates stale schemas; the persisted client is
     cleared on logout so a shared device never shows another member's data.
2. **Offline mutations = React Query paused mutations, persisted and resumed.**
   Mutations use `networkMode: 'offlineFirst'` **together with a retry policy**
   — in v5, `offlineFirst` fires the mutationFn once and only pauses on
   *retry*, so with the default `retry: 0` an offline mutation would fail
   straight into the `onError` rollback instead of queuing. Set
   `retry: (failureCount, err) => !is4xxProblem(err) && failureCount < 3` so
   network failures pause (outbox) while 4xx problems fail fast (no pointless
   replays); `onError` rollback fires only on final failure, never on a pause.
   Paused mutations are persisted (they survive a tab close) and resumed via
   `resumePausedMutations()` on `online`/`visibilitychange` and after cache
   restore.
   - Why: the optimistic `onMutate` cache patches already exist; pause-on-retry
     gives the queue, and the first attempt still runs on flaky in-store
     connections where `onlineManager` misreports.
   - Impact: resuming persisted mutations after a reload requires
     `setMutationDefaults` (a stable `mutationKey` + default `mutationFn` per
     operation) registered at module scope in `queries.ts` — a structural
     refactor of every mutation hook, behavior unchanged.
3. **Pending-create coalescing instead of ID remapping.** Offline adds mint a
   temp UUID and register a payload in a small `pendingCreates` module
   (`Map<tempId, AddItemRequest>`). The create's `mutationFn` reads the
   **latest** payload from the map at execution time. While the create is
   queued: edit/check/uncheck on the temp ID only patch the cache and the map
   (no separate mutation is enqueued); delete removes the map entry and the
   paused mutation. On create success the temp item is replaced by the server
   item in the cache.
   - Why: replay never references an ID the server hasn't seen; the whole
     chain collapses into one POST.
   - Impact: `AddItemRequest` gains an optional `checked` field (spec-first)
     so a folded-in check survives; the server sets `checked_at` at insert.
4. **Soft delete via `deleted_at`, restore as a spec-first operation.**
   `DeleteItem` becomes `UPDATE … SET deleted_at = now()`; every read/count
   filters `deleted_at IS NULL`; `restoreItem` clears it (idempotent — 404
   only when the row is absent, mirroring check/uncheck).
   - Why: delete works offline like any mutation, and undo becomes correct
     across devices instead of a client-held deferred DELETE.
   - Impact: migration 000004 (own commit); the item half of
     `useUndoQueue`/deferred-delete frontend machinery is replaced; the list
     half stays (list delete remains hard).
5. **No service-worker runtime caching for the API.** The SW keeps precaching
   the app shell only; API reads come from the persisted query cache.
   - Why: one offline data path; a SW cache of `/api/v1` responses would be a
     second, unsynchronized copy.
   - Impact: `vite.config.ts` PWA config is unchanged in this plan.

## Current State

```
frontend/src/
├── main.tsx          QueryClientProvider (gcTime default 5m — evicts!)
├── queries.ts        per-hook inline mutationFn, optimistic onMutate/onError
├── useUndoQueue.ts   client-held deferred DELETE (5s) for items AND lists
└── live.ts           SSE singleton + synthetic 'reconnect' → invalidate
frontend/vite.config.ts  precache-only SW (autoUpdate)

backend
├── items: HARD delete (adapters/db/lists.go DeleteItem)
├── AddItemRequest: name/quantity/note (splitkauf.openapi.yaml:496)
└── lists.Repository (lists/lists.go:82): no restore, no deleted_at
```

Offline today: the shell loads (precache) but every view shows a load error;
mutations fail outright.

## Desired End State

```
                     ┌────────────── IndexedDB ──────────────┐
reads:   components ─┤ persisted RQ cache (maxAge 7d)        │
writes:  mutation ───┤ persisted paused mutations (outbox)   │
                     └────────────────────────────────────────┘
offline: onMutate patches cache → mutation pauses → badge on temp items
         edits on temp id → update pendingCreates payload only
online/visibilitychange/restore: resumePausedMutations() → replay in order
         404 on replay → drop + invalidate + quiet notice
item delete: POST DELETE → deleted_at set (offline-capable)
undo:        POST /items/{id}/restore → deleted_at cleared (offline-capable)
```

## Abstractions and Code Reuse

- `splitkauf.openapi.yaml` — `restoreItem` operation (mirrors check/uncheck,
  emits `Item`, `default` Problem response); `AddItemRequest.checked`
  (optional bool, default false).
- `lists/` — `Repository` gains `RestoreItem(ctx, listID, itemID) (Item, error)`;
  `AddItem` gains a `checked bool` parameter; `Service` gains `RestoreItem`.
- `adapters/db/lists.go` — soft-delete `DeleteItem`, `deleted_at IS NULL`
  filters on `Item`/`ListItems`/count aggregates, `RestoreItem`.
- `ports/rest/v1/handlers_lists.go` — `RestoreItem` handler (+ `items` event,
  same pattern as `CheckItem`).
- `frontend/src/pendingCreates.ts` — **new**: the temp-id registry described in
  Key Decision 3; consumed only by `queries.ts` and the item row badge.
- `frontend/src/queries.ts` — mutations refactored to
  `mutationKey` + `setMutationDefaults`; `useRestoreItem`; add-item flow uses
  `pendingCreates`.
- `frontend/src/useUndoQueue.ts` — item path removed; list path kept.
- `frontend/src/OfflineIndicator.tsx` — **new**: quiet banner driven by
  React Query's `onlineManager`.

## Logging & Observability

- Backend: `RestoreItem` logs through the existing handler pattern; no new
  loggers. Soft-deleted rows remain countable via SQL for operators.
- Frontend: no console noise in production paths; the offline banner and the
  replay-failure snackbar are the user-visible observability.

## Implementation

### Phase 1: Migration 000004 — soft-delete column (own commit)

Dependencies: none

- [x] `database/migrations/000004_soft_delete_items.up.sql`:
      `ALTER TABLE items ADD COLUMN deleted_at TIMESTAMPTZ;`
- [x] `000004_soft_delete_items.down.sql`: drop the column.

**Automated Verification**:
- [x] `go build ./...` (migration embeds); `go run . migrate` applies to
      version 4 against a disposable Postgres and the down migration reverts.

### Phase 2: Backend — soft delete, restore, checked-on-create

Dependencies: Phase 1

Spec-first: the restore operation and `AddItemRequest.checked`, then domain,
repository, handlers, events.

**Tasks**:
- [x] Spec: add `POST /lists/{listId}/items/{itemId}/restore`
      (operationId `restoreItem`, 200 → `Item`, `default` Problem) and
      optional `checked` (boolean, default false) to `AddItemRequest`;
      `make generate`.
- [x] `lists/lists.go`: add `RestoreItem` to `Repository`; extend `AddItem`
      signature with `checked bool`.
- [x] `lists/service.go`: `Service.RestoreItem` (delegates, returns the item);
      `AddItem` passes `checked` through (sets `CheckedAt` server-side when
      true). Update `lists/service_test.go` + fake repo.
- [x] `adapters/db/lists.go`: `DeleteItem` →
      `UPDATE items SET deleted_at = now(), updated_at = now() WHERE … AND deleted_at IS NULL`
      (0 rows → `ErrNotFound`); `RestoreItem` →
      `UPDATE items SET deleted_at = NULL, updated_at = now() WHERE list_id=$1 AND id=$2 RETURNING …`
      (idempotent; 0 rows → `ErrNotFound`); add `deleted_at IS NULL` to
      `Item`, `ListItems`, `UpdateItem`, `SetItemChecked`. For the
      open/checked count aggregates in the shared `listSelect`
      (`adapters/db/lists.go:36`), the predicate goes into the
      `LEFT JOIN … ON` clause (`AND i.deleted_at IS NULL`), NOT a `WHERE` —
      a WHERE filter would turn the left join inner and drop lists whose
      items are all deleted (and empty lists). `AddItem` writes
      `checked`/`checked_at`.
- [x] `adapters/db/lists_test.go`: delete-then-restore round trip; deleted
      items excluded from reads and counts; a list whose items are ALL
      soft-deleted still appears in `Lists()`/`List()` with 0/0 counts;
      restore of a never-deleted item is idempotent; add with `checked=true`
      sets `checked_at`.
- [x] `ports/rest/v1/handlers_lists.go`: implement `RestoreItem` (maps errors
      like check/uncheck, publishes `{items, listId}`); `AddItem` passes
      `checked`.
- [x] Handler tests: restore happy path + 404; restore publishes an event;
      create-with-checked round trip.

**Automated Verification**:
- [x] `make generate` clean; `go test ./...` passes; integration tests pass
      against the disposable Postgres; `make lint` green.

### Phase 3: Frontend — server-backed item undo via restore

Dependencies: Phase 2

**Tasks**:
- [x] `frontend/src/api.ts`: `restoreItem(listId, itemId)` helper; add
      `checked?: boolean` to the `AddItemRequest` type.
- [x] `frontend/src/queries.ts`: `useRestoreItem(listId)` with optimistic
      re-insert (`restoreItemLocally` already exists); item delete becomes an
      immediate `useDeleteItemMutation` call.
- [x] `frontend/src/ListDetail.tsx`: item removal fires the delete mutation at
      once; the undo snackbar action calls `useRestoreItem` instead of
      cancelling a deferred delete. `useUndoQueue` keeps only the list path
      (rename/trim accordingly).
- [x] Component tests: remove-then-undo now asserts a restore call (not a
      cancelled delete); list-delete undo tests unchanged.

**Automated Verification**:
- [x] `make frontend-check` green.

### Phase 4: Frontend — persisted cache + offline indicator

Dependencies: none (parallel-safe with Phases 1–3, same files as Phase 5 so
implemented sequentially before it)

**Tasks**:
- [x] Add deps: `@tanstack/react-query-persist-client`,
      `@tanstack/query-async-storage-persister`, `idb-keyval`.
- [x] `frontend/src/main.tsx`: swap `QueryClientProvider` for
      `PersistQueryClientProvider` (async IDB persister, `maxAge` 7 days,
      `buster` from the Vite build hash/env); raise default query
      `gcTime` to 7 days.
- [x] `frontend/src/api.ts` `logout()`: clear the persisted client +
      `queryClient.clear()` before submitting the logout form.
- [x] `frontend/src/OfflineIndicator.tsx`: quiet banner ("Offline — changes
      sync when you're back online") driven by `onlineManager.subscribe`;
      mounted in `App.tsx`; AA contrast in both themes; no interaction cost.
- [x] Tests: indicator renders on simulated offline; persister smoke test
      (cache round-trips through a fake async storage); logout clears storage.

**Automated Verification**:
- [x] `make frontend-check` green.

### Phase 5: Frontend — offline mutation outbox with pending-create coalescing

Dependencies: Phases 2, 3, 4

**Tasks**:
- [x] `frontend/src/queries.ts`: give every mutation a stable `mutationKey`
      and register `setMutationDefaults` (module scope) so persisted paused
      mutations resume after a reload; set mutation
      `networkMode: 'offlineFirst'` **plus the retry policy from Key
      Decision 2** (pause on network failure, fail fast on 4xx problems);
      persist mutations in the provider's `dehydrateOptions`.
- [x] `frontend/src/pendingCreates.ts`: `register(tempId, payload)`,
      `update(tempId, patch)`, `take(tempId)`, `has(tempId)`; the add-item
      default `mutationFn` reads its payload from here at execution time.
- [x] Add-item flow: offline add mints a temp UUID, registers the payload,
      patches the cache (existing optimistic code); on success, replace the
      temp item with the server item in both `listKey` and `listsKey` caches.
- [x] Coalescing: `useUpdateItem`/`useCheckItem`/`useUncheckItem`/
      `useDeleteItemMutation` short-circuit when the target is a pending temp
      ID — patch the cache + `pendingCreates` only (check/uncheck set
      `checked` in the payload; delete removes the map entry and the paused
      create from the mutation cache).
- [x] Resume triggers: `resumePausedMutations()` on `online`,
      `visibilitychange` (document visible), and in the provider's
      `onSuccess` after cache restore; the SSE `reconnect` invalidation stays
      as-is.
- [x] Replay-404 policy: mutation `onError` with a 404 problem does not
      re-queue — invalidate the affected keys and enqueue a quiet snackbar
      ("Some offline changes couldn't sync"); other errors keep the existing
      rollback behavior.
- [x] Unsynced badge: item rows whose ID is in `pendingCreates` render a
      badge + the needs-internet disclaimer text (visible label, AA contrast
      both themes, ≥8px from adjacent controls).
- [x] Tests: coalescing unit tests (edit/check fold into payload; delete
      cancels); temp→server item cache replacement; replay-404 drop + notice;
      badge rendering; resume-on-online.

**Automated Verification**:
- [x] `make frontend-check` green (all suites).

**Manual Verification**:
- [x] DevTools offline: browse both views from cache; add "milk", check it,
      edit it — one queued create carrying the merged state; go online —
      single POST replays, badge clears.
- [x] Two devices: delete a list on one while the other holds a queued item
      edit offline; on reconnect the edit drops with the quiet notice.

### Phase 6: US-O.1 — iOS install verification

Dependencies: Phases 1–5 (verify the final M4 build; deviation from the
README's O.1-first order, recorded here)

**Tasks**:
- [x] Verify the manifest/icon/meta setup against
      `docs/agents/research/2026-07-21-pwa-ios-support.md` (apple-touch-icon
      180px, `apple-mobile-web-app-*` tags, `viewport-fit=cover`, maskable
      icons) — fix anything found.
- [x] Update `docs/architecture.md` §3/§5: offline layer (React Query persist
      — recorded deviation from the Dexie research), soft-delete model, and
      restore endpoint; move implemented 🔜 items to ✅.

**Automated Verification**:
- [x] `make check` green from a clean tree; `make dist` builds.

**Manual Verification**:
- [ ] (User) Install from iOS Safari on a real device: home-screen icon,
      title, standalone launch; then run the offline core loop once in-store
      or with airplane mode. — deferred (manual, user)

## Implementation Notes

- **Offline outbox refactor (post-review hardening).** The `pendingCreates`
  in-memory map (Key Decision 3) was replaced during review with the RQ
  mutation cache as the single source of truth: offline adds mint `temp-<uuid>`
  ids; a check/edit/delete on a still-queued create folds by merging into the
  paused mutation's persisted `state.variables.payload` (reload-safe), and the
  create's `onSuccess` reconciles `temp -> real` id in the cache and any queued
  follow-up mutations. This closed data-loss bugs an in-memory map couldn't.
- **Fold predicate.** A just-added offline create is RQ `status:'pending'`
  (retry backoff), not yet `isPaused`, so the foldable check is
  `tempId match && (isPaused || !onlineManager.isOnline())` — folds when
  offline, falls through to a normal mutation when the create is online/in-flight.
- **Crash-safety.** `randomId()` falls back to `Math.random` when
  `crypto.randomUUID` is unavailable (non-secure context), and toast ids use it too.
- **US-O.1.** Manifest/icon/meta verified iOS-compliant (apple-touch-icon 180,
  apple-mobile-web-app-* tags, viewport-fit=cover, maskable, standalone); the
  custom install banner remains a deferred architecture item.
- **Logout** awaits `persister.removeClient()` before navigating so a shared
  device can't restore the previous member's cache.

## References

- User stories: `docs/user-stories/US-O.1`, `US-O.2`, `US-O.3` (criteria
  extended for this plan), `US-S.3`.
- `docs/research/collaborative-lists.md` §4 (offline research; Dexie
  recommendation deviated from — see Product decision 1).
- `docs/agents/research/2026-07-21-pwa-ios-support.md` (iOS constraints: no
  Background Sync API, install verification).
- `docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md` §6
  (binding UX checklist; quiet sync state, no blocking spinners).
- TanStack Query: persistQueryClient, paused mutations,
  `setMutationDefaults`, `onlineManager`, `networkMode`.
