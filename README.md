# splitkauf

## Development harness

The Makefile and `.golangci.yml` define *what* runs; git hooks and CI only
decide *when*. Quality gates fire at edit time (a Claude Code hook), commit
time, push time, and in CI — all calling the same make targets.

### One-time setup

`golangci-lint` v2.12.2 is expected on PATH (the authoritative pin is the
`golangci/golangci-lint` `rev:` in `.pre-commit-config.yaml`; CI installs the
same version, and `hack/lint/check-golangci-pin.sh` fails the commit if the
two drift):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
pre-commit install --install-hooks
```

`.pre-commit-config.yaml` installs three hook stages, three budgets:

- **pre-commit** — file hygiene, secret scanning (gitleaks with
  `.gitleaks.toml`; `docs/agents/` is path-allowlisted), diff-scoped
  `golangci-lint` fmt/lint/config-verify, `prettier`/`oxlint` on staged
  frontend files. Seconds.
- **commit-msg** — `hack/hooks/check-commit-msg.sh` enforces the
  `feat|fix|chore` conventional-commit subject. The **same script** validates
  PR titles in CI (`.github/workflows/pr-title.yml`), so a local commit and a
  squash-merge title are judged identically. Milliseconds.
- **pre-push** — `make build`, `make lint`, `make tidy-check`,
  `make test-short`. Tens of seconds; this is the gate that makes CI boring.

### Running the gates manually

```sh
pre-commit run --all-files   # everything pre-commit checks, across the whole tree
make check                   # fmt-check, lint-config, lint, tidy-check, test-unit, security, frontend-check
```

`make fmt-check` and `make tidy-check` are non-mutating (`golangci-lint fmt
--diff`, `go mod tidy -diff`) — safe on a dirty tree.

`make security` runs `govulncheck` (blocking, call-graph-aware) and, when
`trivy` is installed, a broad trivy vuln+secret scan (blocking at
HIGH/CRITICAL) plus a report-only licence scan (`trivy.yaml`). Locally trivy
is optional and skipped with a warning; CI sets `REQUIRE_TRIVY=1` to make a
missing trivy a hard failure. govulncheck may report vulnerabilities that stem
from the *host's installed Go toolchain* (stdlib CVEs fixed in a later Go
patch release) rather than from repository code — check whether the reported
module is `stdlib` before treating a `security` failure as a real issue.

### Tests and coverage

- `make test-unit` — race, short, shuffled, atomic coverage profile; the CI
  contract. Coverage is **tracked, never gated**: CI publishes the
  `go tool cover -func` table in the job summary and uploads `coverage.out`;
  there is no threshold. `make coverage` prints the table locally.
- `make test-short` — the pre-push budget.
- `make test` — the full suite. The `adapters/db` integration tests self-skip
  unless `SPLITKAUF_TEST_DATABASE_DSN` is set; CI runs them in the `test-full`
  job against a postgres service container. Locally:

  ```sh
  docker run -d --name splitkauf-pg -p 5432:5432 \
    -e POSTGRES_USER=splitkauf -e POSTGRES_PASSWORD=splitkauf -e POSTGRES_DB=splitkauf \
    postgres:17
  go run . migrate
  SPLITKAUF_TEST_DATABASE_DSN='postgres://splitkauf:splitkauf@localhost:5432/splitkauf?sslmode=disable' make test
  ```

### Quality dashboard

The "Quality Dashboard" issue (label `dashboard`) is a single, living
summary of the project's quality posture: Go coverage (total and per
package), lint debt per deferred linter, Go test pass/skip/fail counts, and
the latest security scan status — plus a trend table, one row per commit,
capped at 20 rows. It is rebuilt automatically after every CI or quality run
on `main` completes, and after the weekly scheduled vulnerability scan.

The bot owns the issue body: it is fully overwritten on every update, so any
manual edit is discarded on the next rebuild (only the trend rows between the
`<!-- trend-start -->`/`<!-- trend-end -->` markers are read back and carried
forward). History beyond 20 rows is lost by design — the repository's git
history still has everything.

To force a rebuild without waiting for the next CI run, use
`workflow_dispatch` on the `dashboard` workflow. To run the rebuild locally
(useful for debugging), with `gh` logged in:

```sh
REPO=owner/repo GH_TOKEN="$(gh auth token)" hack/dashboard/update.sh
```

The rendering/parsing logic lives in the Go tool `hack/dashboard/`
(`go run ./hack/dashboard render|strip-deferred`, unit-tested); the workflow
orchestration (finding runs, downloading artifacts, upserting the issue) is
`hack/dashboard/update.sh`, called by `.github/workflows/dashboard.yml`.

### Claude Code edit-time formatting

`.claude/settings.json` registers a `PostToolUse` hook that runs
`hack/format-file.sh <file>` after every `Edit`/`Write` tool call. The script
dispatches by extension:

- `*.go` → `gofumpt -w`
- `frontend/**/*.{ts,tsx,css,json,md}` → `prettier --write`, plus
  `oxlint --fix` for `.ts`/`.tsx` (exits non-zero, with a message on
  stderr, if oxlint still reports errors after the fix pass)

It can also be run by hand on any file in the repo:

```sh
hack/format-file.sh path/to/file.go
```

### Frontend scripts

Run from `frontend/` (or via `npm run <script> --prefix frontend`):

- `npm run lint` — `oxlint`
- `npm run format` — `prettier --write .`
- `npm run format-check` — `prettier --check .`
- `npm run typecheck` — `tsc -b --noEmit`
- `npm run test` — `vitest run`
