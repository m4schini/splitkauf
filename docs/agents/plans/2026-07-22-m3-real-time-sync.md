---
date: 2026-07-22
branch: main
topic: "M3: real-time sync (SSE)"
tags: [plan, m3, sync, sse, events, realtime, frontend]
status: complete
---

# PLAN: M3 — Real-time Sync

Deliver the M3 slice: members with the app open see items added, edited,
checked, or removed by others appear live (US-S.1); and concurrent edits
converge without data loss, with check/uncheck never silently dropped (US-S.3).

Builds on the M1–M2 stack: chi router + generated `ServerInterface`
(`ports/rest`), the `lists` service, and the React SPA whose views each own
their state and reload via a local `reload()` after mutations.

## Acceptance Criteria

- **US-S.1**: a mutation on one connected client (add/update/delete/check/
  uncheck an item, or list create/rename/delete) causes every other connected
  client viewing the same data to reflect the change without a manual refresh,
  via a server→client push transport (SSE).
- **US-S.3**: concurrent field edits resolve last-write-wins (already the model:
  each mutation is a full server-timestamped row write); check and uncheck are
  absolute set operations serialized by Postgres and are never silently dropped;
  all clients converge to the server state because every mutation broadcasts.
- Transport is **SSE** (`text/event-stream`), the choice recorded in
  `docs/research/collaborative-lists.md` (server→client push is all that is
  needed; mutations stay REST). Single-tenant: one group, so a single global
  event stream — every member receives every event.
- New Go code uses `telemetry.Logger(...)`; the SSE handler is nil-safe and does
  not panic in degraded mode.
- `make check` is green.

## Transport & Endpoint

`GET /api/v1/events` — an SSE stream, registered **manually** on the API router
behind `authn.RequireAuth`, outside the generated `ServerInterface` handler and
its OpenAPI request-validation middleware.

This follows the established precedent for non-JSON browser endpoints: the M2
BFF auth endpoints (`/api/auth/*`) likewise live outside `/api/v1`'s validation
because they are not JSON resources (see `ports/rest/server.go` and the M2
plan). An SSE stream cannot be modeled by `oapi-codegen` (it is not a
request/response JSON operation) and must bypass the JSON validator to stream,
so it is registered by hand rather than added to `splitkauf.openapi.yaml`. The
event payload contract is documented here and in the `events` package.

### Event payload

One JSON object per SSE `data:` frame:

```json
{ "type": "lists" | "items", "listId": "<uuid?>" }
```

Events are **coarse reload hints**, not state deltas: a client reacts by
refetching the affected resource over REST (matching the existing
reload-after-mutation pattern; no client cache to patch). `listId` is present
only on `items` events.

| Event      | Emitted when                                             | Client reloads                          |
|------------|----------------------------------------------------------|-----------------------------------------|
| `lists`    | list created / renamed / deleted                         | lists overview                          |
| `items`    | item added / updated / deleted / checked / unchecked     | that list's detail + lists overview     |

## Key Decisions

1. **In-memory fan-out broker** (`events` package, new top-level package like
   `lists`). No external broker: single-process, single-tenant. A
   `Broker` holds a set of subscriber channels; `Publish` does a **non-blocking**
   send to each (a full buffer drops that hint — acceptable because the next
   event, or the reconnect reload, refetches the latest state anyway). `Subscribe`
   returns a receive channel + an unsubscribe func. The broker implements
   `events.Publisher` (`Publish(Event)`), the seam the REST handlers depend on.
2. **Publish from the handler layer, not the domain.** Handlers already call the
   services directly and hold every id needed for an event; the domain packages
   stay pure (no broadcast concern leaks into `lists`). `V1` gains an
   `Events events.Publisher` field, guarded nil-safe (`v.publish(...)`), so
   existing handler tests that leave it nil keep working. Publish happens only
   after the mutating call succeeds.
3. **Manual SSE route, broker passed through `rest.New`.** `rest.New` takes a
   `*events.Broker`; it registers `GET /api/v1/events` on the API subrouter with
   `authn.RequireAuth`. The handler subscribes, writes each event as
   `data: <json>\n\n`, flushes via `http.NewResponseController`, sends a periodic
   heartbeat comment to survive proxy idle timeouts, and returns when
   `r.Context()` is done (client disconnect / server shutdown).
4. **US-S.3 needs no new conflict machinery.** LWW is the accepted model
   (research doc §2). Each mutation is a full row write with a server-assigned
   `updated_at`; Postgres serializes them, so concurrent edits converge to the
   last write. Check/uncheck are absolute (`SetChecked true/false`), each applied
   and each broadcast — never coalesced or dropped. This phase adds tests that
   pin the convergence behaviour rather than new code.
5. **Frontend: one shared EventSource, ref-counted, no React Query.** A small
   `live` module opens a single `EventSource('/api/v1/events')` while ≥1 view is
   subscribed and closes it when the last unsubscribes (so it never connects
   while logged out, since live views only mount when authenticated). A
   `useLiveEvents(handler)` hook registers/unregisters a listener. On the
   EventSource reconnecting after a drop, listeners receive a synthetic
   `reconnect` event so each view does a full reload, covering any hints missed
   while disconnected. This keeps the existing per-view `reload()` pattern; no
   cache/query layer is introduced.

## Implementation

### Phase 1: Plan (this commit)

`chore(plans)` — this file only.

### Phase 2: `events` package (broker)

Dependencies: none

- [x] `events/events.go`: `Event{Type string; ListID string}` with `Type`
      constants (`TypeLists`, `TypeItems`); `Publisher` interface
      (`Publish(Event)`).
- [x] `events/broker.go`: `Broker` with `NewBroker()`, `Subscribe() (<-chan
      Event, func())`, `Publish(Event)` (non-blocking per-subscriber send),
      `Count() int`. Mutex-guarded subscriber map; buffered subscriber channels.
- [x] `events/broker_test.go`: a subscriber receives a published event;
      unsubscribe stops delivery and drops the subscriber; multiple subscribers
      each receive; publish to a full/slow subscriber does not block other
      deliveries; `Count` reflects subscribe/unsubscribe.

**Automated Verification**:
- [x] `go test ./events/...` passes (`-race`).

### Phase 3: SSE endpoint + wiring

Dependencies: Phase 2

- [x] `ports/rest/sse.go`: `sseHandler(broker *events.Broker) http.HandlerFunc`
      — set SSE headers, subscribe, stream `data:` frames as JSON, heartbeat via
      a ticker, flush with `http.NewResponseController`, exit on
      `r.Context().Done()`. Log via `telemetry.Logger("sse")`.
- [x] `ports/rest/server.go`: `New` takes `broker *events.Broker`; register
      `apiRouter.With(authn.RequireAuth).Get("/api/v1/events", sseHandler(broker))`
      (mounted on the API subrouter, outside the generated handler/validator).
- [x] `cmd/serve.go`: construct `broker := events.NewBroker()`, pass to
      `rest.New`, and set it on the `v1.V1` (Phase 4 field).
- [x] `ports/rest/v1/api_test.go`: update `newTestHandler` to pass a broker.
- [x] `ports/rest/sse_test.go`: a published event is written to the stream as a
      `data:` line; the handler returns when the request context is cancelled;
      requires auth (401 without a dev-auth user — covered by the shared handler
      wiring).

**Automated Verification**:
- [x] `go build ./...`; `go test ./ports/rest/...` passes.

### Phase 4: Publish from handlers

Dependencies: Phase 2, Phase 3

- [x] `ports/rest/v1/api.go`: add `Events events.Publisher` to `V1`; add a
      nil-safe `func (v *V1) publish(e events.Event)` helper.
- [x] `ports/rest/v1/lists.go`: after success, publish — `CreateList`/
      `RenameList`/`DeleteList` → `{lists}`; `AddItem`/`UpdateItem`/`DeleteItem`/
      `CheckItem`/`UncheckItem` → `{items, listId}`.
- [x] `ports/rest/v1/*_test.go`: a capturing `events.Publisher` fake asserts the
      expected events are emitted for a representative mutation of each kind
      (item check, list create), and that no event fires when the mutation
      errors.

**Automated Verification**:
- [x] `go test ./ports/...` passes; `make lint lint-vet` green.

### Phase 5: Frontend live updates

Dependencies: Phase 3 (transport reachable)

- [x] `frontend/src/live.ts`: `AppEvent` type; a ref-counted singleton that owns
      one `EventSource('/api/v1/events')`, dispatches parsed events (and a
      synthetic `{type:'reconnect'}` on reconnect) to registered listeners;
      `subscribeLive(handler): () => void`.
- [x] `frontend/src/live.ts` (or a hooks file): `useLiveEvents(handler)` React
      hook wrapping `subscribeLive` with `useEffect` cleanup.
- [x] Wire views:
      - `ListsOverview`: reload on `lists`, `items`, `reconnect`.
      - `ListDetail`: reload on `items` for this `listId`, and on `reconnect`.
- [x] `frontend/src/live.test.ts`: stub `EventSource` (`vi.stubGlobal`); assert
      subscribe opens one connection, dispatch calls the handler, last
      unsubscribe closes it, and reconnect emits the synthetic event.
- [x] Update component tests as needed so a stubbed `EventSource` exists in the
      jsdom env (no real network).

**Automated Verification**:
- [x] `make frontend-check` green (lint/format/typecheck/tests).

### Phase 6: US-S.3 convergence tests

Dependencies: Phase 4

- [x] `ports/rest/v1` (or `adapters/db` guarded) tests asserting: two sequential
      field updates converge to the last (LWW); a check followed by an uncheck
      leaves the item unchecked with both operations applied (neither dropped);
      each such mutation emits its event. Document in the plan Deviations if the
      existing model already covers a case without new code.

**Automated Verification**:
- [x] `go test ./...` passes; `make check` green.

**Manual Verification**:
- [x] Run the server against compose Postgres; open two browser tabs on the same
      list; add/check/edit/delete an item in one and confirm the other updates
      without refresh.

## References

- User stories: `docs/user-stories/US-S.1`, `US-S.3`.
- `docs/research/collaborative-lists.md` §2 (SSE recommended; LWW + optimistic
  UI; CRDTs overkill for this conflict profile).
- `docs/agents/plans/2026-07-21-m2-oidc-auth.md` (precedent: non-JSON browser
  endpoints registered outside `/api/v1` validation).
