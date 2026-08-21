---
date: 2026-08-21T19:39:37Z
git_commit: 9175b308b52368f97d3ceb69a4ad723254407e27
branch: main
topic: "usermerge + userls: operator CLI for merging user identities"
tags: [plan, cli, auth, members, users, attribution]
status: ready
---

# PLAN: `usermerge` + `userls` — Operator CLI for Merging User Identities

Two new operator CLI commands:

- `splitkauf userls` — list every known identity (local account, OIDC member,
  dev user) with its kind, identifier, user id, name, email, and last login, so
  the operator can identify accounts.
- `splitkauf usermerge <source> <target>` — merge one identity into another by
  rewriting the attribution columns (`lists.created_by`, `items.added_by`,
  `items.bought_by`) from the source user id to the target user id, then
  cleaning up the source identity. Any direction: local→oidc, oidc→local,
  local→local, oidc→oidc. The primary use case is migrating local-only users
  to OIDC accounts after an IdP is introduced.

Based on research: `docs/agents/research/2026-08-21-oidc-authn-implementation.md`.
Builds on the completed alignment plan
(`2026-08-21-align-oidc-with-password-auth.md`, committed through `9175b30`).

## Acceptance Criteria

- `splitkauf userls` lists every identity: kind (`local`/`oidc`/`dev`),
  identifier (username for local, subject otherwise), user id, name, email,
  last login — including local accounts that never logged in (shown as
  `never`) and OIDC/dev identities that have no local account.
- `splitkauf usermerge <source> <target>` accepts selectors
  `local:<username>`, `oidc:<subject>`, `uuid:<user_id>`, in any combination.
- The merge runs in one database transaction: rewrite the three attribution
  columns from source to target user id; delete the source `members` row;
  delete the source `users` row when the source is a local account; create a
  `members` row for the target when the target is local and has none.
- Before writing anything, the command prints the resolved identities and
  per-column row counts, then asks an interactive `y/N` confirmation. `--yes`
  skips the prompt; a non-TTY stdin without `--yes` is an error. Aborting
  performs zero writes.
- `source == target` (same resolved user id) is an error. An unresolvable
  selector is an error naming the selector.
- `oidc:<subject>` requires an existing `members` row for that subject;
  `uuid:` bypasses existence checks on `members` but still classifies the
  identity as local when a `users` row with that id exists.
- When the source identity can still log in (an `oidc:` source, or a `uuid:`
  source that is not a local account), the command prints a warning that the
  next OIDC login re-derives the same user id and recreates the member row.
- Live sessions of the source identity are untouched and last until scs
  expiry (documented limitation, matching existing deleted-account behavior).
- All tests pass; `deploy/README.md` and `docs/architecture.md` document both
  commands.

## Technical Key Decisions and Tradeoffs

1. **Attribution rewrite, not identity indirection:** the merge rewrites the
   three attribution columns to the surviving user id.
   - Why: no change to the auth flow; orthogonal to the just-completed
     OIDC/password alignment. Attribution volume is small for this app.
   - Impact: in local→oidc merges the OIDC-derived UUID is the survivor,
     because OIDC mode derives the user id from the subject at every login
     (`auth/oidc.go` `subjectUUID`) and never reads it from `members`.
2. **Generic direction, one mechanic:** any identity merges into any other.
   - Why: the SQL is identical regardless of direction.
   - Impact: per-kind logic exists only in selector resolution and cleanup
     (delete `users` row / seed target `members` row).
3. **Prefixed positional selectors:** `usermerge <source> <target>` with
   `local:`/`oidc:`/`uuid:` prefixes; no email or preferred_username
   addressing (`preferred_username` is not persisted anywhere).
   - Why: unambiguous, matches the `useradd <username>` positional style.
   - Impact: `userls` is the discovery tool for subjects and UUIDs.
4. **`oidc:` resolves via the `members` row, not by deriving the UUID:** the
   command reads `members.Get(subject)` and uses its `user_id`.
   - Why: requiring the row proves the account has logged in at least once,
     guarding against a typoed subject silently merging into a void UUID; it
     also avoids exporting `auth.subjectUUID`.
   - Impact: an OIDC target must log in once before it can be merged into by
     subject (`uuid:` remains the escape hatch).
5. **Sessions untouched:** no attempt to invalidate the source's live
   sessions.
   - Why: scs stores session data as opaque bytea; sessions are not
     queryable per user. RequireAuth never re-checks the `users` table, so
     this matches the existing behavior for deleted accounts.
   - Impact: documented limitation; sessions expire with the scs lifetime.
6. **New `IdentityRepository` in `adapters/db`, no new domain package:** the
   listing, resolution, counting, and merge queries live in one new adapter
   file with small exported structs.
   - Why: this is operator tooling that spans four tables (`users`,
     `members`, `lists`, `items`); the CLI already talks to `adapters/db`
     directly (`cmd/useradd.go`). A domain port would have a single caller.
   - Impact: `cmd/userls.go` and `cmd/usermerge.go` depend on `adapters/db`
     types directly, like `useradd` does.

## Current State

```
Login (any mode)
   │
   ▼
auth.User.ID (uuid) — derivation per mode:
   ├─ password: users.id (from the users table)
   ├─ OIDC:     UUIDv5(subject) — derived at login, never looked up
   └─ dev:      fixed 00000000-0000-0000-0000-000000000001
   │
   ▼ JIT upsert on every login (user_id re-stamped each time)
members (subject text PK, user_id uuid UNIQUE, email, name,
         created_at, updated_at — updated_at refreshed per login)
   ▲
   │ LEFT JOIN on user_id — display-name resolution only, no FK
   │
attribution columns (store the raw user_id):
   lists.created_by, items.added_by, items.bought_by

users    — local accounts (id, username, password_hash, name, email)
sessions — scs, opaque bytea JSON, not queryable per user
```

When a person starts as a local user and later signs in via OIDC, two
unrelated user ids exist: the local `users.id` and the OIDC-derived
UUIDv5(subject). Past attribution stays under the local id; new attribution
accrues under the OIDC id. There is no way to unify them.

Key code:

- `cmd/useradd.go:28` — existing provisioning CLI (cobra pattern, DB
  connection via `db.NewSQL(config.C.Database.DSN())`, 10s timeout).
- `cmd/root.go:14` — root command; subcommands self-register in `init()`.
- `adapters/db/members.go:34` — `Upsert` re-stamps `user_id` on every login.
- `adapters/db/members.go:51` — `Get` by subject, `members.ErrNotFound`.
- `adapters/db/users.go:56` — `GetByUsername` (no `GetByID` exists).
- `adapters/db/lists.go:53-73` — display-name JOINs on `members.user_id`.
- `auth/dev.go:22` — exported `auth.DevUser` (fixed dev UUID).
- `database/migrations/000007_attribution.up.sql` — attribution columns,
  `members.user_id` UNIQUE, deliberately no FK.
- `adapters/db/members_test.go:20` — integration-test pattern:
  `SPLITKAUF_TEST_DATABASE_DSN`, skip in `-short`, TRUNCATE per test.

## Desired End State

```
$ splitkauf userls
KIND   IDENTIFIER                            USER_ID                               NAME      EMAIL              LAST_LOGIN
local  alex                                  0d9c1e64-…                            Alex      alex@example.com   2026-08-20 19:04
local  maria                                 7be20a11-…                            Maria     —                  never
oidc   238941579532                          a3f8c9d2-…                            Alex S.   alex@schink.xyz    2026-08-21 08:12
dev    00000000-0000-0000-0000-000000000001  00000000-0000-0000-0000-000000000001  Dev User  —                  2026-08-19 11:30

$ splitkauf usermerge local:alex oidc:238941579532
Merge plan:
  source: local:alex          (user_id 0d9c1e64-…, "Alex", alex@example.com)
  target: oidc:238941579532   (user_id a3f8c9d2-…, "Alex S.", alex@schink.xyz)
  lists.created_by:  3 rows
  items.added_by:   41 rows
  items.bought_by:  17 rows
  will delete: local account "alex", members row of source
Proceed? [y/N]: y
Merged local:alex into oidc:238941579532 (lists: 3, added: 41, bought: 17).
```

Merge transaction (single `BEGIN`/`COMMIT`):

```sql
UPDATE lists SET created_by = $target WHERE created_by = $source;
UPDATE items SET added_by   = $target WHERE added_by   = $source;
UPDATE items SET bought_by  = $target WHERE bought_by  = $source;
DELETE FROM members WHERE user_id = $source;          -- covers all kinds
DELETE FROM users   WHERE id = $source;               -- only when source is local
INSERT INTO members (subject, user_id, email, name)   -- only when target is local
VALUES ($target::text, $target, $email, $name)        -- and has no members row;
ON CONFLICT (subject) DO NOTHING;                     -- so names resolve now
```

`userls` query: `users FULL OUTER JOIN members ON members.subject = users.id::text`,
kind = `local` when the `users` row exists, `dev` when the subject equals the
dev UUID string, else `oidc`; last login = `members.updated_at` (refreshed by
the JIT upsert on every login), `never` when there is no members row.

## Abstractions and Code Reuse

Reused: `db.NewSQL` + `config.C.Database.DSN()` connection pattern and the 10s
timeout from `cmd/useradd.go`; `terminalFd` (`cmd/useradd.go:124`) for the TTY
check of the confirmation prompt; `db.NewUserRepository().GetByUsername` and
`db.NewMemberRepository().Get` for `local:`/`oidc:` resolution;
`auth.DevUser.ID` for the dev-kind classification; the
`SPLITKAUF_TEST_DATABASE_DSN` integration-test pattern.

New:

- `adapters/db/`
  - `identity.go` — new `IdentityRepository` over `*sql.DB`:
    - `Identity` struct — `Kind` (`local`/`oidc`/`dev`), `Identifier`,
      `UserID uuid.UUID`, `Name`, `Email`, `LastLogin *time.Time`.
    - `List(ctx) ([]Identity, error)` — the FULL OUTER JOIN above, ordered by
      kind then identifier.
    - `ResolveUUID(ctx, id uuid.UUID) (Identity, error)` — classifies a raw
      UUID: `users` row with that id ⇒ local; else `members` row with that
      user_id ⇒ oidc/dev; else a bare identity with `Kind` oidc-like unknown
      is NOT invented — returns the Identity with kind derived from what was
      found, or a zero-match `Identity{Kind: "unknown", UserID: id}` (merge
      still allowed; cleanup skips users/members deletes that match nothing).
    - `CountAttribution(ctx, userID) (lists, added, bought int, err error)` —
      the three `SELECT count(*)` queries for the confirmation prompt.
    - `MergeResult` struct — `Lists, Added, Bought int64`: the
      `RowsAffected` of the three attribution UPDATEs, for the final report.
    - `Merge(ctx, source Identity, target Identity) (MergeResult, error)` —
      the transaction above; `source.Kind == "local"` gates the `users`
      delete, `target.Kind == "local"` gates the members seed (email/name
      taken from the target's `users` row values in the Identity).
  - `identity_test.go` — integration tests (pattern of `members_test.go`).
- `cmd/`
  - `userls.go` — `userlsCmd`: connect, `IdentityRepository.List`, render
    with `text/tabwriter`; `—` for empty email, `never` for nil last login.
  - `usermerge.go` — `usermergeCmd` (`cobra.ExactArgs(2)`, `--yes` flag):
    - `parseSelector(s string) (kind, value string, err error)` — splits on
      the first `:`, validates the prefix.
    - resolution: `local:` → `GetByUsername` (hash discarded); `oidc:` →
      `MemberRepository.Get` (kind `dev` when the subject is the dev UUID
      string); `uuid:` → `uuid.Parse` + `ResolveUUID`.
    - print plan (counts via `CountAttribution`), warn when the source can
      still log in (kind ≠ local), confirm via TTY prompt or `--yes`
      (non-TTY without `--yes` ⇒ error, mirroring the `useradd`
      "use --password-stdin" pattern), then `Merge`.
  - `usermerge_test.go` — unit tests for `parseSelector` and the
    confirmation gating (no DB).

No migrations. No API, frontend, or auth-flow changes.

## Logging & Observability

CLI commands write human-readable output to stdout/stderr (cobra style, like
`useradd`); they do not use the zap logger. The merge prints the plan before
confirmation and a final `Merged <source> into <target>.` line with the
per-column updated-row counts returned from the transaction. Warnings
(`source can still log in via the IdP; …`) go to stderr.

## Implementation

### Phase 1: `userls` — identity listing

Dependencies: None.

Deliver the discovery command: the `IdentityRepository` with the listing
query, the cobra command, tests, and docs. After this phase the operator can
identify every account and copy selector values for Phase 2.

**Tasks**:
- [x] `adapters/db/identity.go` (new): `Identity` struct,
  `IdentityRepository`, `NewIdentityRepository(*sql.DB)`, and `List(ctx)`
  implementing the FULL OUTER JOIN over `users`/`members` with kind
  classification (`local` when the users row exists, `dev` when the subject
  equals `auth.DevUser.ID.String()`, else `oidc`) and
  `LastLogin = members.updated_at` (nil without a members row). Do the dev
  comparison in Go against the scanned subject, not in SQL, so the dev UUID
  constant stays single-sourced in `auth`.
- [x] `cmd/userls.go` (new): `userlsCmd` ("List all known user identities"),
  connect via `db.NewSQL(config.C.Database.DSN())` with the 10s timeout
  pattern from `useradd`, render `KIND IDENTIFIER USER_ID NAME EMAIL
  LAST_LOGIN` with `text/tabwriter`; `—` for empty email/name, `never` for
  nil last login, timestamps as `2006-01-02 15:04`. Register in `init()`.
- [x] `adapters/db/identity_test.go` (new): integration tests following
  `members_test.go` (TRUNCATE `users`, `members`; skip without DSN):
  local-never-logged-in row (`LastLogin` nil), local-with-member row joins to
  one entry, oidc-only member row, dev member row classified `dev`.
- [x] `deploy/README.md`: document `splitkauf userls` next to the existing
  `useradd` operator docs (what it shows, that IDENTIFIER is the
  `usermerge` selector value).

**Automated Verification**:
- [x] `go build ./...` passes.
- [x] `go vet ./...` passes.
- [x] `go test ./...` passes (unit); integration tests pass with
  `SPLITKAUF_TEST_DATABASE_DSN` set.

**Manual Verification**:
- [ ] Against a dev database with a local account and a dev login:
  `splitkauf userls` shows both, with `never` for a fresh `useradd` account.

### Phase 2: `usermerge` — merge command

Dependencies: Phase 1 (`IdentityRepository`, selector values discoverable).

Deliver the merge: selector parsing and resolution, the transactional merge
with cleanup, the confirmation UX with counts and warnings, tests, and docs.

**Tasks**:
- [ ] `adapters/db/identity.go`: add `ResolveUUID(ctx, id)` (classify a raw
  UUID via `users` then `members`; zero matches ⇒ `Kind: "unknown"` with the
  id set), `CountAttribution(ctx, userID)` (three counts), and
  `Merge(ctx, source, target Identity) (MergeResult, error)` — one
  transaction: three attribution UPDATEs (capture `RowsAffected` into
  `MergeResult`), `DELETE FROM members WHERE user_id = source`,
  `DELETE FROM users WHERE id = source` only when `source.Kind == "local"`,
  and when `target.Kind == "local"` an
  `INSERT INTO members … ON CONFLICT (subject) DO NOTHING` seeding
  `(target.UserID.String(), target.UserID, target.Email, target.Name)`.
  Rollback on any error.
- [ ] `cmd/usermerge.go` (new): `usermergeCmd` with `cobra.ExactArgs(2)` and
  `--yes`; `parseSelector` (`local:`/`oidc:`/`uuid:` prefixes, error naming
  the bad selector otherwise); resolution per kind (`local:` via
  `GetByUsername` mapping `users.ErrNotFound` to a clear error; `oidc:` via
  `MemberRepository.Get` mapping `members.ErrNotFound` to an error telling
  the operator the account must have logged in once, kind `dev` when the
  subject is the dev UUID string; `uuid:` via `uuid.Parse` +
  `ResolveUUID`); error when both resolve to the same user id.
- [ ] `cmd/usermerge.go`: confirmation flow — print the merge plan (resolved
  identities with kind, user id, name, email; counts from
  `CountAttribution`; the cleanup lines that apply), print the
  source-can-still-log-in warning to stderr when `source.Kind != "local"`,
  then prompt `Proceed? [y/N]:` reading one line from stdin (accept `y`/`Y`);
  `--yes` skips the prompt; non-TTY stdin (reuse `terminalFd`) without
  `--yes` ⇒ error `confirmation required; use --yes`. On success print one
  line with the `MergeResult` counts:
  `Merged <source> into <target> (lists: N, added: N, bought: N).`
- [ ] `adapters/db/identity_test.go`: integration tests for the merge
  (TRUNCATE `users`, `members`, `lists`, `items`): local→oidc merge rewrites
  all three attribution columns, deletes the source users and members rows,
  and leaves the target member row intact; oidc→local merge seeds the target
  members row (display-name JOIN resolves) and deletes the source members
  row; `ResolveUUID` classification for local, member-only, and unknown ids;
  `CountAttribution` counts; merge with zero attribution rows succeeds.
- [ ] `cmd/usermerge_test.go` (new): unit tests for `parseSelector` (valid
  prefixes, missing prefix, empty value) and the non-TTY-without-`--yes`
  error path.
- [ ] `deploy/README.md`: operator guide section for `usermerge` — selector
  syntax, the local→oidc migration walkthrough (create IdP account, user
  logs in once, `userls`, `usermerge local:<name> oidc:<subject>`), the
  still-can-log-in warning, and the sessions limitation (live source
  sessions last until scs expiry).
- [ ] `docs/architecture.md`: add both commands to the CLI/operations
  coverage (same place `useradd` is described); note that attribution merge
  rewrites `created_by`/`added_by`/`bought_by` and that identity unification
  is an operator action, not an auth-flow feature.
- [ ] `docs/user-stories/US-A.7-operator-provisions-accounts.md`: add a
  cross-reference note that account consolidation is covered by
  `usermerge` (no new user story).

**Automated Verification**:
- [ ] `go build ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `go test ./...` passes (unit); integration tests pass with
  `SPLITKAUF_TEST_DATABASE_DSN` set.
- [ ] `grep -n "usermerge\|userls" deploy/README.md docs/architecture.md`
  shows both commands documented.

**Manual Verification**:
- [ ] End-to-end local→oidc migration on a dev stack: create a local account,
  create lists/items with it, sign in via OIDC once, run
  `usermerge local:<name> oidc:<subject>` — attribution names resolve to the
  OIDC identity in the UI, the local login no longer works, `userls` no
  longer shows the local account.
- [ ] Abort path: answering `N` performs no changes (`userls` and the UI
  unchanged).

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

## References

- Research: `docs/agents/research/2026-08-21-oidc-authn-implementation.md`
- Prior plans: `docs/agents/plans/2026-08-21-align-oidc-with-password-auth.md`
  (completed), `docs/agents/plans/2026-08-01-m7-username-password-auth.md`,
  `docs/agents/plans/2026-08-12-list-item-attribution.md`
- `database/migrations/000007_attribution.up.sql` — attribution model
- `docs/architecture.md` §6 (auth), `deploy/README.md` (operator guide)
- User story: US-A.7 (operator provisions accounts)
