# AGENTS.md

Guidance for AI coding agents working in this repository. `CLAUDE.md` is a symlink to this file.

---

## Project overview

Splitkauf is a self-hosted, real-time collaborative shopping list for a household. It ships as a **single Go binary** with the React PWA (built by Vite) embedded via `go:embed`; PostgreSQL is the only runtime dependency. Licensed CC0-1.0: every new source file gets an `// SPDX-License-Identifier: CC0-1.0` header where the format allows.

Product scope lives in [`docs/user-stories/`](docs/user-stories) as `US-<area>.<n>-<slug>.md`. Areas: **L** lists, **S** real-time sync, **O** offline/PWA, **A** authentication, **Q** API quality & operations, **B** branding & visual polish. Personas: **Member** (authenticated user of the single-tenant instance) and **Operator** (the self-hoster). Story IDs are stable — reference them in plans and commit messages. Milestone order (M1–M9): [`docs/user-stories/README.md`](docs/user-stories/README.md).

Rules for every change:

- **Spec-first API.** `openapi.yaml` is the source of truth. Change it, then `make generate` to regenerate `ports/rest/v1/api.go` and `client/client.gen.go`. Generated code is committed; never hand-edit it.
- **Uniform errors.** Every API error is RFC 9457 Problem Details (`ports/rest/problem`).
- **Real-time and offline.** Mutations publish reload hints on `events/` so SSE clients refetch; concurrent edits converge last-write-wins on the server's `updated_at`. The PWA keeps working offline — writes queue and replay on reconnect.
- **Migrations.** Schema changes are new numbered pairs (`NNNNNN_name.up.sql` / `.down.sql`) in `database/migrations`. Never edit a committed migration.
- **UX guardrails.** UI stories inherit the §6 checklist of [`docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md`](docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md): touch targets ≥44pt/48dp, 8pt grid, ≥16px body text, WCAG 2.2 AA contrast in light *and* dark mode, no confirmation dialogs or blocking spinners in the core loop (use undo), a tap alternative for every gesture.
- **Out of scope.** Native mobile apps, multi-tenancy/billing, in-app user management, localization beyond German and English.
- **No secrets.** Never commit secrets, tokens, credentials, or fixtures with personal data.
- **No unrequested parallelism.** Don't spawn subagents to parallelise plan steps without the user's explicit consent.

---

## Directory structure

Not exhaustive.

```
.github/     CI workflows: ci, lint, quality, dashboard, pr-title
ports/       Inbound interfaces
  rest/      chi HTTP API, SSE, /docs, middleware, problem responses;
             v1/ = handlers + oapi-codegen output
  web/       Serves the embedded PWA from ports/web/dist (gitignored;
             `make` writes a stub so Go compiles)
adapters/    Outbound I/O; db/ = pgx PostgreSQL repositories
lists/       Pure-Go lists/items domain and Service (Repository port only)
members/     Every authenticated identity (OIDC, local, dev), upserted
users/       Local accounts for password auth (bcrypt), provisioned via CLI
auth/        BFF auth: OIDC, username/password, dev mode, sessions
events/      In-process fan-out of reload hints to SSE streams
client/      Generated typed Go API client
cmd/         Cobra commands: serve, migrate, user add/ls/merge
config/      Viper config: SPLITKAUF_* env → config.yaml → defaults.go
database/    Embedded SQL migrations
telemetry/   Logger and Prometheus metrics
frontend/    React + Vite PWA (TypeScript, React Query, Vitest)
deploy/      Podman Quadlet units and operator README
hack/        Non-shipped tooling: commit-msg validator (hooks/), lint pin
             check (lint/), quality dashboard (dashboard/)
docs/        Human docs: architecture.md, development.md, user stories
  agents/    AI-written output: plans/, research/
```

---

## Build, test and containers

See [`docs/development.md`](docs/development.md); `make help` lists every target.

```sh
make generate        # regenerate server stubs and client from the OpenAPI spec
make dist            # frontend build + go generate + binary with the real PWA
make test-unit       # CI test contract: race, short, shuffle, coverage
make test            # full suite; DB tests need SPLITKAUF_TEST_DATABASE_DSN
make lint            # golangci-lint
make frontend-check  # frontend lint, format, typecheck, tests
make check           # every local gate
docker compose up --build   # PostgreSQL 17 + migration job + app on :8080
```

Frontend dev: `go run . serve` plus `npm run dev --prefix frontend` (proxies `/api` to `localhost:8080`). With no OIDC issuer or password auth configured, the backend runs in dev-auth mode (one hardcoded user).

The `Dockerfile` builds the frontend (Node), then the binary (Go), onto a `gcr.io/distroless/static:nonroot` runtime (`ENTRYPOINT ["/app"] CMD ["serve"]`). When changing it:

- No package manager, shell, or debug tools in the runtime stage — a shell is a security regression.
- Keep the `nonroot` base, `CGO_ENABLED=0`, and `-trimpath`.
- Keep things out of image layers via `.dockerignore`, not `COPY` exclusions.

---

## Go coding practices

Follow [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments); defer to them when in doubt. Always apply:

- **Tooling.** Format with `make fmt` (`gofmt` + `goimports`); pass `make lint` (includes `go vet`). After dependency changes run `make tidy` and commit `go.mod` and `go.sum` together. Imports in three blocks: stdlib, third-party, `github.com/m4schini/splitkauf/...`.
- **Naming.** Short lowercase single-word packages. Exported identifiers have a doc comment starting with their name. Consistent acronyms (`URL`, `ID`, `HTTP`, `SSE`, `DSN`). Single-method interfaces end in `-er` and are defined on the consumer side. Short receiver names, consistent per type.
- **Errors.** Always handle them; `_ = f()` needs a comment. Wrap with `%w` (`fmt.Errorf("load list: %w", err)`). Lowercase messages, no trailing punctuation. Sentinels are exported `Err...` vars checked with `errors.Is`/`errors.As`. Log or return, not both.
- **Context.** `ctx context.Context` is always the first parameter; never store it in a struct.
- **Types & APIs.** `any`, not `interface{}`. Accept interfaces, return concrete types. Export only the public contract. A nil slice is a valid empty slice — don't return `[]T{}`.
- **Tests.** Table-driven with `t.Run(tc.name, ...)`; `t.Helper()` in helpers; `t.Cleanup` over `defer`; `-race` for concurrent code (`make test-unit` does).

---

## Quality gates

Gates are mandatory. Never skip, bypass, or disable them — not to save time, not to land an unrelated change, not when a failure looks flaky or pre-existing. Specifically, never:

- pass `--no-verify`, `-n`, or similar bypass flags to `git` or any other command;
- set `HUSKY=0`, `SKIP=...`, `PRE_COMMIT_ALLOW_NO_CONFIG`, `GIT_HOOKS_PATH=/dev/null`, or any env var that disables a gate;
- uninstall, move, comment out, or edit a hook (`hack/hooks/`, `.git/hooks/`) to make it pass;
- add `//nolint`, `eslint-disable`, `t.Skip`, `it.skip`, `xit`, `--no-tests`, or relax `golangci-lint`/`tsconfig`/CI settings to silence a finding;
- narrow a run (`-run`, `--filter`, single package) and report it as the full gate;
- merge, force-push, or mark a PR ready while checks are failing or running.

If a gate fails, fix the cause. If you can't within the task, stop and report the exact output. A specific suppression (`//nolint` with a reason, a skipped test) is allowed only when a human explicitly asks for it in that place.

---

## Documentation scope

Agents may write documentation-style content (docs, notes, memory, plans, designs, research, analyses) only in `docs/agents/`. Everything else — `README.md`, the rest of `docs/`, `deploy/README.md`, this file, standalone package doc work, any prose for human readers — requires explicit human instruction naming that file or location; "improve the docs" is not enough, so ask. This does not restrict source code changes or code comments written as part of a requested change.

---

## Commits, PRs and attribution

**Trailers.** Never add `Signed-off-by` (only a human certifies the DCO; the human committer reviews the code, ensures licensing compliance, signs off, and takes responsibility), `Co-authored-by`, or `Claude-Session`/session-link trailers. When AI materially shaped a commit, add `Assisted-by: AGENT_NAME:MODEL_VERSION [TOOL1] [TOOL2]` (e.g. `Assisted-by: Claude:claude-opus-4`), without basic tools (git, go, make, editors).

**Messages and PR titles** follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `type(scope): description`. PRs are squash-merged with the title as the commit message, so the same rules apply to both; `hack/hooks/check-commit-msg.sh` enforces them locally and in the pr-title workflow.

- Types: only `feat`, `fix`, `chore`. Flag breaking changes with `!` (`feat(rest)!: ...`) or a `BREAKING CHANGE:` footer.
- Scope optional but recommended; description lowercase, no trailing period.
- Always pass `--title` to `gh pr create`.
- Good: `feat(frontend): per-unit quantity preset chips`, `chore(plans): quick quantity entry (US-L.12), implemented`. Bad: `Add quantity preset chips`, `feat: Add Quantity Preset Chips.`, `docs: update README`.

**Commit granularity.**

- Each research doc or plan change is its own commit containing only that file: `chore(research)` or `chore(plans)`.
- Migrations get their own commit, separate from code.
- When implementing a plan, commit each phase once its verification passes — one commit per phase, never batched, never left uncommitted when starting the next.

**PR comments are for humans only.** Never write or post review comments, issue comments, or approvals — not even when asked, via any tool. If asked for help, summarize your output, let the user review it, and draft a concise comment in the conversation (ending with an `Assisted-by` trailer) for the user to edit and post themselves. PR **descriptions** are fine to write and post (`gh pr create`/`gh pr edit`).
