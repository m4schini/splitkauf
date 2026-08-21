---
date: 2026-08-21T20:33:10+00:00
git_commit: 68861a5920008e66099752e5f822974a781ebb78
branch: main
topic: "Quality gates overhaul (testing, linting, security) based on goapp-template"
tags: [plan, quality, lint, ci, pre-commit, security, makefile]
status: ready
---

# PLAN: Quality Gates Overhaul

Port the testing/linting/security posture of `../goapp-template/` to splitkauf, keeping the Makefile (no Taskfile). The guiding principle from the template: **one definition of every check** — the Makefile and `.golangci.yml` define *what* runs; git hooks and CI only decide *when*. This plan installs the gates; it does **not** fix the ~550 pre-existing lint findings — every currently-firing linter is disabled with a dated `deferred` reason, to be re-enabled by follow-up plans.

## Acceptance Criteria

- The Go-side gates of `make check` pass locally with only `go` and `golangci-lint` on PATH (trivy optional, skipped gracefully; gitleaks runs via pre-commit's own hook env); `frontend-check` additionally needs node/npm, as today.
- `.golangci.yml` is v2 schema with `default: all`; every `disable:` entry carries a reason comment in the form `# reason; owner @m4schini; review YYYY-MM`; deferred entries name the finding count.
- `golangci-lint config verify` passes (`make lint-config`).
- `make fmt-check` and `make tidy-check` are non-mutating (no more "run fmt then git diff").
- Tests run with `-race -shuffle=on -covermode=atomic`; coverage profile produced and reported (tracked, never gated).
- Pre-push hook stage runs build, full lint, tidy-check, and short tests via make targets.
- CI: pre-commit hooks job, quality gates job (fmt/lint/tidy/security with trivy required), coverage summary + artifact, weekly scheduled vulnerability scan, PR title validation sharing the commit-msg validator script.
- CI runs the DB integration tests (`adapters/db`, gated on `SPLITKAUF_TEST_DATABASE_DSN`) against a postgres service container — today they never run anywhere.
- All GitHub Actions pinned by commit SHA with version comment; every job has `timeout-minutes`; Go version comes from `go-version-file: go.mod`; go.mod says `go 1.26.0`.
- gitleaks stays quiet on `docs/agents/` via a path-only allowlist in `.gitleaks.toml`.
- A drift-check hook fails when the golangci-lint version pinned in `.pre-commit-config.yaml` differs from the one CI installs.

## Technical Key Decisions and Tradeoffs

1. **`default: all` posture, fixes deferred:** adopt the template's "everything on, disabling needs a written reason" config, but disable all ~39 currently-firing linters with `deferred` reason comments and a review date.
   - Why: user decision — install the gates now, clean the code later; a curated enable-list rots.
   - Impact: `make lint` is green immediately; follow-up plans re-enable linters batch by batch. Formatter findings (gofumpt, 3 files) cannot be deferred (fmt hooks rewrite files), so `golangci-lint fmt` runs once in Phase 1.
2. **golangci-lint from PATH, no bootstrap scripts:** Makefile invokes `golangci-lint` (overridable `GOLANGCI_LINT ?=`); CI installs the pinned version with `go install .../v2/cmd/golangci-lint@v2.12.2`.
   - Why: user decision — assume the tool is installed locally; keep hack/ small.
   - Impact: the version pin lives in `.pre-commit-config.yaml` (rev) and the quality workflow (go install version); `hack/lint/check-golangci-pin.sh` keeps them in sync. A contributor's PATH version may drift from the pin — the pre-commit/pre-push hooks and CI use the pinned one, so drift is caught before merge.
3. **Plain `go test`, no gotestsum:** add `-shuffle=on` and `-covermode=atomic`; keep the standard test runner.
   - Why: user decision — no extra tooling for cosmetic output.
   - Impact: order-dependent tests start failing under shuffle; any such failure is a real bug and gets fixed in Phase 2 (the only sanctioned test-code change).
4. **trivy CI-required, local-optional:** `make security` runs govulncheck always, trivy only if installed (or hard-fails when `REQUIRE_TRIVY=1`, which CI sets).
   - Why: trivy is not installed on the dev machine; CI must not silently skip it.
   - Impact: `REQUIRE_TRIVY` variable in the Makefile; `aquasecurity/setup-trivy` action in CI.
5. **Official golangci-lint pre-commit hooks + pre-push stage:** replace the local `go run` gofumpt/golangci hooks with the upstream `golangci/golangci-lint` hooks; add a pre-push stage running make targets.
   - Why: upstream hooks pin tool+hook together; pre-push makes CI boring.
   - Impact: the upstream `golangci-lint` (run) hook typechecks the packages (`golangci-lint-fmt` and `config-verify` do not), so a local `embed-stub` hook must create the gitignored `ports/web/dist/index.html` before it; pre-commit runs hooks in config-file order across repo blocks, so ordering is guaranteed. First hook install compiles golangci-lint from source (minutes, once).
6. **Shared conventional-commit validator:** one script (`hack/hooks/check-commit-msg.sh`) backs both the commit-msg hook and a new `pr-title.yml` workflow.
   - Why: a local commit and a squash-merge title must never be judged differently.
   - Impact: replaces the inline bash grep in `.pre-commit-config.yaml`.
7. **CI keeps the backend/frontend/docker job split; no hermetic job:** ci.yml keeps build/test (backend), frontend, docker; lint.yml is replaced by quality.yml (hooks job, gates job, weekly vuln job).
   - Why: parallelism and the node toolchain argue for split jobs; the docker build already exercises a pinned-ish environment, so a hermetic job adds maintenance for little signal.
   - Impact: the frontend job switches from inline npm commands to `make frontend-check` — this adds `prettier --check` to CI (previously a local-only gate). A new `test-full` job (postgres service container) runs `make test` so the non-short DB integration tests finally execute somewhere.
8. **go.mod bumps to `go 1.26.0`:** CI reads `go-version-file: go.mod`.
   - Why: avoids the silent downgrade from today's hardcoded `1.26.6` to go.mod's `1.25.0`.
   - Impact: one-line go.mod change, committed with the CI phase.

## Current State

```
Definition of checks            When they run
──────────────────────          ─────────────────────────────────────────
Makefile                        pre-commit (commit): hygiene, gitleaks,
  fmt        gofumpt -w via       go run gofumpt/golangci v1 (local hooks),
             go run @v0.7.0       prettier/oxlint, inline commit-msg grep
  fmt-check  fmt + git diff     no pre-push stage
  lint       go run golangci    CI ci.yml: backend (generate/drift/build/
             v1.62.2              test-unit), frontend (inline npm, no
  lint-vet   go vet               format-check), docker
  test-unit  -race -short       CI lint.yml: fmt-check, lint, tidy-check,
             -coverprofile        security (govulncheck only)
  tidy-check tidy + git diff    actions pinned by tag, no timeouts,
  security   govulncheck only     go hardcoded "1.26.6" (go.mod: 1.25.0)
  check      aggregate          coverage.out generated, never looked at
.golangci.yml: v1 schema, 5 linters enabled
No trivy, no .gitleaks.toml, no weekly scan, no PR-title check
```

Facts that shape the work:

- `ports/web/web.go:17` has `//go:embed all:dist`; `ports/web/dist/` is gitignored (`.gitignore:16`). Anything that compiles the packages needs the stub (`make ports/web/dist/index.html`).
- Generated files `ports/rest/v1/gen.go` / `client/client.gen.go` are committed and carry the standard `Code generated by ... DO NOT EDIT.` header — golangci v2's `exclusions.generated: lax` covers them; the v1 `exclude-files` patterns die with the old config.
- Probe run of the template's v2 config (v2.12.2) against splitkauf: **547 issues from 39 linters** — and per-linter counts are capped at 50 by default, so `varnamelen`, `exhaustruct`, `paralleltest`, `depguard` are ≥50 each. Full list with counts in Phase 1.
- `renovate.json` extends `config:recommended`, which already manages GitHub Actions SHA pins; the pre-commit manager must be enabled explicitly.
- Dev machine: go 1.26.6, pre-commit installed; golangci-lint v1.62.2 (needs a v2 install), trivy/gitleaks not installed.

## Desired End State

```
Definition of checks            When they run
──────────────────────          ─────────────────────────────────────────
Makefile (tools from PATH)      pre-commit (commit): hygiene(+symlinks,
  fmt         golangci fmt        line-endings,toml), gitleaks(+.toml),
  fmt-check   fmt --diff          embed-stub, upstream golangci hooks
  lint        golangci run        (fmt / --whole-files / config-verify),
  lint-config config verify       pin-drift, prettier/oxlint
  test        -race -shuffle    commit-msg: hack/hooks/check-commit-msg.sh
  test-short  -short -shuffle   pre-push: make build / lint / tidy-check /
  test-unit   -race -short        test-short
              -shuffle -atomic  CI ci.yml: backend (+coverage summary &
              -coverprofile       artifact), test-full (postgres service,
  coverage    cover -func         make test), frontend (make targets, now
                                  incl. format-check), docker
  tidy-check  verify + tidy     CI quality.yml: hooks job (pre-commit
              -diff               --all-files), gates job (fmt-check,
  security    govulncheck +       lint-config, lint, tidy-check, security
              trivy (optional     REQUIRE_TRIVY=1), weekly vuln job
              local, REQUIRE_   CI pr-title.yml: same validator script
              TRIVY=1 in CI)    All actions SHA-pinned, timeouts, go from
  check       aggregate           go-version-file
.golangci.yml: v2, default: all, deferred disables w/ reasons+dates
trivy.yaml, .gitleaks.toml (docs/agents/ path allowlist)
```

## Abstractions and Code Reuse

Files copied/adapted from `../goapp-template/` (adapt, do not symlink):

- `.golangci.yml` — template structure + splitkauf module prefix + deferred-disable block
- `.gitleaks.toml` — near-verbatim (docs/agents/ allowlist applies here too)
- `trivy.yaml` — adapted skip-dirs
- `hack/lint/check-golangci-pin.sh` — rewritten to compare `.pre-commit-config.yaml` rev vs quality.yml `go install` version
- `.pre-commit-config.yaml` pre-push block, `quality.yml` hooks/weekly jobs, `pr-title.yml` — adapted to make targets

Changed files:

- `Makefile` — targets `fmt`, `fmt-check`, `lint`, `lint-fix`, `lint-config` (new), `test`, `test-short` (new), `test-unit`, `coverage`, `tidy`, `tidy-check`, `security`, `deps`, `check`; delete `lint-vet` (govet runs inside golangci-lint)
- `.golangci.yml` — full rewrite to v2
- `.pre-commit-config.yaml` — full rewrite
- `hack/hooks/check-commit-msg.sh` — new shared validator
- `hack/lint/check-golangci-pin.sh` — new drift check
- `.github/workflows/ci.yml` — hardening, coverage summary, frontend via make
- `.github/workflows/quality.yml` — new (replaces `lint.yml`, which is deleted)
- `.github/workflows/pr-title.yml` — new
- `go.mod` — `go 1.26.0`
- `renovate.json` — enable pre-commit manager
- `README.md` — harness section rewrite

## Logging & Observability

No runtime logging changes. CI observability: coverage table in `$GITHUB_STEP_SUMMARY` plus `coverage.out` artifact on every backend run; weekly vuln job surfaces new advisories against unchanged code.

## Implementation

### Phase 1: golangci-lint v2 migration

Dependencies: None

Replace the v1 config and Makefile lint/fmt plumbing with golangci-lint v2.12.2 from PATH. One-time `golangci-lint fmt` (the only sanctioned code diff: 3 files with gofumpt findings, plus whatever goimports regroups).

**Tasks**:
- [x] Install golangci-lint v2.12.2 locally (out of repo scope, e.g. `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`); verify `golangci-lint version` reports 2.12.2.
- [x] Rewrite `.golangci.yml` to v2 schema, modeled on the template:
  - Header comment stating the posture and the rules for changing the file (copy the template's, adjust owner to `@m4schini`).
  - `version: "2"`, `linters.default: all`.
  - Template's permanent disables with their reasons: `noinlineerr`, `wsl` (superseded by wsl_v5), `gomodguard` (superseded by gomodguard_v2).
  - Deferred-disable block, one entry per firing linter, each as `# deferred: fires ~N× on existing code (probe-time count, capped at 50 where noted); re-enable via follow-up cleanup plan; owner @m4schini; review 2027-02.` Counts are approximate: the probe ran with the template's `cmd/[^/]+/main\.go` exclusions, which don't match splitkauf's layout, so e.g. `gochecknoglobals` will differ under the adapted exclusions. Probe linters and counts: `contextcheck` 1, `cyclop` 19, `depguard` 50+ (also needs an allowlist config before re-enabling), `embeddedstructfieldcheck` 3, `err113` 29, `errcheck` 26, `errchkjson` 1, `exhaustruct` 50+, `funcorder` 5, `funlen` 2, `gochecknoglobals` 33, `gochecknoinits` 3, `gocognit` 1, `goconst` 40, `gocyclo` 1, `godoclint` 1, `godot` 2, `gosec` 4, `gosmopolitan` 2, `inamedparam` 1, `interfacebloat` 2, `intrange` 2, `ireturn` 1, `lll` 33, `mnd` 15, `modernize` 2, `nlreturn` 6, `noctx` 13, `nonamedreturns` 2, `paralleltest` 50+, `revive` 5, `tagliatelle` 6, `testpackage` 9, `unconvert` 3, `unparam` 1, `unused` 1, `varnamelen` 50+, `wrapcheck` 26, `wsl_v5` 43.
  - Settings kept from the template (they gate future code even while related linters are deferred): `nolintlint` (require-explanation, require-specific, allow-unused false), `govet.enable: [nilness]`, `forbidigo` fmt.Print ban, `sloglint.forbidden-keys` (harmless with zap; guards a future slog migration), `revive.file-length-limit: 400` (inert while revive is deferred, documents the target).
  - `exclusions.generated: lax` (replaces the v1 `exclude-files` gen.go patterns).
  - Template's path exclusions, adapted: `_test\.go` → funlen/dupl/gocognit; forbidigo + gochecknoglobals lifted for `^main\.go$` and `^cmd/` (splitkauf's cobra commands live in `cmd/*.go`, main.go at repo root — not the template's `cmd/*/main.go` layout).
  - `formatters.enable: [gofumpt, goimports]`, `gofumpt.extra-rules: true`, `goimports.local-prefixes: [github.com/m4schini/splitkauf]`. Set `formatters.exclusions.generated: lax` explicitly: committed `ports/rest/v1/gen.go` / `client/client.gen.go` must stay byte-identical to oapi-codegen output or the CI generated-drift check breaks (the old gofumpt hook excluded them for the same reason).
- [x] Makefile: replace tool pins and lint/fmt targets:
  - Delete `GOFUMPT_PACKAGE` and `GOLANGCI_LINT_PACKAGE`; add `GOLANGCI_LINT ?= golangci-lint`.
  - `fmt: generate` → `$(GOLANGCI_LINT) fmt`.
  - `fmt-check: generate` → `$(GOLANGCI_LINT) fmt --diff` (non-mutating; drop the git-diff dance).
  - `lint: generate` → `$(GOLANGCI_LINT) run` (the `generate` prerequisite provides the embed stub + generated code golangci needs to compile packages).
  - `lint-fix: generate` → `$(GOLANGCI_LINT) run --fix`.
  - New `lint-config:` → `$(GOLANGCI_LINT) config verify`.
  - Delete `lint-vet` (govet runs inside golangci-lint); remove it from `check` and from `help`.
  - `deps`: drop the gofumpt/golangci installs; keep `go install tool` and govulncheck; echo a hint that golangci-lint v2.12.2 is expected on PATH.
  - Update `help` text for all added/removed targets (also in later phases).
- [x] Run `make fmt` once and commit the resulting formatting diffs together with this phase.
- [x] Update `check` aggregate to `fmt-check lint-config lint tidy-check test-unit security frontend-check` (order; Phase 2 refines the test targets).

**Automated Verification**:
- [x] `make lint-config` passes.
- [x] `make fmt-check` passes and is non-mutating: `git diff | sha256sum` identical before and after running it (the tree holds this phase's uncommitted edits, so a plain `git diff --exit-code` cannot be used).
- [x] `make lint` passes.
- [x] `grep -c 'deferred:' .golangci.yml` ≥ 39 (every firing linter documented).
- [x] `make lint-vet` fails with "No rule to make target" (target gone).

### Phase 2: Test and module targets

Dependencies: Phase 1 (Makefile layout)

Bring the test flags to template parity and make tidy-check non-mutating.

**Tasks**:
- [x] Makefile test targets:
  - `test: generate` → `$(GO) test -race -shuffle=on ./...` (full suite, no profile; the `adapters/db` integration tests self-skip unless `SPLITKAUF_TEST_DATABASE_DSN` is set — Phase 5 wires this target into a postgres-backed CI job so they actually run somewhere).
  - New `test-short: generate` → `$(GO) test -short -shuffle=on ./...` (pre-push budget).
  - `test-unit: generate` → `$(GO) test -race -short -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...` (stays the CI contract).
  - `coverage:` → fail with a hint if `coverage.out` is missing, else `$(GO) tool cover -func=coverage.out` (report-only, like the template; no longer re-runs tests).
  - Delete the old `coverage` test invocation and `GOTESTFLAGS`/`RACE_ENABLED` knobs (superseded by explicit flags per target).
- [x] Fix any test that fails under `-shuffle=on` (order-dependence is a bug; expected blast radius: small or zero). (None failed — no changes needed.)
- [x] Module targets:
  - `tidy:` → plain `$(GO) mod tidy` (drop the `-compat` extraction; go 1.26 toolchain).
  - `tidy-check:` → `$(GO) mod verify` then `$(GO) mod tidy -diff` (non-mutating; drop the git-diff dance).
- [x] Update `help` text.

**Automated Verification**:
- [x] `make test-unit` passes and produces `coverage.out`.
- [x] `make coverage` prints the function coverage table.
- [x] `make test-short` passes.
- [x] `make test` passes (DB integration tests report skipped without `SPLITKAUF_TEST_DATABASE_DSN`).
- [x] `make tidy-check` passes and is non-mutating: `git diff | sha256sum` identical before and after running it.

### Phase 3: Security scanning

Dependencies: Phase 1 (Makefile layout)

govulncheck stays the precise, blocking scanner; trivy is the broad one (CI-required); gitleaks gets its config.

**Tasks**:
- [x] Add `trivy.yaml`, adapted from the template: keep the header comment explaining the govulncheck/trivy split and the report-only license posture; `scan.skip-dirs: [frontend/node_modules, ports/web/dist]`; `license.full: true` with the copyleft `forbidden` list.
- [x] Add `.gitleaks.toml`, near-verbatim from the template: `[extend] useDefault = true`, path-only allowlist for `^docs/agents/` with the template's rationale comment (owner `@m4schini`, review 2027-02). The pre-commit gitleaks hook picks it up automatically from the repo root.
- [x] Makefile `security` target: keep `$(GO) run $(GOVULNCHECK_PACKAGE) ./...` (blocking), then the template's trivy block verbatim in make syntax: if `trivy` on PATH → `trivy fs --scanners vuln,secret --exit-code 1 --severity HIGH,CRITICAL .` (blocking) and `trivy fs --scanners license --exit-code 0 .` (report); elif `$(REQUIRE_TRIVY)` non-empty → hard error; else warning + skip. Add `REQUIRE_TRIVY ?=` variable.
- [x] Probe trivy's secret scanner against docs/agents/: (deferred to first CI run — trivy not installed locally; if it fires, add docs/agents to trivy.yaml skip-dirs) trivy does **not** read `.gitleaks.toml`, and the research/plan docs quote example credentials — run `trivy fs --scanners secret docs/agents` (any machine with trivy, or defer to the first CI run). If it fires, add `docs/agents` to `trivy.yaml` `scan.skip-dirs` with the same rationale comment as the gitleaks allowlist.

**Automated Verification**:
- [x] `make security` passes locally (trivy skip path: prints the warning, exit 0).
- [x] `REQUIRE_TRIVY=1 make security` fails locally with the "trivy is required" error (proves the hard-fail path without trivy installed).
- [x] Allowlist actually exercised (the gitleaks hook scans only the staged diff, so a clean-tree `--all-files` run proves nothing): stage a scratch file `docs/agents/research/tmp-allowlist-probe.md` containing a fake high-entropy token (e.g. an obviously fake `ghp_` string), `pre-commit run gitleaks` passes; move the same content to a staged file outside docs/agents/, the hook fails; delete both probes.

### Phase 4: Git hooks

Dependencies: Phases 1–3 (hooks call the new make targets)

Rewrite `.pre-commit-config.yaml`: upstream golangci hooks, pre-push stage, shared commit-msg validator, pin-drift check.

**Tasks**:
- [ ] New `hack/hooks/check-commit-msg.sh` (executable): takes one argument — path to a file containing the message. Strips comment lines and everything below a scissors line, then validates the subject against `^(feat|fix|chore)(\([a-z0-9./-]+\))?!?: .+` (the repo's AGENTS.md contract, incl. `!` breaking marker). Exempts subjects git generates itself, as the template's validator does: `Merge ...`, `Revert "..."`, `fixup! ...`, `squash! ...` (git runs the commit-msg hook for merge/revert too). On failure prints the expected format to stderr and exits 1. Used by both the commit-msg hook and pr-title.yml (Phase 5 — PR titles hit the same regex; the git-generated exemptions are harmless there).
- [ ] New `hack/lint/check-golangci-pin.sh` (executable): extracts the `rev:` of the `golangci/golangci-lint` repo from `.pre-commit-config.yaml` and the `golangci-lint/v2/cmd/golangci-lint@vX.Y.Z` version from the workflow file (`WORKFLOW_FILE` env override, default `.github/workflows/quality.yml` — the override exists so the script is testable against a fixture); exits 1 with both values printed if they differ. Until Phase 5 lands quality.yml, a missing default workflow file is a warning + exit 0, so Phases 4 and 5 stay independently committable; Phase 5 hardens this to an error.
- [ ] Rewrite `.pre-commit-config.yaml`:
  - Header comment: install command, the three stages and their budgets, escape hatches (`SKIP=`, `--no-verify`), the note that CI's hooks job re-runs everything (adapted from the template).
  - `minimum_pre_commit_version: "4.0.0"`, `default_install_hook_types: [pre-commit, commit-msg, pre-push]`, `default_stages: [pre-commit]`.
  - Hygiene repo (`pre-commit-hooks` v6.0.0): keep existing hooks and their splitkauf args (`--maxkb=1024` appicon comment, `--unsafe` yaml, tsconfig JSONC exclude); add `check-toml`, `mixed-line-ending --fix=lf`, `check-symlinks`, `destroyed-symlinks`.
  - gitleaks repo unchanged (rev v8.30.1).
  - New local `embed-stub` hook *before* the golangci repo: `make ports/web/dist/index.html`, `language: system`, `always_run: true`, `pass_filenames: false` — the upstream golangci hooks compile the packages and need the gitignored embed stub.
  - `golangci/golangci-lint` repo at rev `v2.12.2` (this rev is the authoritative pin): `golangci-lint-fmt`, `golangci-lint` with `args: [--whole-files]`, `golangci-lint-config-verify`. Delete the old local gofumpt/golangci `go run` hooks.
  - Local `golangci-lint-pin-drift` hook: `hack/lint/check-golangci-pin.sh`, `files: ^(\.pre-commit-config\.yaml|\.github/workflows/quality\.yml)$`, `pass_filenames: false`.
  - Keep the frontend prettier/oxlint local hooks unchanged.
  - commit-msg hook: entry `hack/hooks/check-commit-msg.sh`, `stages: [commit-msg]` (replaces the inline grep).
  - pre-push local hooks, each `language: system`, `pass_filenames: false`, `stages: [pre-push]`: `push-build` → `make build`, `push-lint` → `make lint`, `push-tidy-check` → `make tidy-check`, `push-test-short` → `make test-short`.
- [ ] Reinstall hooks: `pre-commit install --install-hooks` (now installs all three stages).

**Automated Verification**:
- [ ] `pre-commit run --all-files` passes.
- [ ] `pre-commit run --hook-stage pre-push --all-files` passes.
- [ ] `printf 'feat(auth): add thing\n' > /tmp/m && hack/hooks/check-commit-msg.sh /tmp/m` exits 0; `docs: nope` exits 1; `Merge branch 'x'`, `Revert "feat: y"`, and `fixup! feat: z` subjects all exit 0.
- [ ] `hack/lint/check-golangci-pin.sh` exits 0 (warning branch while quality.yml absent).
- [ ] Fixture check of the drift logic: write a minimal fixture to the scratchpad containing `golangci-lint@v2.12.2` → `WORKFLOW_FILE=<fixture> hack/lint/check-golangci-pin.sh` exits 0; with `@v2.12.1` in the fixture it exits 1.

### Phase 5: CI workflows, go.mod, docs

Dependencies: Phases 1–4 (workflows call the make targets and hook suite)

Replace lint.yml with quality.yml, harden ci.yml, add pr-title.yml, bump go.mod, update docs.

**Tasks**:
- [ ] `go.mod`: change `go 1.25.0` → `go 1.26.0`; run `make tidy-check` to confirm no churn. (Own commit per AGENTS.md is not required — not a migration — but keep it a distinct task inside the phase commit.)
- [ ] New `.github/workflows/quality.yml` (delete `.github/workflows/lint.yml` in the same commit):
  - `on: pull_request, push (main), schedule (cron "17 6 * * 1"), workflow_dispatch`; `permissions: contents: read`; concurrency group with `cancel-in-progress: true`; every job `timeout-minutes: 20`.
  - Job `hooks` (pull_request/push only): checkout; setup-go with `go-version-file: go.mod` + `cache-dependency-path: "**/go.sum"`; setup-node (frontend hooks need `npm exec`); `npm ci` in frontend/ (prettier/oxlint hooks resolve from there); cache `~/.cache/pre-commit` keyed on the config hash; `pipx install pre-commit`; `pre-commit run --all-files --show-diff-on-failure`.
  - Job `gates` (pull_request/push only): checkout; setup-go (go-version-file); `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2` (the greppable pin the drift check reads) followed by `echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"` (on runners' PATH today, explicit is robust); `aquasecurity/setup-trivy`; then `make generate`, `git diff --exit-code` (generated-code drift — moves here from ci.yml; lives in exactly one place), `make fmt-check`, `make lint-config`, `make lint`, `make tidy-check`, `make security REQUIRE_TRIVY=1`.
  - Job `vuln-weekly` (schedule/workflow_dispatch only): checkout, setup-go, setup-trivy, `make generate`, `make security REQUIRE_TRIVY=1`.
- [ ] New `.github/workflows/pr-title.yml`: `on: pull_request: types [opened, edited, synchronize, reopened]`; single job (timeout 5): checkout, write `${{ github.event.pull_request.title }}` to a temp file via env var (env: TITLE, `printf '%s\n' "$TITLE" > /tmp/title` — env indirection, never inline template into the script, injection risk), run `hack/hooks/check-commit-msg.sh /tmp/title`.
- [ ] Rework `.github/workflows/ci.yml`:
  - Backend job: setup-go switches to `go-version-file: go.mod`; the generated-drift check (`git diff --exit-code`) moves to the quality `gates` job, but `make generate` stays as the first step because build/test need the embed stub and regeneration is idempotent. Steps: `make generate` → `make build` → `make test-unit`; then coverage summary (`go tool cover -func=coverage.out` into `$GITHUB_STEP_SUMMARY`, `if: always()` guarded by file existence) and `actions/upload-artifact` of `coverage.out`.
  - New `test-full` job: postgres `services:` container (postgres:17 pinned by digest, health-checked, `POSTGRES_PASSWORD`/`POSTGRES_DB` set); checkout, setup-go (go-version-file); `make generate`; apply migrations with `go run . migrate` configured for the service DB via `SPLITKAUF_DATABASE_*` env vars (viper env binding — confirm exact names from `config/config.go` during implementation); then `make test` with `SPLITKAUF_TEST_DATABASE_DSN` pointing at the same DB. This is the first place the `adapters/db` integration tests run.
  - Frontend job: replace inline npm run steps with `make frontend-deps`, `make frontend-check`, `make frontend-build` (adds prettier `format-check` to CI). If `format-check` is red on the current tree, run `npx prettier --write .` in frontend/ and include the diff (formatting fixes are sanctioned, like gofumpt).
  - Docker job unchanged in behavior.
  - Add `timeout-minutes` to all jobs.
- [ ] Pin every action in all three workflows (and quality/pr-title) by commit SHA with `# vX.Y.Z` comment. Known-good SHAs from the template: `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`, `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0`, `actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0`, `actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1`, `aquasecurity/setup-trivy@81e514348e19b6112ce2a7e3ecbafe19c1e1f567 # v0.3.1`. For the rest (setup-node, docker/*), resolve the SHA of the currently used major's latest release via `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` at implementation time.
- [ ] Harden `hack/lint/check-golangci-pin.sh`: quality.yml now exists, so a missing workflow file becomes an error instead of a warning (delete the warning branch from Phase 4).
- [ ] `renovate.json`: add `"pre-commit": {"enabled": true}` so hook revs stay current (actions SHA pins are already covered by `config:recommended`).
- [ ] `README.md`: rewrite the development-harness section — three hook stages and budgets, `make check` composition, golangci-lint v2.12.2 expected on PATH, trivy optional locally / required in CI, coverage tracked-not-gated, DB integration tests and how to run them locally (compose postgres + `SPLITKAUF_TEST_DATABASE_DSN`), PR titles validated by the same script as commit messages. Delete the stale `gofumpt -w` fmt-check explanation.

**Automated Verification**:
- [ ] `actionlint` passes on all workflow files (install via `go run github.com/rhysd/actionlint/cmd/actionlint@latest` if absent).
- [ ] `grep -rE 'uses: [^@]+@(v[0-9]|main|master)' .github/workflows/` finds nothing (every action SHA-pinned).
- [ ] `grep -rL 'timeout-minutes' .github/workflows/*.yml` finds nothing.
- [ ] `.github/workflows/lint.yml` no longer exists.
- [ ] `hack/lint/check-golangci-pin.sh` exits 0; with quality.yml version edited to `v2.12.1` it exits 1 (then revert).
- [ ] `make check` passes end to end.
- [ ] `pre-commit run --all-files` passes.

**Manual Verification**:
- [ ] After push: all workflows (CI incl. the new test-full job, quality, pr-title on a test PR, weekly via `workflow_dispatch`) run green on GitHub; coverage table visible in the backend job's step summary; test-full job log shows the `adapters/db` tests ran instead of skipping.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

- Probe details (2026-08-21): template config v2.12.2 against splitkauf produced 547 reported issues across 39 linters; per-linter cap 50 means varnamelen/exhaustruct/paralleltest/depguard are undercounted. Probe config lives only in the session scratchpad; the real `.golangci.yml` is written fresh in Phase 1.
- Phase 1 (2026-08-21): `errorlint` fired 4× at gate-install time but was absent from the probe list (probe ran with the template's path exclusions); deferred like the rest — 40 deferred entries total. `make fmt` reformatted 22 Go files (import regrouping under the local-prefix rule plus gofumpt); generated `gen.go`/`client.gen.go` untouched, confirming `formatters.exclusions.generated: lax` works.

## References

- `../goapp-template/` — source template: `.golangci.yml`, `Taskfile.yml` (translated to Makefile), `.pre-commit-config.yaml`, `trivy.yaml`, `.gitleaks.toml`, `.github/workflows/quality.yml`, `pr-title.yml`, `hack/lint/`, `hack/hooks/`
- `Makefile`, `.golangci.yml`, `.pre-commit-config.yaml`, `.github/workflows/{ci,lint}.yml` — current splitkauf state
- `ports/web/web.go:17` — `//go:embed all:dist` constraint driving the embed-stub hook
- https://golangci-lint.run/docs/configuration/ — v2 config schema
- https://www.conventionalcommits.org/en/v1.0.0/ — commit/PR-title contract (restricted to feat|fix|chore per AGENTS.md)
