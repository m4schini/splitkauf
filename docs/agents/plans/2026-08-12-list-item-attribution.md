---
date: 2026-08-12T09:53:44+00:00
git_commit: c8bc3732fefdb4ade7121e8144944ecdcb42b513
branch: main
topic: "List & item attribution: Created by / Added by / Bought by"
tags: [plan, lists, items, members, auth, frontend, api]
status: draft
---

# PLAN: List & Item Attribution (Created by / Added by / Bought by)

Show who acted on shared shopping data: "Created by" on shopping lists (overview rows), "Added by" on open (unchecked) items, and "Bought by" on checked items. Attribution is stored as the acting user's UUID and the display name is resolved at read time, so profile renames propagate.

Suggested user story id: **US-L.11** (see who did what).

## Acceptance Criteria

- Each list row in the overview shows who created the list ("… · by Alex" / "· by you"); nothing extra is shown for pre-existing lists (unknown creator).
- Open (unchecked) items show a muted "Added by Alex" / "Added by you" line under the qty/note; checked items show "Bought by …" instead. No line when the attribution is unknown.
- "You" is determined client-side by comparing the attribution's user id with the `/me` user id.
- Unchecking clears the bought-by attribution; re-checking stamps the (possibly different) new checker.
- Copying a list stamps the copier as creator of the copy and adder of every copied item.
- Optimistic/offline mutations render the correct attribution immediately (from the cached `/me` user), and replayed queued mutations attribute to the acting user server-side.
- Attribution survives profile renames (stored as user id, name resolved via join at read time).

## Technical Key Decisions and Tradeoffs

1. **Store user UUIDs, resolve names via join:** `lists.created_by`, `items.added_by`, `items.bought_by` (all `uuid NULL`), LEFT JOINed against `members` for the display name.
   - Why: normalized and rename-proof (user decision over a name snapshot).
   - Impact: identity must become UUID-addressable (decision 2), and the acting user id must be plumbed through handlers → Service → Repository.
2. **Extend `members` with `user_id uuid NOT NULL UNIQUE`:** stamped on every upsert (all three auth modes already upsert on login/startup), with a one-time SQL backfill: subjects that parse as UUIDs cast directly (dev/password mode), all others via `uuid_generate_v5('6f9619ff-8b86-d011-b42d-00c04fc964ff', subject)` (uuid-ossp), which is bit-identical to Go's `uuid.NewSHA1` in `auth/oidc.go:435`.
   - Why: `members` is already the just-in-time identity table for every auth mode; only its key (text subject) is wrong for our purpose.
   - Impact: needs `CREATE EXTENSION "uuid-ossp"` (requires `CREATE` privilege on the database — fine under the docker-compose superuser; a managed-Postgres deployment may need the extension pre-installed). Known caveat: an OIDC provider whose subjects *are* UUID strings (e.g. Keycloak) backfills to the cast value instead of the v5 value; the row self-heals on that user's next login (upsert overwrites `user_id`), and until then their *new* attributions render without a name (id-only). Accepted as a rare, self-healing edge.
3. **No FK from attribution columns to members:** the acting `auth.User.ID` is not guaranteed to have a matching `members.user_id` row — the dev startup upsert is best-effort (`cmd/serve.go:154` logs and continues), and the Decision-2 backfill edge can leave a mismatched `user_id` until the next login. A FK would turn those tolerable gaps into failed list/item writes. A missing member row simply resolves the name to null and the UI hides the line (except "you", which needs only the id). (Note: the OIDC/password *login* upserts are mandatory — they abort the login on failure — so post-login gaps are rare.)
4. **Attribution rides on the existing read models:** write methods that return a `List`/`Item` re-read the row through the (now-joining) `List()`/`Item()` selects, mirroring the existing `RenameList` write-then-re-read shape — so name resolution lives in exactly one select per entity and `RETURNING` never has to fake a join.
5. **API shape:** new `Attribution {id, name}` schema; `List.createdBy`, `Item.addedBy`, `Item.boughtBy` as **optional** (not nullable) `$ref` fields — the spec is OpenAPI 3.0.3, where `nullable` next to a `$ref` is silently ignored, and "absent" already means "unknown". `name` is nullable *inside* the object (member row may be missing). oapi-codegen generates `*Attribution` for an optional `$ref`, which is all the mappers need. Spec-first: edit `splitkauf.openapi.yaml`, then `make generate`.
6. **Semantics:** uncheck clears `bought_by`; re-check stamps the current actor; copy attributes the new list and all copied items to the copier; pre-existing rows stay NULL forever (line hidden). `AddItem` with `checked=true` (an offline check folded into a queued create) stamps **both** `added_by` and `bought_by` with the actor — mirroring how it already stamps `checked_at` at insert — so a folded offline check keeps its "Bought by you" after replay.
7. **Events unchanged:** SSE hints stay coarse reload signals; attribution arrives with the refetch.
8. **Migration is `000007`:** the draft (unimplemented) sqlite-backend plan proposed re-baselining at `000007`; it must renumber around this migration. A note is added to that plan (its `uuid_generate_v5` backfill is also Postgres-only — the sqlite re-baseline will need a Go-side equivalent).

## Current State

No attribution exists anywhere. The authenticated user is placed in the request context by `RequireAuth`, but only `GET /me` reads it — every mutating handler ignores the actor.

```
Browser (React)                     Go backend                        Postgres
┌─────────────────────┐   POST      ┌──────────────────────┐   SQL   ┌──────────────────────────┐
│ ListsOverview.tsx   │ ──────────► │ handlers_lists.go     │ ──────► │ lists(id, name,          │
│  · name, counts     │             │  auth.UserFrom() ──── │         │   created_at, updated_at)│
│ ListDetail.tsx      │             │  ▲ only GetMe uses it │         │ items(id, list_id, name, │
│  · ItemRow: name,   │             │ lists/service.go      │         │   qty, unit, note,       │
│    qty, note, badge │             │ lists/lists.go (port) │         │   checked, checked_at,…) │
└─────────────────────┘             │ adapters/db/lists.go  │         │  ── no user columns ──   │
                                    └──────────────────────┘          └──────────────────────────┘
```

- Identity: `auth.User{ID uuid, Name, Email}` (`auth/auth.go:32`). ID is the dev UUID, the password user's UUID, or UUIDv5 of the OIDC subject (`auth/oidc.go:432-436`).
- `members(subject text PK, email, name)` is upserted by all three auth modes: dev at startup (`cmd/serve.go:154` via `auth.DevMember()`), OIDC on callback (`auth/oidc.go:266`), password on login (`auth/password.go:147`). Subjects: dev/password = UUID string; OIDC = raw provider subject.
- There is no user-by-UUID lookup anywhere (members keyed by subject; `users.Repository` has only `Create`/`GetByUsername`).
- Reads: `listSelect` (`adapters/db/lists.go:44`) and `itemColumns` (`:52`); `scanList`/`scanItem` map rows.
- Frontend: overview rows `ListsOverview.tsx:74-96` (name + counts line); item rows `ItemRow` in `ListDetail.tsx:141-192` (stacked name/qty/note/unsynced inside `.item-text`). `/me` is fetched once in `App.tsx` under the local `meKey = ['me']`.
- Offline: every mutation is optimistic (`queries.ts`); optimistic `Item`/`List` objects are hand-built in `buildAddItemDefaults`/`buildCreateListDefaults`/`toggleChecked`.

## Desired End State

```
members: + user_id uuid NOT NULL UNIQUE   ← stamped on every upsert; SQL backfill
lists:   + created_by uuid NULL ──┐
items:   + added_by   uuid NULL ──┼── LEFT JOIN members ON user_id → display name
         + bought_by  uuid NULL ──┘

API:  List.createdBy / Item.addedBy / Item.boughtBy : {id, name|null} | null

UI:
┌────────────────────────────────┐      Open (2)
│ Weekly groceries               │      ┌─────────────────────────────┐
│ 3 open · 2 done · by Alex      │      │ ☐ Milk                      │
├────────────────────────────────┤      │   2 l                       │
│ Party supplies                 │      │   Added by Alex   Edit  Rm  │
│ 5 open · by you                │      ├─────────────────────────────┤
└────────────────────────────────┘      │ ☐ Bread                     │
                                        │   Added by you    Edit  Rm  │
                                        └─────────────────────────────┘
                                        Done (1)
                                        ┌─────────────────────────────┐
                                        │ ☑ Eggs                      │
                                        │   Bought by Maria Edit  Rm  │
                                        └─────────────────────────────┘
```

## Abstractions and Code Reuse

- New domain type `lists.Actor{ID uuid.UUID, Name string}` carried as `*Actor` on `List.CreatedBy` and `Item.AddedBy`/`Item.BoughtBy` (nil = unknown). The existing, unused `lists.User` (`lists/lists.go:58-61`) is removed in its favour.
- `members.Member` gains `UserID uuid.UUID`; every existing upsert call site supplies it (the auth layer already knows the correct UUID per mode).
- Repository write methods gain a `userID uuid.UUID` parameter (`CreateList`, `CopyList`, `AddItem`) or `*uuid.UUID` (`SetItemChecked`, nil on uncheck); reads gain the join. Write-then-re-read via `List()`/`Item()` reuses the single joined select (decision 4).
- Frontend reuses the stacked `.item-text` layout (attribution styled like `.item-note`), the existing `meKey` query (moved to `queries.ts` as `useMe()`), and the optimistic-update builders.

File tree of changes:

- `database/migrations/`
  - `000007_attribution.up.sql` / `.down.sql` — new columns + backfill
- `members/members.go` — `Member.UserID`, doc updates
- `adapters/db/members.go` — `Upsert` writes/`Get` scans `user_id`
- `auth/dev.go` (`DevMember`), `auth/oidc.go` (callback upsert), `auth/password.go` (login upsert) — supply `UserID`
- `lists/lists.go` — `Actor`, fields on `List`/`Item`, `Repository` signatures
- `lists/service.go` — actor parameters on `CreateList`/`CopyList`/`AddItem`/`CheckItem`/`UncheckItem` (`setChecked`)
- `lists/service_test.go` — fake repo + tests
- `adapters/db/lists.go` — joined selects, insert/update attribution, `scanList`/`scanItem`
- `adapters/db/lists_test.go`, `adapters/db/members_test.go` — coverage
- `splitkauf.openapi.yaml` — `Attribution` schema; `List`/`Item` fields
- `ports/rest/v1/` — regenerated `gen.go`; `api.go` (`ListService` interface signatures); `handlers_lists.go` reads `auth.UserFrom`, maps `Actor→Attribution`; fakes in `handlers_lists_test.go`, `publish_test.go`, `maxbody_test.go`
- `client/client.gen.go` — regenerated
- `frontend/src/api.ts` — `Attribution` type, `List`/`Item` fields
- `frontend/src/queries.ts` — `useMe()`/`meKey`, optimistic attribution in add/create/toggle
- `frontend/src/App.tsx`, `frontend/src/LoginForm.tsx` — use `useMe()`/`meKey` from queries
- `frontend/src/ListsOverview.tsx`, `frontend/src/ListDetail.tsx` — render attribution
- `frontend/src/index.css` — `.item-attribution`
- frontend tests: `ListsOverview.test.tsx`, `ListDetail.test.tsx`, `api.test.ts`, `offlineOutbox.test.tsx` (fixtures)
- `docs/user-stories/US-L.11-see-who-did-what.md` — new user story
- `docs/agents/plans/2026-08-11-sqlite-backend.md` — renumbering note

## Logging & Observability

No new logs. The existing upsert error paths (`login: upserting member failed`, `upserting member` — which abort the login; and the best-effort dev startup upsert) now also cover the `user_id` stamping. Handler errors for a missing context user reuse the `problem.Internal` path already used by `GetMe`.

## Implementation

### Phase 1: Migration 000007 — attribution columns and members.user_id

Dependencies: None.

Schema only. Committed on its own (repo rule: migrations in their own commit). A separate `chore(plans)` commit adds the renumbering note to the sqlite plan.

**Tasks**:
- [x] Create `database/migrations/000007_attribution.up.sql`:
  ```sql
  CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

  ALTER TABLE members ADD COLUMN user_id uuid;
  UPDATE members SET user_id = CASE
      WHEN subject ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
          THEN subject::uuid
      ELSE uuid_generate_v5('6f9619ff-8b86-d011-b42d-00c04fc964ff'::uuid, subject)
  END;
  ALTER TABLE members ALTER COLUMN user_id SET NOT NULL;
  CREATE UNIQUE INDEX members_user_id_idx ON members (user_id);

  ALTER TABLE lists ADD COLUMN created_by uuid;
  ALTER TABLE items ADD COLUMN added_by  uuid;
  ALTER TABLE items ADD COLUMN bought_by uuid;
  ```
- [x] Create `database/migrations/000007_attribution.down.sql` (drop the three attribution columns, the index, and `members.user_id`; leave the extension installed).
- [x] Commit the migration alone (`feat(db): …`).
- [x] Add a note to `docs/agents/plans/2026-08-11-sqlite-backend.md` that 000007 now exists (re-baseline must renumber, and the uuid-ossp backfill needs a Go-side equivalent in the sqlite world); commit separately as `chore(plans)`.

**Automated Verification**:
- [x] `make build` passes (embedded migration files parse/compile into the binary).
- [x] Up and down apply cleanly against a scratch Postgres: `docker compose up -d` the database, then `splitkauf migrate` (up to 000007), `splitkauf migrate --version 6` (down), `splitkauf migrate` again — no errors. (The adapter test suite skips without `SPLITKAUF_TEST_DATABASE_DSN` and never runs migrations itself, so this is the only executable check.)

### Phase 2: "Created by" on lists, end-to-end

Dependencies: Phase 1.

Identity stamping + list attribution through every layer, rendered on overview rows.

**Tasks**:
- [ ] `members/members.go`: add `UserID uuid.UUID` to `Member`; document that it is the stable auth UUID (dev/password id, or UUIDv5 of the OIDC subject).
- [ ] `adapters/db/members.go`: `Upsert` writes `user_id` (`ON CONFLICT … SET user_id = EXCLUDED.user_id`); `Get` scans it.
- [ ] Supply `UserID` at the three upsert call sites: `auth/dev.go` `DevMember()` (`DevUser.ID`), `auth/oidc.go` callback upsert (`subjectUUID(idToken.Subject)`), `auth/password.go` login upsert (`user.ID`).
- [ ] `lists/lists.go`: add `Actor{ID uuid.UUID; Name string}`; remove unused `User`; add `CreatedBy *Actor` to `List`; change port: `CreateList(ctx, name string, createdBy uuid.UUID)`, `CopyList(ctx, sourceID uuid.UUID, name string, actor uuid.UUID)`.
- [ ] `lists/service.go`: `CreateList`/`CopyList` accept `actor uuid.UUID` and pass it through.
- [ ] `adapters/db/lists.go`:
  - `listSelect` selects `l.created_by, m.name` via `LEFT JOIN members m ON m.user_id = l.created_by`; the `GROUP BY` clauses live at the two call sites (`Lists()` at `adapters/db/lists.go:72`, `List()` at `:95`) — both become `GROUP BY l.id, m.name`. (The one-row join on unique `user_id` cannot distort the item counts.)
  - `scanList` maps `(created_by, m.name)` → `*Actor` (nil when `created_by` is NULL; empty name when the member row is missing).
  - `CreateList` inserts `created_by` and re-reads via `r.List()` (decision 4 — replaces the bare `RETURNING` scan).
  - `CopyList` stamps `created_by` on the new list and re-reads via `r.List()` (drop the manual count fix-up).
- [ ] `lists/service_test.go`: extend the fake repo + assert the actor reaches `CreateList`/`CopyList`.
- [ ] `splitkauf.openapi.yaml`: add `Attribution` schema (`required: [id]`; `name` nullable) and an **optional** (not nullable — see Decision 5) `createdBy: $ref` on `List`; run `make generate` (regenerates `ports/rest/v1` and `client`).
- [ ] `ports/rest/v1/api.go`: update the hand-written `ListService` interface (`api.go:29-42`) — `CreateList`/`CopyList` gain the actor parameter.
- [ ] `ports/rest/v1/handlers_lists.go`: `CreateList`/`CopyList` read `auth.UserFrom(r.Context())` (missing → `problem.Internal`, like `GetMe`) and pass `u.ID`; `toList`/`toListWithItems` map `CreatedBy` → `Attribution`. Update the `ListService` fakes in `handlers_lists_test.go`, `publish_test.go`, and `maxbody_test.go`.
- [ ] `frontend/src/api.ts`: add `Attribution { id: string; name: string | null }`; `createdBy?: Attribution` on `List` (and inherited by `ListWithItems`).
- [ ] `frontend/src/queries.ts`: export `meKey` + `useMe()` (moved from `App.tsx`; `App.tsx` consumes it, and `LoginForm.tsx:27`'s `['me']` invalidation literal switches to the exported `meKey`); the optimistic list in `buildCreateListDefaults.onMutate` sets `createdBy` from `queryClient.getQueryData(meKey)`.
- [ ] `frontend/src/ListsOverview.tsx`: append `· by {label}` to the counts line, where `label` is `"you"` when `createdBy.id === me.id`, else `createdBy.name`; omit when `createdBy` is null or name is empty and not me. Extract a shared `attributionLabel(a, meId)` helper (used again in Phase 3).
- [ ] Tests: `ListsOverview.test.tsx` (by-you / by-name / hidden-when-null), `api.test.ts` fixture updates.
- [ ] Add `docs/user-stories/US-L.11-see-who-did-what.md` covering all three attributions (list + item scope; items land in Phase 3).

**Automated Verification**:
- [ ] `make generate && make build` — spec, generated code, and mappers compile.
- [ ] `make test` — Go unit + adapter + handler tests pass.
- [ ] `cd frontend && npm run test && npm run typecheck && npm run lint` pass.

**Manual Verification**:
- [ ] With dev auth, create a list → overview row shows "· by you"; pre-existing lists show no attribution.

### Phase 3: "Added by" / "Bought by" on items, end-to-end

Dependencies: Phase 2 (Actor type, Attribution schema, useMe, attributionLabel).

**Tasks**:
- [ ] `lists/lists.go`: add `AddedBy *Actor`, `BoughtBy *Actor` to `Item`; change port: `AddItem(…, addedBy uuid.UUID)`, `SetItemChecked(…, checkedBy *uuid.UUID)`.
- [ ] `lists/service.go`: `AddItem` takes `actor uuid.UUID`; `CheckItem`/`UncheckItem`/`setChecked` take the actor and pass `&actor` when checking, `nil` when unchecking (uncheck clears `bought_by` alongside `checked_at`).
- [ ] `adapters/db/lists.go`:
  - Item reads (`ListItems`, `Item`) select `added_by, bought_by` plus two member joins (`ma.name`, `mb.name`); `scanItem` maps both to `*Actor`.
  - `AddItem` inserts `added_by = actor`, and when `checked=true` also `bought_by = actor` (Decision 6 — a folded offline check replays as a single checked create, so `SetItemChecked` never runs; mirrors the existing `checked_at`-at-insert handling at `adapters/db/lists.go:236-239`); re-reads via `r.Item()`.
  - `SetItemChecked` writes `bought_by = checkedBy` (NULL on uncheck) and re-reads via `r.Item()`; `UpdateItem`/`RestoreItem` re-read via `r.Item()` so their responses carry attribution.
  - `CopyList` item-copy stamps `added_by = actor`, `bought_by = NULL`.
- [ ] `lists/service_test.go`: fake + tests — actor recorded on add; checked-create stamps both `added_by` and `bought_by`; check stamps `bought_by`; uncheck clears it; idempotent re-check of an already-checked item does NOT restamp (existing early-return keeps the original buyer).
- [ ] `splitkauf.openapi.yaml`: optional (not nullable) `addedBy` / `boughtBy` `$ref` fields on `Item`; `make generate`.
- [ ] `ports/rest/v1/api.go` + `handlers_lists.go`: `ListService` signatures for `AddItem`/`CheckItem`/`UncheckItem` gain the actor; handlers pass the context user's id; `toItem` maps both actors. Update the fakes in `handlers_lists_test.go`, `publish_test.go`, `maxbody_test.go`.
- [ ] `frontend/src/api.ts`: `addedBy?: Attribution`, `boughtBy?: Attribution` on `Item`.
- [ ] `frontend/src/queries.ts`: optimistic attribution from `meKey` — `buildAddItemDefaults.onMutate` sets `addedBy`; `toggleChecked` sets `boughtBy` to me when checking, null when unchecking.
- [ ] `frontend/src/ListDetail.tsx` `ItemRow`: after the note, render `<span className="item-attribution">Added by {label}</span>` for open items / `Bought by {label}` for checked items, via `attributionLabel`; omit when no label.
- [ ] `frontend/src/index.css`: `.item-attribution` (muted, small — visually consistent with `.item-note`).
- [ ] Tests: `ListDetail.test.tsx` (Added by you/name on open, Bought by on checked, hidden when unknown, uncheck clears optimistically), `offlineOutbox.test.tsx` fixture updates if needed.
- [ ] Docs: mention attribution in `docs/architecture.md` if its data model section lists columns; finalize `US-L.11`.

**Automated Verification**:
- [ ] `make generate && make build` passes.
- [ ] `make test` passes.
- [ ] `cd frontend && npm run test && npm run typecheck && npm run lint` pass.

**Manual Verification**:
- [ ] Add an item → "Added by you" appears immediately (optimistic) and survives a refetch.
- [ ] Check an item → it moves to Done with "Bought by you"; uncheck → back to Open with "Added by you" (no stale bought-by).
- [ ] With two browsers/users (password or OIDC mode), user B sees "Added by ‹A's name›" live via SSE refetch.
- [ ] Copy a list → the copy's overview row says "by you" and every copied item says "Added by you".
- [ ] Airplane-mode: add + check items offline → attributions show "you"; go online → replays keep the same attribution.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

### Phase 1

- **Decision 2's backfill was challenged and kept.** An advisory review argued the
  backfill is dead weight — "every auth mode upserts the member (stamping
  `user_id`) before any attributed write, so only post-migration logins matter".
  That premise is false: `RequireAuth` authorizes from the *persisted session*
  alone in both OIDC (`auth/oidc.go:330`) and password (`auth/password.go:181`)
  mode, with no upsert on that path. The `sessions` table survives the migration,
  so a member with a live pre-migration session can create attributions for the
  remaining lifetime of that session without ever re-logging in. Without the
  backfill their name would not resolve for that whole window. Backfill kept.
- **UUIDv5 equivalence verified, not assumed.** `uuid_generate_v5` on the
  subject `auth0|507f1f77bcf86cd799439011` yields
  `b10d9108-f47c-5127-b94a-0af1e5f31874`, bit-identical to Go's
  `uuid.NewSHA1(subjectNamespace, ...)` for the same input (checked
  independently on both sides, including empty and multi-byte subjects).
- **Unique index cannot collide.** Cast-branch values are v4 (`gen_random_uuid`)
  or the fixed dev id; v5-branch values always carry version nibble 5 — the two
  sets are disjoint by construction. `SET NOT NULL` cannot fail either: `subject`
  is `NOT NULL PRIMARY KEY` and the `CASE` has no NULL arm.
- The migration runner (golang-migrate over pgx) executes each file as a single
  `ExecContext`, i.e. one implicit transaction, so `CREATE EXTENSION` + DDL +
  backfill apply atomically.

## References

- `auth/oidc.go:432-436` — `subjectUUID` (UUIDv5 derivation the backfill mirrors)
- `adapters/db/lists.go:44-52` — `listSelect` / `itemColumns` (joins land here)
- `frontend/src/queries.ts:291-366` — `buildAddItemDefaults` (optimistic item shape)
- `docs/agents/plans/2026-08-11-sqlite-backend.md` — draft plan whose re-baseline must renumber past 000007
- Repo rules: migrations in their own commit; plan/doc commits as `chore(plans)`/`chore(docs)` (CLAUDE.md)
