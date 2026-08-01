# US-O.2 — Use the app offline

**Milestone:** M4
**Depends on:** US-O.1, US-S.3

**As a** member in a store with no reception, **I want** to view lists, check items
off, and add items while offline, **so that** shopping isn't blocked by
connectivity.

## Acceptance criteria

- App shell and data load from the service worker/local cache.
- Core actions (view, check, add) work with no network.
- All mutations are optimistic: they apply instantly to local state and sync
  in the background; rollback (with notice) only on hard failure.
- Offline/sync state is a quiet indicator, never a blocking spinner or modal
  (UX research §2).
- Items created offline (not yet known to the server) carry a visible
  "unsynced" badge, with a disclaimer that internet is needed before they can
  sync.
- Checking, editing, or deleting an item that was itself created offline works
  fully: the changes merge into that item's still-queued create (a delete
  cancels it) — the full core loop works on just-added items with no network.
