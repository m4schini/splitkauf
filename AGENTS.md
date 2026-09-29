# AGENTS.md

Guidance for AI coding agents working in this repository. `CLAUDE.md` is a symlink to this file.

---

## Project overview

Splitkauf is a self-hosted, collaborative shopping list for a household: shared lists that everyone edits together, in real time, from their phones. It ships as a **single self-contained Go binary** — the React PWA is built by Vite and embedded with `go:embed`. The only external runtime dependency is PostgreSQL.

The product scope lives in [`docs/user-stories/`](docs/user-stories), one story per file, named `US-<area>.<n>-<slug>.md`. Areas: **L** lists, **S** real-time sync, **O** offline/PWA, **A** authentication, **Q** API quality & operations, **B** branding & visual polish. Personas: **Member** (an authenticated user of the single-tenant instance) and **Operator** (the person self-hosting it). Story IDs are stable — reference them in plans and commit messages. The milestone order (M1–M9) is in [`docs/user-stories/README.md`](docs/user-stories/README.md).

Rules that apply to every change:

- **Spec-first API.** `openapi.yaml` is the source of truth. Change the spec first, then run `make generate` to regenerate the chi server stubs (`ports/rest/v1/api.go`) and the typed Go client (`client/client.gen.go`). Generated code is committed; never hand-edit it.
- **Uniform errors.** Every API error is an RFC 9457 Problem Details response (`ports/rest/problem`).
- **Real-time and offline.** Mutations publish reload hints on `events/` so SSE clients refetch; concurrent edits converge last-write-wins on the server's `updated_at`. The PWA must keep working offline — writes queue and replay on reconnect.
- **UX guardrails.** Every story with a UI surface inherits the do's-and-don'ts checklist (§6) of [`docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md`](docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md): touch targets ≥44pt/48dp, 8pt spacing grid, ≥16px body text, WCAG 2.2 AA contrast in light *and* dark mode, no confirmation dialogs or blocking spinners in the core loop (use undo instead), every gesture has a tap alternative.
- **Out of scope.** Do not write stories or code for native mobile apps, multi-tenancy/billing, in-app user management, or localization beyond German and English.

---

## Licensing

The project is licensed CC0-1.0. Go source files carry an `// SPDX-License-Identifier: CC0-1.0` header; add it to every new source file where the format allows.

---

## Directory structure

```
.
├── .github/      GitHub configuration: the ci, lint, quality, dashboard and
│                 pr-title workflows.
│
├── ports/        Inbound interfaces the application exposes to callers.
│   ├── rest/     HTTP API: chi server, SSE stream, /docs, middleware and RFC
│   │             9457 problem responses. v1/ holds the handlers and the
│   │             oapi-codegen output generated from openapi.yaml.
│   └── web/      Serves the embedded PWA from ports/web/dist (built by the
│                 frontend, gitignored; `make` writes a stub so Go compiles).
├── adapters/     Outbound I/O against external systems. db/ is the pgx
│                 PostgreSQL implementation of the domain repositories.
├── lists/        Pure-Go domain for lists and items: entities, validation and
│                 the Service, depending on a Repository port only.
├── members/      Membership domain: every authenticated identity (OIDC subject,
│                 local account or dev user) upserted into a members table.
├── users/        Local-account domain for password auth: usernames and bcrypt
│                 hashes provisioned by the operator via the CLI.
├── auth/         BFF auth: OIDC, username/password and dev modes, sessions.
├── events/       In-process fan-out of reload hints to connected SSE streams.
├── client/       Typed Go API client generated from openapi.yaml.
│
├── cmd/          Cobra commands: serve, migrate, user add/ls/merge. main.go
│                 only calls into cmd.
├── config/       Viper-based configuration loading (SPLITKAUF_* env vars →
│                 config.yaml → defaults.go) and validation.
├── database/     Embedded SQL migrations (database/migrations).
├── telemetry/    Logger and Prometheus metrics.
│
├── frontend/     React + Vite PWA (TypeScript, React Query, Vitest). Builds
│                 into ports/web/dist.
├── deploy/       Deployment files: Podman Quadlet units and the operator
│                 README.
├── hack/         Developer and CI tooling that is not part of the shipped
│                 binary: the Conventional Commits validator (hooks/), the
│                 golangci-lint pin check (lint/) and the quality dashboard
│                 generator (dashboard/).
└── docs/         Documentation for humans: architecture.md, development.md
    │             and the user stories.
    └── agents/   Documentation and similar written output produced by AI
                  agents and assistants: plans/ and research/.
```

This list is not exhaustive and will grow with the project.

---

## Building and testing

The development harness is described in [`docs/development.md`](docs/development.md); `make help` lists every target. The ones agents use most:

```sh
make generate        # regenerate server stubs and client from the OpenAPI spec
make dist            # frontend build + go generate + single binary with the real PWA embedded
make test-unit       # CI test contract: race, short, shuffle, coverage
make test            # full suite; DB tests need SPLITKAUF_TEST_DATABASE_DSN
make lint            # golangci-lint
make frontend-check  # frontend lint, format, typecheck, tests
make check           # every local gate
```

For frontend work, run the backend with `go run . serve` and the Vite dev server with `npm run dev --prefix frontend` — it proxies `/api` to `localhost:8080`. With no OIDC issuer and no password auth configured the backend runs in dev-auth mode (a single hardcoded user).

Schema changes are new numbered migration pairs (`NNNNNN_name.up.sql` / `.down.sql`) in `database/migrations`. Never edit a migration that has already been committed.

---

## Go coding practices

All Go code follows [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments). The subset below is what AI assistants must apply to every change; when in doubt, defer to the linked documents.

### Formatting & tooling

- Code MUST be formatted with `make fmt` (`golangci-lint fmt`: `gofmt` + `goimports`).
- Code MUST pass `make lint` (`golangci-lint`, which includes `go vet`).
- Run `make tidy` after dependency changes; commit `go.mod` and `go.sum` together.

### Naming

- Package names: short, lowercase, single word, no underscores or mixedCaps.
- Exported identifiers have a doc comment starting with the identifier's name.
- Acronyms keep consistent case: `URL`, `ID`, `HTTP`, `SSE`, `DSN` (not `Url`, `Id`, `Http`).
- Interface names: single-method interfaces end in `-er`. Define interfaces on the consumer side.
- Receiver names: short (1–2 letters), consistent across all methods of a type.

### Error handling

- Always handle errors; never `_ = someFunc()` without a comment explaining why.
- Wrap errors with `%w`: `fmt.Errorf("load list: %w", err)`.
- Error strings: lowercase, no trailing punctuation.
- Sentinel errors are exported `Err...` variables, compared with `errors.Is`/`errors.As`.
- Don't log and return the same error — pick one.

### Context

- `context.Context` is always the first parameter, named `ctx`. Never store it in a struct.

### Variables, types & APIs

- Use `any`, not `interface{}`, in new code.
- Accept interfaces, return concrete types.
- Don't export identifiers that aren't part of the package's public contract.
- A nil slice is a valid empty slice — don't return `[]T{}` to "be safe".

### Testing

- File names: `*_test.go`. Prefer table-driven tests with `t.Run(tc.name, ...)`.
- Use `t.Helper()` in helpers; `t.Cleanup(...)` instead of `defer` for teardown.
- Run with `-race` for concurrent code (`make test-unit` does).
- Do not commit fixtures with real credentials, tokens, or personal data.

### Imports

- Group imports in three blocks: standard library, third-party, local (`github.com/m4schini/splitkauf/...`). `goimports` enforces this.

---

## Quality gates

Quality gates are mandatory. Agents MUST NOT skip, bypass, or disable them — not to save time, not to get an unrelated change committed, and not even when a gate looks flaky or the failure looks pre-existing.

Concretely, agents MUST NOT:

- Pass `--no-verify`, `-n`, `--no-gpg-sign`-style bypass flags to `git commit`, `git push`, or any other command in order to skip hooks.
- Set `HUSKY=0`, `SKIP=...`, `PRE_COMMIT_ALLOW_NO_CONFIG`, `GIT_HOOKS_PATH=/dev/null`, or any other environment variable that disables a hook or gate.
- Uninstall, rename, move, comment out, or edit a hook (`hack/hooks/`, `.git/hooks/`) to make it stop failing.
- Add `//nolint`, `eslint-disable`, `t.Skip`, `it.skip`, `xit`, `--no-tests`, or a relaxed `golangci-lint`/`tsconfig`/CI setting to silence a finding instead of fixing it.
- Narrow a test or lint run (`-run`, `--filter`, single-package invocations) and then report it as if the full gate had passed.
- Merge, force-push, or mark a PR ready while its checks are failing or still running.

If a gate fails, fix the underlying problem. If it cannot be fixed within the task, stop and report the failure to the user with the exact output — do not work around it. Suppressing a specific finding (a `//nolint` with a reason, a skipped test) is allowed only when a human explicitly asks for that suppression in that place.

---

## Container builds

The image is defined in `Dockerfile`: a Node stage builds the frontend, a Go stage builds the binary with the frontend embedded, and the runtime stage is `gcr.io/distroless/static:nonroot` with `ENTRYPOINT ["/app"] CMD ["serve"]`.

```sh
docker compose up --build   # PostgreSQL 17 + one-shot migration job + app on :8080
```

Rules for agents changing the `Dockerfile`:

- Do not add a package manager, shell, or debugging tools to the runtime stage. A shell in the final image is a security regression.
- Do not remove the `nonroot` base, `CGO_ENABLED=0`, or `-trimpath`.
- Anything that must not reach an image layer belongs in `.dockerignore`, not in a `COPY` exclusion.

---

## Documentation scope

By default, AI assistants may only write documentation-style content inside the `docs/agents/` directory. This covers documentation, notes, memory files, plans, designs, research reports, analyses, and anything similar.

Documentation anywhere else in the repository — including `README.md`, `docs/` outside `docs/agents/` (among them `docs/architecture.md`, `docs/development.md` and `docs/user-stories/`), `deploy/README.md`, this file, package-level doc comments written as standalone documentation work, and any other prose intended for human readers — is off-limits unless a human user explicitly instructs the agent to change that specific file or location. A general request such as "improve the docs" is not sufficient authorization for files outside `docs/agents/`; ask which files to touch.

This rule governs documentation only. It does not restrict ordinary source-code changes, which follow the usual review process, and it does not apply to code comments written as part of a code change the user asked for.

---

## Attribution

### For AI-assisted commits

AI assistants MUST NOT add `Signed-off-by` tags — only a human can certify the DCO. The human committer is responsible for:

- Reviewing all AI-generated code.
- Ensuring licensing compliance.
- Adding their own `Signed-off-by`.
- Taking full responsibility for the contribution.

AI assistants MUST NOT add `Co-authored-by` tags.

AI assistants MUST NOT add `Claude-Session` (or similar session-link) trailers.

When AI assistance materially shaped a commit, add an attribution trailer:

```
Assisted-by: AGENT_NAME:MODEL_VERSION [TOOL1] [TOOL2]
```

Examples:

```
Assisted-by: Cursor:claude-sonnet-4.5
Assisted-by: Copilot:gpt-5
Assisted-by: Claude:claude-opus-4
```

Do not list basic tools (git, go, make, editors).

Follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) standard for commit messages and PR titles. Restrict to the core types — `feat`, `fix`, `chore` — plus a `!` after the type/scope or a `BREAKING CHANGE:` footer to flag breaking changes. Do not use other types (`docs`, `refactor`, `style`, `perf`, `test`, `build`, `ci`, etc.). The same validator (`hack/hooks/check-commit-msg.sh`) runs as the local commit-msg hook and in the pr-title workflow.

Commits that add or update research documents use `chore(research)`. Commits that add or update implementation plans use `chore(plans)`.

Each addition, change, or deletion of a research document or plan MUST be its own commit, containing only that research document or plan — do not mix it with code changes or with other research/plan files.

When implementing a plan, commit after each completed phase — one commit per phase, made once the phase's verification passes. Do not batch multiple phases into a single commit, and do not leave a finished phase uncommitted while starting the next one.

Database migrations MUST be committed in their own commit, separate from application/code changes.

### Pull-request titles

This repository squash-merges pull requests, and GitHub uses the **PR title** as the squash commit message. The PR title must therefore follow the same Conventional Commits rules as a commit.

**Format:** `type(scope): description`

- Allowed types: `feat`, `fix`, `chore` — no others.
- Scope is optional but recommended (e.g. `feat(frontend): ...`, `fix(rest): ...`).
- For breaking changes, append `!` after the type/scope (`feat!: ...` or `feat(rest)!: ...`) or add a `BREAKING CHANGE:` footer in the PR body.
- Description starts lowercase, no trailing period.

**Good:**

- `feat(frontend): per-unit quantity preset chips`
- `fix(rest): make openapi.json conversion independent of the json encoder`
- `chore(plans): quick quantity entry (US-L.12), implemented`

**Bad:**

- `Add quantity preset chips` (no type prefix)
- `feat: Add Quantity Preset Chips.` (capitalized, trailing period)
- `docs: update README` (type `docs` not allowed)

When creating PRs via `gh pr create`, always pass `--title` in this format.

### For AI-assisted PR comments

PR comments are for humans only. AI assistants MUST NOT write or post PR comments (review comments, issue comments, or approvals) under any circumstances — not even when explicitly asked to "post" or "submit" one, and not via `gh`, the GitHub API, or any other tool.

If a user wants help drafting a PR comment, the agent should:

- Summarize its own output (e.g. a code review or analysis).
- Let the user review that summary.
- Draft a concise, human-audience comment as text in the conversation, for the user to copy, edit as needed, and post **themselves**.
- Include an `Assisted-by` trailer (same format as commits, see above) at the end of the draft, so the human-posted comment discloses AI assistance.

This restriction applies to PR **comments** only. AI-assisted PR **descriptions** are allowed — an agent may write and post/update the PR description body itself (e.g. via `gh pr create`/`gh pr edit`).

The user is always the one who writes and posts the comment. The agent's role ends at producing a draft.

---

## Things to Avoid

- ❌ Adding `Signed-off-by` on behalf of a human.
- ❌ Adding `Co-authored-by` tags.
- ❌ Adding `Claude-Session` (or similar session-link) trailers.
- ❌ Writing or posting PR comments on behalf of a human.
- ❌ Committing secrets, tokens, or credentials.
- ❌ `interface{}` in new code (use `any`).
- ❌ Spawning subagents to parallelise plan steps without the user's explicit consent.
- ❌ Hand-editing generated code instead of changing `openapi.yaml` and regenerating.
- ❌ Editing a committed migration instead of adding a new one.
- ❌ Writing documentation outside `docs/agents/` without explicit instruction.
- ❌ Skipping or bypassing quality gates and commit hooks — `--no-verify`, `HUSKY=0`, disabling a hook, or silencing a finding instead of fixing it.
