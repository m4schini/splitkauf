# Research: Best Practices for Collaborative Shopping Lists

> **Note on sources:** Web search tools were unavailable. Findings are synthesized from training knowledge through August 2025. Named projects (Yjs, Automerge, Bring!, AnyList, etc.) are real and well-documented at their respective sites.

---

## 1. Data Model

### Core Entities

**`lists`** — `id, name, owner_id, currency, created_at, updated_at, archived_at`

**`list_members`** — `(list_id, user_id) PK, role enum('owner','editor','viewer'), joined_at`

**`items`** — the central table:
```
id, list_id, name, quantity (numeric), unit (text),
note, category_id, sort_order (real — fractional index),
checked (bool), checked_by, checked_at,
assignee_id, added_by,
created_at, updated_at, deleted_at (soft delete — critical for sync)
```

**`categories`** — `id, list_id (nullable for global), name, sort_order, color, icon`

### Key Design Decisions

- **Soft delete** (`deleted_at`) is mandatory — a hard DELETE cannot be propagated to offline clients. A tombstone record can.
- **`checked_by` + `checked_at`** — store who checked an item and when, not just the boolean. Required for undo and for seeing who picked up which item.
- **`quantity` + `unit` are separate fields** — storing "2 liters" as a string breaks quantity editing and merging. Support units: pcs, kg, g, L, mL, pack, bottle, can, plus free-text for edge cases.
- **Single `assignee_id`** — multi-assignee requires a junction table; single-assignee is sufficient for household use and simpler.
- **`sort_order` as a float (fractional index)** — reordering one item only updates one row. Integer ranks require rewriting all siblings. Libraries: `fractional-indexing` (npm), `lexorank` (npm).

---

## 2. Conflict Resolution and Real-Time Sync

### Conflict Profile of Shopping Lists

Shopping lists have a **low conflict rate**: operations are mostly independent, and the rare conflict (two people check the same item simultaneously) is low-stakes. **Full CRDTs are overkill.**

### Recommended: LWW + Optimistic UI

**Last-Write-Wins** (LWW) per item using server-assigned `updated_at`:
- On conflict, the item with the later `updated_at` wins
- Simple, correct for shopping list granularity

**Optimistic UI** layered on top:
1. User action → local state updates immediately
2. Mutation sent to server async
3. Server confirms → done; server errors → roll back + toast

This is the approach Linear documents in their engineering blog and the pattern React Query's `onMutate`/`onError`/`onSettled` is designed for.

### CRDTs: When to Adopt

Yjs and Automerge become worth adopting if:
- Item names are collaboratively edited character-by-character
- Offline periods are long and conflicts become frequent

**Yjs** is the better choice if you go CRDT — better ecosystem, providers for WebSocket (y-websocket), IndexedDB (y-indexeddb), and WebRTC (y-webrtc). `Y.Array` handles ordered lists; `Y.Map` handles item properties.

### Transport Layer

| Technology | Verdict for splitkauf |
|---|---|
| **SSE** | Recommended — server→client push is all you need; mutations go via REST POST/PATCH |
| **WebSocket** | Warranted only if you add presence/cursors; overkill for list sync |
| **Supabase Realtime** | Ideal if using Supabase — Postgres CDC broadcasts row changes via Phoenix Channels automatically |
| **Pusher / Ably** | Managed WS; good fallback if self-hosting |
| **Polling** | Acceptable fallback only; 5–10s delay is noticeable |

---

## 3. UX Patterns for Shopping Lists

### Quick Add
- **Always-visible single text input** — no modal, no navigation barrier
- **Autocomplete from household history** — most-used items surface as user types
- **Natural-language quantity parsing**: "2 liters milk" → `{name: "milk", quantity: 2, unit: "L"}`
- Barcode scanning (mobile) and voice input are useful V2 features

### Check-Off Flow
- **Check → item sinks to "In Cart" / "Done" section** at bottom, greyed out — universal pattern across Bring!, AnyList, OurGroceries
- Un-check is always available (tap again)
- **"Clear checked items"** is a bulk action shown when ≥1 item is checked
- **Never hard-delete on check** — keep as record for history and undo
- Check-off animation must be instantaneous (optimistic) — network latency must not block it

### Grouping by Store Section / Aisle
- **Manual categorization** — user sets category per item; optional, not required on add
- **Auto-categorization** — maintain a mapping of item names → category (milk → Dairy; apples → Produce). Bring! has a large crowdsourced dictionary.
- **Store-specific aisle order** — let users reorder categories to match their store layout (V2 feature)
- **Sticky section headers** in the scrollable list

For MVP: preset categories (Produce, Dairy, Bakery, Meat, Beverages, Household, Other) + manual assignment. Auto-categorization is V2.

### Item Recurrence / Smart Suggestions
- "Add again" from previous lists
- Frequency-based suggestions bubble up in autocomplete
- Template lists users can copy ("weekly groceries")
- Suggestions pooled from all household members' history

### Sort Order
- Default: **by category** (aisle order → item sort_order within category)
- Manual drag-and-drop using fractional index
- Alphabetical as an optional secondary view

---

## 4. Offline-First Architecture

### Local Storage

**Recommended: Dexie.js** (clean IndexedDB wrapper for web). Supports indexes, transactions, and reactive queries. WatermelonDB is the equivalent for React Native.

Do not use localStorage — 5–10 MB limit and no structured queries.

SQLite via WASM (with OPFS backend) is an alternative with full SQL query power but a larger bundle.

### Sync Queue Pattern

```
offline_queue:
  id, operation ('create'|'update'|'delete'),
  entity_type, entity_id, payload (json),
  created_at, synced_at (null = pending), error
```

On mutation while offline:
1. Apply to local Dexie store immediately
2. Append to `offline_queue`
3. Listen for `online` event / `navigator.onLine`
4. On reconnect: replay queue in insertion order

### Conflict Resolution on Reconnect

**Server-wins LWW** is appropriate for shopping lists: if `server.updated_at > local.updated_at`, server version wins. Otherwise, send the local version. For per-field merges (two users changed different fields offline), compare field-level timestamps.

### Service Worker (PWA)

- **Workbox** handles the service worker boilerplate
- Cache-first for static assets (app shell)
- Network-first with Dexie fallback for API reads
- **`workbox-background-sync`** queues failed mutations and replays them when connectivity returns — even if the tab is closed (using the Background Sync API, supported in Chrome/Firefox)

### React Query Integration

```js
useMutation({
  mutationFn: updateItem,
  networkMode: 'offlineFirst',  // queues when offline
  onMutate: (variables) => {
    // snapshot + optimistic update
  },
  onError: (err, variables, context) => {
    // rollback
  },
})
```

---

## 5. Notable Prior Art and Open Source

| Project | Key Takeaways |
|---|---|
| **Ink & Switch "Local-first Software" (2019)** | Foundational manifesto; defines 7 local-first properties; introduces Automerge |
| **Yjs** | Best CRDT library for web; `Y.Array` for lists; `y-websocket`, `y-indexeddb` providers |
| **Automerge 2.x** | Rust/WASM CRDT; JSON document model; alternative to Yjs |
| **Grocy** (grocy.info) | Open-source self-hosted grocery mgmt; good data model for items/products/units; PHP+SQLite |
| **Bring!** | Best-in-class shopping list UX; household dictionary; category auto-assign |
| **AnyList** | Recipe integration; store aisle ordering; single best UX for power users |
| **OurGroceries** | Simple, battle-tested; widely used by households |
| **Linear engineering blog** | Best public documentation of optimistic UI + rollback + offline queue for collaborative apps |
| **TodoMVC** | Reference implementations in all frameworks; good for comparing state management |
| **Electric SQL** | Postgres-native sync layer; declarative shapes for what data syncs to which client |
| **PowerSync** | Postgres/MongoDB sync for mobile apps; offline-first with conflict resolution |

---

## Architecture Summary for Splitkauf

```
Data Model:    lists → list_members → items + categories
               items.sort_order = fractional float
               items.checked + checked_by + checked_at
               items.deleted_at (soft delete)
               items.quantity + unit

Sync:          REST mutations → SSE push (or Supabase Realtime)
               LWW per item (updated_at)
               Optimistic UI via React Query onMutate/onError

Offline:       Dexie.js (IndexedDB) for local store + offline_queue
               Workbox service worker + background-sync
               Replay queue on reconnect; server-wins LWW

UX:            Always-visible quick-add + autocomplete from history
               Check → sink to "In Cart" (optimistic, instant)
               Preset categories + manual override
               "Clear checked" bulk action
               Fractional index for drag-reorder
```
