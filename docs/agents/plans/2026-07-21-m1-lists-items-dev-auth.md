---
date: 2026-07-21
branch: main
topic: "M1 walking skeleton: dev auth + lists/items domain"
tags: [plan, m1, lists, items, dev-auth, postgres, openapi, frontend]
status: complete
---

# PLAN: M1 Walking Skeleton — Dev Auth + Lists/Items Domain

Deliver the M1 end-to-end slice: a hardcoded dev-auth user (US-A.1) can create a
list, view all lists, add/edit/remove items, and check/uncheck them (US-L.1–L.8),
persisted in Postgres, exposed through a spec-first API with RFC 9457 errors, and
usable from the embedded PWA.

Builds on the completed RFC 9457 error handling
(`docs/agents/plans/2026-07-21-rfc9457-error-responses.md`).

## Acceptance Criteria

- A single hardcoded dev user is injected into every `/api/v1` request; `GET
  /api/v1/me` returns it. No login is required (replaced entirely in M2).
- Every endpoint below exists in `splitkauf.openapi.yaml` before its
  implementation commit, references the `default` `Problem` response, and its
  server/client artifacts are regenerated.
- Lists and items are persisted in Postgres (migration `000002`); all lists and
  items are visible to every request (no per-user ownership).
- Create/rename/delete a list; list all lists with open/checked item counts; add
  an item (name required, quantity defaults to 1, note optional); edit an item
  (last-write-wins); remove an item; check and uncheck an item.
- Deleting a list removes its items.
- Invalid requests return RFC 9457 problems (exercising the validation surface
  deferred from the RFC 9457 plan).
- `make check` is green; combined coverage for domain-logic packages ≥ 70%.

## Endpoints (all under `/api/v1`)

| Method | Path | operationId | Story |
|--------|------|-------------|-------|
| GET | `/me` | `getMe` | US-A.1 |
| GET | `/lists` | `listLists` | US-L.2 |
| POST | `/lists` | `createList` | US-L.1 |
| GET | `/lists/{listId}` | `getList` | US-L.2 |
| PATCH | `/lists/{listId}` | `renameList` | US-L.3 |
| DELETE | `/lists/{listId}` | `deleteList` | US-L.3 |
| POST | `/lists/{listId}/items` | `addItem` | US-L.4 |
| PATCH | `/lists/{listId}/items/{itemId}` | `updateItem` | US-L.5 |
| DELETE | `/lists/{listId}/items/{itemId}` | `deleteItem` | US-L.6 |
| POST | `/lists/{listId}/items/{itemId}/check` | `checkItem` | US-L.7 |
| POST | `/lists/{listId}/items/{itemId}/uncheck` | `uncheckItem` | US-L.8 |

Check/uncheck are dedicated idempotent state transitions (POST … /check sets
`checked=true`, /uncheck sets `checked=false`), so a check or uncheck is never
silently dropped. Real-time propagation and concurrent-edit convergence are M3.

## Key Decisions

1. **Hexagonal split**: a pure-Go `lists` domain package (entities, validation,
   summary, `Service` over a `Repository` interface) with a Postgres
   implementation in `adapters/db`. The `Service` is unit-tested against an
   in-memory fake repo → domain coverage without a live DB. The Postgres repo is
   integration-tested and skipped under `-short` (so `make test-unit`/CI stay
   hermetic).
2. **Dev auth (US-A.1)**: a `ports/rest/middleware` `DevAuth` middleware injects
   a fixed `User{ID, Name}` into the request context; a context helper reads it.
   No credentials — every request is the dev user. Clearly marked temporary,
   removed in M2 (US-A.2).
3. **IDs & schema**: UUID primary keys via `gen_random_uuid()` (pgcrypto already
   enabled in migration `000001`); `timestamptz` created/updated; item
   `quantity int NOT NULL DEFAULT 1 CHECK (quantity >= 1)`; nullable `note`;
   `checked bool NOT NULL DEFAULT false`; nullable `checked_at`.
4. **Body binding**: chi-server handlers decode request bodies into the
   generated request structs themselves; the OpenAPI request-validation
   middleware (already wired) validates bodies/path params first, so malformed
   input yields a validation problem before the handler runs.
5. **Path param type**: `listId`/`itemId` are `format: uuid`; oapi-codegen emits
   `openapi_types.UUID` handler args, giving free 400s on malformed IDs.
6. **Error mapping**: domain `ErrNotFound` → `problem.NotFound`; domain
   validation errors → `problem.Validation` (with a `FieldError` when a field is
   identified).

## Implementation

### Phase 1: OpenAPI spec (spec-first)

Dependencies: none

- [x] Add the eleven operations above to `splitkauf.openapi.yaml`, each with the
      `default: {$ref: '#/components/responses/Problem'}` response.
- [x] Add schemas: `User`, `List` (with `openItemCount`/`checkedItemCount`),
      `ListWithItems`, `Item`, `CreateListRequest`, `RenameListRequest`,
      `AddItemRequest`, `UpdateItemRequest`.
- [x] `make generate`; verify both artifacts refresh.

**Automated Verification**:
- [x] `make generate` succeeds; `grep -q listLists ports/rest/v1/gen.go`.
- [x] `go build ./...` (generated `ServerInterface` now has the new methods; the
      build fails until Phase 5 implements them — acceptable within the phase,
      but the *commit* for Phase 1 is spec + generated artifacts only, which
      compile because `V1` need not yet satisfy the interface until wired).

Note: to keep Phase 1 building, the generated code compiles on its own; `V1`
only must satisfy `ServerInterface` where `rest.New(si)` is called. Phase 5
adds the methods. If `go build ./...` breaks after regeneration because
`cmd/serve.go` passes `*v1.V1`, add stub methods in Phase 1 returning
`http.StatusNotImplemented` and flesh them out in Phase 5. Prefer stubs so every
commit builds.

### Phase 2: Migration 000002 (own commit)

Dependencies: Phase 1

- [x] `database/migrations/000002_lists_items.up.sql`: `lists` and `items`
      tables (FKs, checks, `updated_at` trigger or app-managed timestamps).
- [x] `000002_lists_items.down.sql`: drop both tables.
- [x] Keep `database.LatestSchema` semantics (0 = latest) unchanged.

**Automated Verification**:
- [x] `go build ./...`; migration files embed.
- [x] (manual/integration) `splitkauf migrate` applies cleanly against compose PG.

### Phase 3: Domain package `lists`

Dependencies: Phase 1

- [x] `lists/lists.go`: `List`, `Item`, `User` types; validation
      (`validateListName`, `validateItemName`, quantity default/normalise);
      `Repository` interface; domain errors (`ErrNotFound`, `ValidationError`).
- [x] `lists/service.go`: `Service` with the eleven operations’ logic.
- [x] `lists/service_test.go`: in-memory fake repo; cover validation failures and
      happy paths for every method.

**Automated Verification**:
- [x] `go test ./lists/...` passes; `go test -cover ./lists/...` ≥ 70%.

### Phase 4: Postgres repository (adapters/db)

Dependencies: Phase 2, Phase 3

- [x] `adapters/db/lists.go`: `Repository` impl over `*sql.DB` (CRUD + counts).
- [x] `adapters/db/lists_test.go`: integration tests guarded by `testing.Short()`
      skip and a `SPLITKAUF_TEST_DATABASE_DSN` env (skip when unset).

**Automated Verification**:
- [x] `go build ./...`; `go vet ./...`.
- [x] `go test -short ./adapters/...` passes (integration skipped without DSN).

### Phase 5: Dev auth + REST handlers + wiring

Dependencies: Phase 3, Phase 4

- [x] `ports/rest/middleware/devauth.go`: `DevAuth` + context helpers.
- [x] `ports/rest/v1/api.go` (+ new files): implement all eleven `ServerInterface`
      methods on `V1`, decoding bodies, calling the `Service`, mapping domain
      errors to problems, encoding responses.
- [x] `V1` gains a `*lists.Service`; `cmd/serve.go` constructs the Postgres repo
      + service and passes them; wire `DevAuth` into the `/api/v1` middleware
      chain in `ports/rest/server.go`.
- [x] Tests in `ports/rest/v1`: full-stack happy paths with a fake service, the
      deferred **validation surface** (POST `/lists` with an empty body → 400
      `/problems/validation`), `getMe`, and 404 for an unknown list.

**Automated Verification**:
- [x] `go test ./...` passes; `make check` green.

### Phase 6: Frontend — lists & items UI

Dependencies: Phase 5

- [x] `frontend/src/api.ts`: typed helpers for the new endpoints (reuse
      `apiFetch`).
- [x] Components for viewing lists, creating a list, viewing a list’s items,
      adding/editing/removing items, checking/unchecking; open vs checked
      sections visually separated (US-L.7).
- [x] `App.tsx` renders the lists UI (keep the health indicator or fold it in).
- [x] vitest coverage for the new components/api helpers (mock `fetch`).

**Automated Verification**:
- [x] `make check` green (frontend lint/format/typecheck/tests/build).

**Manual Verification** (self-verified via compose + curl where possible):
- [x] Compose stack boots; create a list, add items, check/uncheck, edit,
      remove, delete — reflected in Postgres and the PWA.

## Deviations

- Deleting a list cascades to all its items.

## Implementation Notes

During implementation, document feedback, problems, and decisions here.

## References

- User stories: `docs/user-stories/US-A.1`, `US-L.1`–`US-L.8`.
- `docs/agents/plans/2026-07-21-rfc9457-error-responses.md` (error format, spec
  conventions, validation surface).
- `docs/architecture.md` (hexagonal layout, error handling).
