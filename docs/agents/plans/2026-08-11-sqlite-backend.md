---
date: 2026-08-11T17:08:28+00:00
git_commit: 6f54974d0a1efaecd5f76a5a0ee3b8a8bb6daf28
branch: main
topic: "Alternative SQL backend: SQLite"
tags: [plan, database, adapters-db, config, cmd, migrations]
status: draft
---

# PLAN: Alternative SQL backend — SQLite

> **Stale numbering (2026-08-12):** this plan was written when 000006 was the
> last migration, so it claims 000007 for both the legacy transition and the
> common baseline. `000007_attribution` has since landed (list/item attribution,
> `members.user_id`). Everything numbered 000007 below must shift to 000008, and
> the legacy chain is now 000001–000007. Note also that
> `000007_attribution.up.sql` backfills `members.user_id` with
> `uuid_generate_v5` from the Postgres-only `uuid-ossp` extension — the portable
> baseline cannot express that, so the SQLite world needs a Go-side equivalent
> (or a baseline that simply starts with `user_id` already populated).

Add SQLite as a second, fully supported SQL backend next to PostgreSQL, selected
by config (`database.driver`). The target use case is small single-node
deployments that should not need a Postgres server. The default stays
`postgres`; nothing changes for existing deployments or the local dev workflow.

## Acceptance Criteria

- Setting `SPLITKAUF_DATABASE_DRIVER=sqlite` and `SPLITKAUF_DATABASE_PATH` runs
  the full app (`serve`, `migrate`, `useradd`) on a local SQLite file with no
  cgo and no PostgreSQL.
- Existing Postgres deployments upgrade in place: legacy migrations 000001–000006
  stay byte-identical, a new transition migration 000007 applies cleanly, and
  behavior is unchanged afterwards.
- Fresh installs of both backends converge on the same logical schema; both
  backends report the same `schema_migrations` version numbers forever; every
  future migration is written exactly once in portable SQL.
- Sessions are durable under sqlite (scs `sqlite3store`); OIDC mode works with
  sqlite.
- Adapter integration tests run against a temp-file SQLite database
  unconditionally (including `-short` / CI); Postgres tests stay gated on
  `SPLITKAUF_TEST_DATABASE_DSN`. `make check` passes.
- Default behavior unchanged (`database.driver` defaults to `postgres`);
  `docker-compose.yaml` and the dev workflow untouched; sqlite mode documented.

## Technical Key Decisions and Tradeoffs

1. **SQLite driver: `modernc.org/sqlite` (pure Go):** no cgo.
   - Why: the build stays `CGO_ENABLED=0` everywhere (Docker, CI, cross-compile);
     golang-migrate ships a matching `database/sqlite` driver for it.
   - Impact: slightly slower than mattn/go-sqlite3 — irrelevant at this app's
     scale. Driver registers as `"sqlite"` with `database/sql`.
2. **One shared migrations folder (`common/`) for both backends, plus a frozen
   `legacy_postgres/` history with a special transition migration:**
   - `database/migrations/common/` starts at `000007_baseline` (full schema in
     portable SQL) and holds all future migrations, written once.
   - `database/migrations/legacy_postgres/` holds 000001–000006 (moved,
     byte-identical) plus `000007_to_common`, which converts the legacy schema
     to exactly the common baseline (drops `gen_random_uuid()`/`now()` column
     defaults).
   - **Postgres always runs the legacy chain** (fresh installs too): source =
     `legacy_postgres/*` + `common/*` minus `000007_baseline`. SQLite runs
     `common/*` only. This is a static composition rule — no runtime
     schema-version detection, no duplicate-version conflicts. A fresh Postgres
     ends up at the identical schema via 000001–000007.
   - Why: honors "never edit applied migrations" while giving one shared,
     write-once folder going forward.
   - Impact: needs a small merged/filtered `fs.FS` over the embedded tree.
3. **App-generated UUIDs and timestamps:** repos pass `uuid.New()` and
   `time.Now()` explicitly on every insert; DB column defaults are dropped.
   - Why: SQLite has no `gen_random_uuid()`; supplying values from Go works
     identically on both engines, so repository SQL stays shared and
     dialect-free (SQLite ≥3.35 already supports `$N` placeholders,
     `RETURNING`, and `ON CONFLICT DO UPDATE`).
   - Impact: `users.Create`, `members.Upsert`, and `CopyList` must stop relying
     on DB defaults / `now()`; `CopyList` is rewritten portably (its
     `FOR KEY SHARE` lock becomes the one driver-conditional clause in the
     repos); portable-SQL round-trip tests guard `$N`, `RETURNING`,
     `time.Time`, and `uuid.UUID` scanning under modernc.
4. **Config: `database.driver` (`postgres`|`sqlite`, default `postgres`) +
   `database.path`:** existing host/port/user/… keys stay and are ignored under
   sqlite.
   - Why: zero breakage for existing env-var deployments.
   - Impact: `DatabaseConfig.DSN()` becomes driver-aware; validation rejects
     unknown drivers and an empty path under sqlite.
5. **Sessions: scs `sqlite3store` on the same `*sql.DB` when driver=sqlite:**
   one portable `sessions` DDL in the baseline.
   - Why: `sqlite3store` is plain `database/sql` code (works with modernc);
     SQLite's dynamic typing stores its julian-day REALs and BLOBs fine in
     columns declared `timestamptz`/`bytea`.
   - Impact: the `sessionStore` policy treats sqlite as always reachable —
     sessions are always durable; the in-memory fallback and OIDC fail-fast
     gate remain postgres-only concerns.
6. **SQLite connection settings:** DSN pragmas `foreign_keys(1)` (FK
   enforcement is off by default in SQLite and `AddItem`'s not-found mapping
   depends on it), `busy_timeout(5000)`, `journal_mode(WAL)`; plus
   `SetMaxOpenConns(1)` to sidestep `SQLITE_BUSY` write contention entirely.
   - Why: correctness first; a shopping-list app does not need concurrent
     writers.
   - Impact: applied inside `db.NewSQL` for the sqlite branch only.
7. **Dev workflow unchanged:** postgres remains the default and
   `docker-compose.yaml` is untouched; sqlite is documented as a deployment
   option.

## Current State

```
config.DatabaseConfig (host/port/user/password/name/ssl_mode)
        │ DSN() → libpq keyword/value string
        ▼
adapters/db/db.go: NewSQL(dsn) ── sql.Open("pgx", dsn)          [pgx v5 stdlib]
        │
        ├─► adapters/db/lists.go    ListsRepository   (lists.Repository)
        ├─► adapters/db/members.go  MemberRepository  (members.Repository)
        ├─► adapters/db/users.go    UserRepository    (users.Repository)
        ├─► cmd/serve.go:89         scs postgresstore (session store)
        └─► database/migrations.go  golang-migrate + postgres driver
                └── go:embed migrations/  (000001–000006, Postgres dialect)
```

Postgres-specific surface:

| Area | Postgres-ism | Where |
|---|---|---|
| DDL | `pgcrypto`, `gen_random_uuid()` / `now()` defaults, `timestamptz`, `bytea` | `database/migrations/*.sql` |
| DML | relies on DB defaults for id/created_at/updated_at; `now()` in upsert | `users.go:36`, `members.go:29` |
| DML | `CopyList`: `FOR KEY SHARE`, `$3::timestamptz` cast, `interval '1 microsecond'` arithmetic, id-less `INSERT … SELECT` | `lists.go:174-225` |
| Errors | `pgconn.PgError` SQLSTATE 23503 / 23505 | `lists.go:248`, `users.go:46` |
| Sessions | `scs/postgresstore` | `cmd/serve.go:89` |
| Migrations | `migrate/database/postgres` driver, single embed dir | `database/migrations.go:137-152` |
| Config | libpq DSN only | `config/config.go:140-160` |
| Tests | DSN-gated, `TRUNCATE ... CASCADE` | `adapters/db/*_test.go` |

## Desired End State

```
config.DatabaseConfig (+ driver, path)
        │ DSN() → libpq string  |  sqlite file URI with pragmas
        ▼
adapters/db/db.go: NewSQL(driver, dsn)
        │     driver=postgres → sql.Open("pgx", …)
        │     driver=sqlite   → sql.Open("sqlite", …) + SetMaxOpenConns(1)
        │
        ├─► shared repos (unchanged SQL; ids/timestamps from Go;
        │                 errors.go maps pgconn + modernc constraint errors)
        ├─► cmd/serve.go: postgresstore | sqlite3store  (by driver)
        └─► database/migrations.go: driver-aware migrator
                ├── go:embed migrations/common          (000007_baseline, 000008+…)
                ├── go:embed migrations/legacy_postgres (000001–000006, 000007_to_common)
                └── composed fs.FS:
                      postgres: legacy_postgres/* + common/* − 000007_baseline
                      sqlite:   common/*
```

Target schema after version 7 (identical logical shape on both engines): the
current schema minus the function defaults (`DEFAULT gen_random_uuid()` /
`DEFAULT now()`). Literal defaults (`quantity DEFAULT 1`, `checked DEFAULT
false`, `unit DEFAULT 'amount'`, `users.name DEFAULT ''`) are portable SQL and
are KEPT on both engines, so the common baseline declares them and the
transition migration leaves them alone. NOT NULL constraints stay.

## Abstractions and Code Reuse

The three repositories, the domain ports, and all HTTP/auth code stay
backend-agnostic and untouched in structure — only the insert statements gain
explicit id/timestamp arguments, and error inspection moves behind two shared
helpers. New abstractions: a driver-string constant pair in `config`, a merged
`fs.FS` for migration composition, and the error-mapping helpers.

- `config/`
  - `config.go` — `DatabaseConfig`: add `Driver`, `Path` fields; `DSN()`
    driver-aware (sqlite: `file:<path>?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)`);
    add `DriverPostgres`/`DriverSQLite` constants.
  - `defaults.go` — `database.driver: "postgres"`, `database.path: "./splitkauf.db"`.
  - `validation.go` (wherever `validate` lives) — reject unknown driver; require
    non-empty path when sqlite.
- `adapters/db/`
  - `db.go` — `NewSQL(driver, dsn string)`; sqlite branch opens `"sqlite"`,
    sets `SetMaxOpenConns(1)`; blank-import `modernc.org/sqlite`.
  - `errors.go` (new) — `isUniqueViolation(err)`, `isForeignKeyViolation(err)`
    recognizing `pgconn.PgError` (23505/23503) and modernc `*sqlite.Error`
    (codes 2067 `SQLITE_CONSTRAINT_UNIQUE`, 787 `SQLITE_CONSTRAINT_FOREIGNKEY`,
    1555 `SQLITE_CONSTRAINT_PRIMARYKEY`).
  - `lists.go` — `CreateList`/`AddItem` pass `uuid.New()` as id; FK check via
    helper; `CopyList` rewritten portably (read-then-insert loop, app-computed
    staggered timestamps, driver-conditional `FOR KEY SHARE`); repositories'
    constructors take the driver so the lock clause can be postgres-only.
  - `users.go` — `Create` passes id + timestamps; unique check via helper.
  - `members.go` — `Upsert` passes created_at/updated_at; `ON CONFLICT … SET
    updated_at = $4` instead of `now()`.
- `database/`
  - `migrations/common/` — `000007_baseline.{up,down}.sql`, future shared files.
  - `migrations/legacy_postgres/` — moved 000001–000006 +
    `000007_to_common.{up,down}.sql`.
  - `migrations.go` — `newMigrator(db, driver)`: composed source FS per driver;
    postgres → `migrate/database/postgres`, sqlite → `migrate/database/sqlite`.
  - `source.go` (new) — merged/filtered `fs.FS` implementation.
- `cmd/`
  - `serve.go` — pass driver to `NewSQL`; store selection
    postgresstore/sqlite3store; `sessionStore` policy: sqlite ⇒ durable.
  - `migrate.go`, `useradd.go` — pass driver to `NewSQL` and migrator.
- `docs/architecture.md`, `README.md` — document the sqlite mode.

## Logging & Observability

- `serve` startup logs the active database driver:
  `{"msg":"database configured","driver":"sqlite","path":"/data/splitkauf.db"}`
  (path only for sqlite; never log the postgres password — reuse the existing
  DSN-free style).
- Migration logs gain the driver field:
  `{"msg":"syncing database schema","driver":"sqlite"}`.
- No metrics changes; the existing health ping (`v1/api.go:104`) works for both.

## Implementation

### Phase 1: Driver-aware config and connection layer

Dependencies: None

Introduce backend selection in config and the connection constructor, add the
modernc dependency, and prove the portable-SQL assumptions (`$N` placeholders,
`RETURNING`, `time.Time`/`uuid.UUID` round-trips) with tests before any
migration work builds on them.

**Tasks**:
- [ ] `go get modernc.org/sqlite` (and `go mod tidy`).
- [ ] `config/config.go`: add `Driver string` and `Path string` to
  `DatabaseConfig` (mapstructure `driver`, `path`); add exported constants
  `DriverPostgres = "postgres"`, `DriverSQLite = "sqlite"`.
- [ ] `config/config.go`: make `DSN()` driver-aware — postgres branch unchanged;
  sqlite branch returns
  `file:<path>?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)`.
- [ ] `config/defaults.go`: `v.SetDefault("database.driver", "postgres")`,
  `v.SetDefault("database.path", "./splitkauf.db")`.
- [ ] `config/validation.go`: error on a driver other than `postgres`/`sqlite`;
  error on empty `database.path` when driver is sqlite; make the existing
  `database.port`/`database.name`/`database.user` checks (`validation.go:36-44`)
  postgres-only, since those keys are ignored under sqlite.
- [ ] `adapters/db/db.go`: change to `NewSQL(driver, dsn string)`; sqlite branch
  uses `sql.Open("sqlite", dsn)` + `SetMaxOpenConns(1)`; blank-import
  `modernc.org/sqlite`. Update the three callers (`cmd/serve.go`,
  `cmd/migrate.go`, `cmd/useradd.go`) mechanically (behavior for postgres
  unchanged).
- [ ] New test `adapters/db/sqlite_dialect_test.go`: against a `t.TempDir()`
  sqlite file, create a scratch table and verify `$1`-style placeholders,
  `RETURNING`, `ON CONFLICT DO UPDATE`, FK-violation error surfacing (pragma
  on), and `time.Time` + `uuid.UUID` round-trips through columns declared
  `timestamptz` / `uuid`. This test runs in `-short` mode.
- [ ] Config unit tests: driver default, sqlite DSN shape, validation errors.

**Automated Verification**:
- [ ] `go test ./config/... ./adapters/db/... -short -race` passes (dialect test
  included, no external DB).
- [ ] `make check` passes.

### Phase 2: Migrations and repositories on SQLite

Dependencies: Phase 1

Restructure the migration tree, add the postgres transition and the portable
baseline, make the migrator driver-aware, move id/timestamp generation into the
repositories, and run the full adapter test suite against sqlite.

Commit discipline (per project rules): the migration-file restructure/additions
land in their own commit, separate from the Go changes.

**Tasks**:
- [ ] Move `database/migrations/0000{01..06}_*.sql` to
  `database/migrations/legacy_postgres/` byte-identical.
- [ ] New `database/migrations/legacy_postgres/000007_to_common.{up,down}.sql`:
  `ALTER TABLE … ALTER COLUMN … DROP DEFAULT` for every
  `gen_random_uuid()`/`now()` default (lists.id/created_at/updated_at,
  items.id/created_at/updated_at, members.created_at/updated_at,
  users.id/created_at/updated_at); down restores the defaults.
- [ ] New `database/migrations/common/000007_baseline.{up,down}.sql`: full
  schema in portable SQL — current tables/indexes/CHECKs, no extensions, no
  function defaults (literal defaults `quantity DEFAULT 1`, `checked DEFAULT
  false`, `unit DEFAULT 'amount'`, `users.name DEFAULT ''` are kept — they are
  portable), types spelled `uuid`/`timestamptz`/`bytea`/`text`/… (SQLite
  treats them as affinities); down drops all tables. Add a header comment:
  "runs on postgres AND sqlite — portable SQL only".
- [ ] `database/source.go` (new): `migrationFS(driver string) (fs.FS, error)`
  returning `common/` for sqlite, and for postgres a merged FS of
  `legacy_postgres/*` + `common/*` excluding `000007_baseline.*` (static
  filename filter). Implement as a flat single-directory `fs.FS` (Open +
  ReadDir) over the embedded tree.
- [ ] `database/migrations.go`: `newMigrator(db *sql.DB, driver string)` — pick
  source via `migrationFS`, database driver via
  `migrate/database/postgres` or `migrate/database/sqlite` (modernc-backed);
  thread `driver` through `Migrate`/`MigrateDown`/`OverrideDirty` and their
  callers; add `zap.String("driver", …)` to the migration logs.
- [ ] `adapters/db/errors.go` (new): `isUniqueViolation` /
  `isForeignKeyViolation` covering pgconn SQLSTATEs and modernc
  `*sqlite.Error` codes (2067, 787, 1555); unit-test the mapping with
  fabricated pgconn errors and real sqlite constraint errors.
- [ ] `adapters/db/lists.go`: `CreateList` and `AddItem` insert explicit
  `uuid.New()` ids; replace the inline `pgconn` check in `AddItem` with
  `isForeignKeyViolation`; drop the `pgconn` import.
- [ ] `adapters/db/lists.go`: rewrite `CopyList` (`lists.go:174-225`) portably —
  it currently relies on the dropped `gen_random_uuid()` default (would break
  Postgres too after 000007) and uses `FOR KEY SHARE`, a `::timestamptz` cast,
  and `interval` arithmetic, none of which exist in SQLite. New shape, still
  one transaction: existence guard `SELECT 1 FROM lists WHERE id = $1` with
  ` FOR KEY SHARE` appended only when driver=postgres (sqlite's single-writer
  connection makes the delete race impossible there); insert the new list with
  an explicit `uuid.New()`; read the source items ordered by
  `created_at, id`; insert each copy in a loop with `uuid.New()` and
  app-computed staggered timestamps `now.Add(time.Duration(i+1) * time.Microsecond)`
  (preserving the existing ordering semantics). Repository constructors
  (`NewListsRepository` et al.) gain the driver argument to support the
  conditional lock clause; update call sites.
- [ ] `adapters/db/users.go`: `Create` inserts explicit `uuid.New()` id and
  `created_at`/`updated_at`; replace the inline check with
  `isUniqueViolation`; drop the `pgconn` import.
- [ ] `adapters/db/members.go`: `Upsert` passes `created_at`/`updated_at`
  parameters; `ON CONFLICT … DO UPDATE SET … updated_at = $4` (no `now()`).
- [ ] Test harness: extend `newTestRepo` (and the members/users equivalents) to
  run each test in two sub-configurations — "sqlite" (always: `t.TempDir()`
  file, `database.Migrate` to latest, fresh DB per test, runs even in
  `-short`) and "postgres" (existing behavior: gated on
  `SPLITKAUF_TEST_DATABASE_DSN`, skipped in `-short`, `TRUNCATE … CASCADE`
  cleanup).
- [ ] New migration test `database/migrations_test.go`: sqlite temp file
  migrates up to latest and down to nil cleanly; the composed postgres FS
  lists versions 1–7 exactly once (unit-level, no postgres needed); with DSN
  set, postgres migrates 6→7 and down again.

**Automated Verification**:
- [ ] `go test ./... -short -race` passes — all adapter and migration tests
  exercise sqlite without any external service.
- [ ] With `SPLITKAUF_TEST_DATABASE_DSN` set (compose postgres up):
  `go test ./adapters/db/... ./database/...` passes on postgres, including the
  6→7 transition.
- [ ] `make check` passes.

### Phase 3: Command wiring, sessions, end-to-end, docs

Dependencies: Phase 2

Wire the driver through serve/migrate/useradd, select the session store, and
document the mode. After this phase a sqlite deployment is fully usable.

**Tasks**:
- [ ] `go get github.com/alexedwards/scs/sqlite3store`.
- [ ] `cmd/serve.go`: extend the `sessionStore` decision — driver=sqlite ⇒
  always durable (`usePersistent=true`); a failed sqlite ping (unwritable or
  locked path) is fatal at startup regardless of auth mode, since a broken
  file path cannot self-heal the way a network DB can (the degraded-serve
  path and the OIDC gate remain postgres-only); `newSessionManager` picks
  `sqlite3store.New(conn)` vs `postgresstore.New(conn)` by driver; update the
  policy comment and the `sessionStore` unit tests.
- [ ] `cmd/serve.go`: startup log line with driver (and path for sqlite).
- [ ] `cmd/migrate.go`, `cmd/useradd.go`: pass the configured driver into
  `NewSQL` and the migrator entry points (mostly done mechanically in earlier
  phases — verify flags `--down`/`--force` still work for both drivers).
- [ ] `docs/architecture.md`: document the second backend — driver/path config
  keys, migration layout (`common/` + `legacy_postgres/` + composition rule),
  session-store selection, single-writer pragma choices; update the
  "Postgres-only stack" wording and the deployment section.
- [ ] `deploy/README.md`: add a "SQLite variant" section for the quadlet
  deployment — single app container, no `splitkauf-db.container`/`.volume`, a
  volume for the `.db` file, `SPLITKAUF_DATABASE_DRIVER`/`_PATH` env; the
  existing postgres quadlet files stay unchanged.
- [ ] `README.md`: add a "SQLite mode" snippet
  (`SPLITKAUF_DATABASE_DRIVER=sqlite SPLITKAUF_DATABASE_PATH=/data/splitkauf.db ./splitkauf migrate && ./splitkauf serve`).
- [ ] New end-to-end test (build on the existing server test patterns in
  `ports/rest`): boot the API against a migrated temp-file sqlite DB in
  dev-auth mode; login, create list, add item, check item, delete list over
  HTTP.

**Automated Verification**:
- [ ] `go test ./... -short -race` passes, including the sqlite end-to-end test.
- [ ] `make check` passes.
- [ ] `SPLITKAUF_DATABASE_DRIVER=sqlite SPLITKAUF_DATABASE_PATH=$(mktemp -d)/e2e.db go run . migrate`
  exits 0, then the same env `go run . useradd -u test …` exits 0.

**Manual Verification**:
- [ ] Run `migrate` + `serve` with sqlite env vars, open the PWA, log in
  (dev-auth), create a list, add/check/delete items, restart the server, and
  confirm the data and the login session survive the restart.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

## References

- `adapters/db/{db,lists,members,users}.go` — current Postgres adapter
- `database/migrations.go`, `database/migrations/` — migrator + legacy SQL
- `config/config.go:131-160`, `config/defaults.go` — database config
- `cmd/serve.go:70-98` — session-store policy
- golang-migrate sqlite driver: `github.com/golang-migrate/migrate/v4/database/sqlite`
- modernc driver: `modernc.org/sqlite`; scs store: `github.com/alexedwards/scs/sqlite3store`
