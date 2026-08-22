---
date: 2026-08-21T21:31:40+00:00
git_commit: 708485565ea447d97f12c6240928d5994acf3468
branch: main
topic: "Quality dashboard as an auto-updated GitHub issue"
tags: [plan, quality, ci, dashboard, coverage, lint]
status: ready
---

# PLAN: Quality Dashboard Issue

Publish a single GitHub issue (label `dashboard`, title "Quality Dashboard") that shows the project's quality posture — Go test coverage, lint debt, test counts, last security scan — and is rebuilt automatically after every CI run on `main` and after the weekly vulnerability scan. This makes the numbers the quality-gates overhaul started producing (coverage tracked-never-gated, 40 deferred linters with ~547 findings) visible in one living place instead of buried in per-run step summaries and stale config comments.

## Acceptance Criteria

- An open issue labeled `dashboard` exists after the first post-merge CI cycle; the workflow creates it (and the label) when missing and fully overwrites its body on every update.
- The body contains: a summary table (Go coverage total, lint debt total + deferred-linter count, Go test pass/skip counts, security scan status with timestamp and run link), a collapsible per-package coverage table, a collapsible per-linter debt table, and a trend table (≤ 20 rows, one row per commit, carried forward across rebuilds via HTML comment markers).
- Updates fire only for `main`: `workflow_run` completion of CI and quality (push events) plus the weekly scheduled quality run. A weekly run for an unchanged commit refreshes the security row but adds no trend row (trend rows are keyed by the commit of the CI run that produced the coverage data).
- Only `dashboard.yml` holds `issues: write` and `actions: read`; `ci.yml` and `quality.yml` keep `contents: read`.
- Lint debt is a live count: golangci-lint runs with the deferred disables stripped from `.golangci.yml` (permanent disables kept), `--issues-exit-code 0`, counted per linter from JSON output.
- Local behavior unchanged: `make test-unit` output is identical by default (`GOTESTFLAGS` empty), `make lint` untouched.
- The new Go tool passes the existing gates (`make lint`, `make test-unit`); its rendering, parsing, and trend-carryover logic is unit-tested.
- All new/changed workflow steps use already-pinned actions or `gh`/`go` from the runner; no new action dependencies. New jobs carry `timeout-minutes`.
- No CVE details in the issue — the security row is status/timestamp/link only (the issue is public; the run log has the details).

## Technical Key Decisions and Tradeoffs

1. **Metrics: Go coverage (total + per-package), lint debt per deferred linter, Go test counts, security scan status:** user decision (option "core three + security row").
   - Why: everything derivable from data CI already produces or one extra non-blocking lint pass; frontend coverage would need a new dev dependency (`@vitest/coverage-v8`), skipped.
   - Impact: no frontend changes at all; security appears as one status row, not a findings list.
2. **Trigger: `workflow_run` on CI + quality completion, `branches: [main]`:** updates only from main pushes and the weekly schedule (scheduled runs report `head_branch: main`).
   - Why: fork PRs get a read-only token and PR numbers would thrash the dashboard with data never merged.
   - Impact: one push to main fires two dashboard runs (CI completes, quality completes); the rebuild is idempotent and concurrency-grouped, so the double run is harmless.
3. **Producer-artifact architecture:** ci.yml and quality.yml upload small artifacts (`coverage` exists already; new `test-report`, `lint-debt`); `dashboard.yml` aggregates the latest artifacts, queries the security step outcome via the jobs API, and rebuilds the whole body.
   - Why: producers already have toolchains and generated code; the dashboard job stays a fast aggregator; full-body rebuild kills partial-merge races.
   - Impact: `dashboard.yml` needs `actions: read` to download artifacts across workflows; artifact download is `gh run download`, not a new pinned action.
4. **Issue discovery by label, not number:** search open issues labeled `dashboard`, create label + issue when absent.
   - Why: no hardcoded state; self-heals after accidental close or delete.
   - Impact: the label is reserved for the bot — a human applying `dashboard` to another issue could misroute updates; the fixed title in the create step and "first listed open labeled issue" selection (`gh issue list` default order, newest first) keep this deterministic.
5. **Trend history lives in the issue body between markers:** rows between `<!-- trend-start -->` / `<!-- trend-end -->` are parsed from the previous body, a new row is prepended when its commit is new, trimmed to 20.
   - Why: no external storage; the issue is the database.
   - Impact: a human editing the body inside the markers can corrupt a row — the parser skips malformed rows instead of failing; history beyond 20 rows is lost by design (git history of the repo still has everything).
6. **Implementation as a Go tool, `hack/dashboard/` (package main, same module):** subcommands `strip-deferred` and `render`; the workflow-side orchestration (find runs, download artifacts, security status, issue upsert) is one bash script `hack/dashboard/update.sh` that the workflow calls.
   - Why: rendering and parsing are string-heavy and deserve unit tests; `gopkg.in/yaml.v3` is already a direct dependency, and its Node API preserves comments so deferred entries are identified by their `# deferred:` head comments, not by a hardcoded linter list. The bash half keeps `gh` orchestration out of Go (no GitHub API client dependency) and is runnable locally with a personal `gh` login.
   - Impact: the tool is linted by the existing `default: all` config (deferred disables apply to it too, so only the enabled linters gate it); `go run ./hack/dashboard` needs setup-go in dashboard.yml.
7. **Per-package coverage computed from the raw profile, not `go tool cover -func`:** the tool parses `coverage.out` (statement blocks) and aggregates covered/total statements per package.
   - Why: `cover -func` needs the source tree of the exact commit; parsing the profile needs nothing, so the dashboard job never checks out the producing commit.
   - Impact: totals are statement-weighted (same method `cover -func` uses for `total:`); package names derive from file paths by trimming the `github.com/m4schini/splitkauf/` module prefix.
8. **Test counts via `GOTESTFLAGS` passthrough:** `test-unit` gains `$(GOTESTFLAGS)`; CI sets `-json` and tees stdout to `test-report.json`.
   - Why: the local contract stays byte-identical (variable empty by default); `go test -json` is the only reliable way to count tests.
   - Impact: the teed file contains non-JSON lines (make's command echo); the parser skips any line that is not a JSON object.
9. **Debt config written to a gitignored repo-root file `.golangci.debt.yml`:** not to `$RUNNER_TEMP`.
   - Why: golangci-lint v2 resolves relative paths in `run.relative-path-mode: cfg` mode relative to the config file's directory — a config outside the repo root would break the path-scoped exclusions.
   - Impact: one `.gitignore` entry; the file is transient CI state.

## Current State

```
Data produced today                     Where it goes
─────────────────────────────           ─────────────────────────────────────
ci.yml backend:  make test-unit         coverage table in $GITHUB_STEP_SUMMARY
                 → coverage.out         (per-run, buried), `coverage` artifact
                                        (downloaded by nobody)
ci.yml test-full: make test (postgres)  pass/fail only
quality.yml gates: make lint            pass/fail only (deferred linters
                                        invisible — they are disabled)
quality.yml gates + vuln-weekly:        pass/fail only, weekly run unseen
                 make security            unless it fails
.golangci.yml:   40 deferred disables   per-linter counts as comments, frozen
                                          at the 2026-08 probe, rotting
Frontend:        vitest run             no coverage tooling installed
```

No dashboard exists. Relevant plumbing facts:

- `Makefile:76-77` — `test-unit` is the CI coverage contract (`-race -short -shuffle=on -covermode=atomic -coverprofile=coverage.out`).
- `.github/workflows/ci.yml:64-70` — `coverage` artifact uploaded with `if: always()`.
- `.github/workflows/quality.yml:106-109` — gates job installs pinned golangci-lint v2.12.2 and puts it on PATH; `make generate` has run by the lint step.
- `.golangci.yml` — deferred entries each carry a `# deferred: …` head comment (permanent disables carry different comments); `gopkg.in/yaml.v3 v3.0.1` is a direct dependency of the module.
- Workflow conventions (quality-gates overhaul): actions SHA-pinned with version comment, every job `timeout-minutes`, least-privilege permissions, checks defined in Makefile/scripts — workflows only decide *when*.

## Desired End State

```
ci.yml backend ──── coverage artifact (exists) ──────┐
ci.yml backend ──── test-report artifact (new) ──────┤
quality.yml gates ─ lint-debt artifact (new) ────────┼─▶ dashboard.yml (workflow_run
quality.yml runs ── security step status (jobs API) ─┘    on CI + quality, main only)
                                                          └─ hack/dashboard/update.sh
                                                             ├─ download latest artifacts
                                                             ├─ query security status
                                                             ├─ go run ./hack/dashboard render
                                                             └─ gh issue upsert (label
                                                                `dashboard`, full overwrite)
```

Issue body layout (agreed mockup):

```markdown
# 📊 Quality Dashboard

_Auto-updated on each main push + weekly scan. Do not edit — the bot overwrites._
Last update: 2026-08-21 14:32 UTC · commit `abc1234` · [run](…)

## Summary
| Metric              | Value                                |
|---------------------|--------------------------------------|
| Go coverage (total) | 61.4%                                |
| Lint debt           | 547 findings across 40 deferred linters |
| Go tests            | 214 pass · 3 skip                    |
| Security scan       | ✅ pass · 2026-08-21 · [run](…)      |

## Coverage by package
<details><summary>18 packages</summary>

| Package     | Coverage |
|-------------|----------|
| adapters/db | 71.2%    |
| …           | …        |
</details>

## Lint debt by linter
<details><summary>40 linters · 547 findings</summary>

| Linter      | Findings |
|-------------|----------|
| varnamelen  | 92       |
| …           | …        |
</details>

## Trend
<!-- trend-start -->
| Date       | Commit    | Coverage | Lint debt | Tests |
|------------|-----------|----------|-----------|-------|
| 2026-08-21 | `abc1234` | 61.4%    | 547       | 214   |
<!-- trend-end -->
```

Missing inputs (first runs, failed producer jobs) render as `n/a` in the affected rows/sections; the update never hard-fails on absent data.

## Abstractions and Code Reuse

Reused as-is: the `coverage` artifact (ci.yml), the pinned golangci-lint install + `make generate` in the gates job, existing action SHA pins (checkout, setup-go, upload-artifact), `gh` and `jq` preinstalled on ubuntu runners, `gopkg.in/yaml.v3`.

- `hack/dashboard/`
  - `main.go` — CLI entry, subcommand dispatch (`strip-deferred`, `render`)
  - `stripdeferred.go` — `stripDeferred(in []byte) ([]byte, error)`: yaml.v3 Node walk of `linters.disable`, drop sequence entries whose head comment contains `deferred:`, re-marshal
  - `coverage.go` — `parseProfile(r io.Reader) (Coverage, error)`: statement-weighted totals per package and overall from the raw cover profile
  - `testreport.go` — `parseTestJSON(r io.Reader) (TestCounts, error)`: count `Action` pass/skip/fail where `Test != ""`, skip non-JSON lines
  - `lintdebt.go` — `parseLintJSON(r io.Reader) (LintDebt, error)`: per-`FromLinter` counts from golangci-lint JSON output
  - `trend.go` — `extractTrend(prevBody string) []TrendRow` (marker-delimited, malformed rows skipped), `mergeTrend(rows, newRow) []TrendRow` (prepend if commit new, trim 20)
  - `render.go` — `render(inputs) string`: full body; `n/a` for nil inputs
  - `*_test.go` — table-driven tests with small fixtures for each parser, strip-deferred, trend carryover/dedup, and a golden full-body render
  - `update.sh` — orchestration: find latest artifact-bearing runs, download, security status, previous body fetch, render, issue upsert
- `Makefile` — `test-unit` gains `$(GOTESTFLAGS)`; new variable `GOTESTFLAGS ?=`
- `.github/workflows/ci.yml` — backend job tees `-json` test output, uploads `test-report`
- `.github/workflows/quality.yml` — gates job: debt-count step + `lint-debt` artifact upload
- `.github/workflows/dashboard.yml` — new
- `.gitignore` — `.golangci.debt.yml`, `lint-debt.json`, `test-report.json`
- `README.md` — dashboard section

## Logging & Observability

No runtime logging changes. The dashboard itself is the observability deliverable. `update.sh` echoes each stage (chosen run IDs, artifact presence, issue number) so a wrong dashboard is diagnosable from the run log; a missing artifact logs a warning and renders `n/a` instead of failing.

## Implementation

### Phase 1: `hack/dashboard` Go tool

Dependencies: None

The pure-logic half: parsers, trend carryover, body rendering, deferred-strip. Everything unit-tested; nothing touches CI yet.

**Tasks**:
- [x] Create `hack/dashboard/main.go`: `package main`, subcommands `strip-deferred` (stdin or `.golangci.yml` default → stdout) and `render` with flags `--coverage`, `--tests`, `--lint-debt`, `--security`, `--meta`, `--prev-body` (each a file path, each optional — absent file ⇒ `n/a` rendering) writing the body to stdout. `--meta` is JSON `{commit, run_url, updated}` assembled by the caller; `--security` is JSON `{status, completed_at, run_url}`.
- [x] `stripdeferred.go`: decode `.golangci.yml` into `yaml.Node`, locate `linters.disable`, remove sequence items whose `HeadComment` contains `deferred:`, re-encode (2-space indent). Fail loudly if the `linters.disable` path is missing (config restructured ⇒ the tool must be updated, not silently no-op).
- [x] `coverage.go`: parse cover-profile lines (`file.go:l.c,l.c numStmts hitCount`), aggregate covered/total statements per package (trim `github.com/m4schini/splitkauf/` prefix, use the file's directory; files at the module root render as `.`) and overall; ignore the `mode:` line.
- [x] `testreport.go`: line-wise scan; unmarshal only lines starting with `{`; count final `pass`/`skip`/`fail` actions where `Test != ""`.
- [x] `lintdebt.go`: unmarshal `{"Issues":[{"FromLinter":…}]}`, count per linter, sort descending by count then name.
- [x] `trend.go`: extract rows between `<!-- trend-start -->`/`<!-- trend-end -->` from the previous body (regexp per row: date, backticked short commit, coverage, debt, tests; malformed rows dropped); merge: skip when the new row's commit already present, else prepend and trim to 20.
- [x] `render.go`: assemble the full body per the mockup (header + warning line, summary table, two `<details>` sections, trend block with markers). Security status maps `success` → `✅ pass`, `failure` → `❌ fail`, anything else/absent → `n/a`.
- [x] Tests (`*_test.go`): fixture-driven parser tests (including a test-report fixture with interleaved non-JSON make output), strip-deferred test asserting permanent disables (`noinlineerr`, `wsl`, `gomodguard`) survive and a `# deferred:`-commented entry is removed, trend extract/merge/dedup/trim tests, and a golden-body render test (fixtures + golden file under `hack/dashboard/testdata/`).

**Automated Verification**:
- [x] `go test ./hack/dashboard/...` passes.
- [x] `make lint` passes (tool code satisfies the enabled linters).
- [x] `go run ./hack/dashboard strip-deferred > .golangci.debt.yml && golangci-lint config verify -c .golangci.debt.yml` passes, and `grep -c '# deferred:' .golangci.debt.yml` is 0 while `grep -c 'noinlineerr' .golangci.debt.yml` is 1 (then delete the file — it is gitignored in Phase 2). *(Deviation: the plan's original `grep -c 'deferred'` matches the repo's own doc-comment prose too — see Implementation Notes.)*
- [x] `go run ./hack/dashboard render --meta <fixture>` (only meta provided) exits 0 and prints a body containing `n/a` for coverage, lint debt, tests, and security.

### Phase 2: Producer wiring

Dependencies: Phase 1 (gates step runs `strip-deferred`)

CI starts producing the two new artifacts. Nothing consumes them yet; ci.yml/quality.yml behavior is otherwise unchanged.

**Tasks**:
- [x] `Makefile`: add `GOTESTFLAGS ?=` (near `GO ?=`) and append `$(GOTESTFLAGS)` to the `test-unit` recipe before `./...`. Help text unchanged (internal knob).
- [x] `.gitignore`: add `.golangci.debt.yml`, `lint-debt.json`, `test-report.json`.
- [x] `.github/workflows/ci.yml` backend job: change the test step to `make test-unit GOTESTFLAGS=-json | tee test-report.json` and set `shell: bash` on the step — the default run shell is `bash -e {0}` *without* pipefail, so a failing `go test` would be masked by `tee`; an explicit `shell: bash` runs `bash --noprofile --norc -eo pipefail {0}`, which propagates the failure. Add an `actions/upload-artifact` step (same SHA pin as the coverage upload, `if: always()`): name `test-report`, path `test-report.json`, `if-no-files-found: warn`.
- [x] `.github/workflows/quality.yml` gates job, after the `Lint` step: a `Count lint debt` step running `go run ./hack/dashboard strip-deferred > .golangci.debt.yml` then `golangci-lint run -c .golangci.debt.yml --issues-exit-code 0 --show-stats=false --output.json.path=lint-debt.json`; then an upload step (pinned SHA, `if: always()`): name `lint-debt`, path `lint-debt.json`, `if-no-files-found: warn`. Comment on the step: report-only pass counting the deferred-linter findings for the dashboard; never gates.
- [x] Note for the step summary in ci.yml: the existing coverage summary step stays untouched (it and the dashboard serve different audiences).

**Automated Verification**:
- [x] `actionlint` passes on all workflow files.
- [x] `make test-unit` output is byte-compatible with before (no `-json`): run and confirm no JSON lines; then `make test-unit GOTESTFLAGS=-json | tee /tmp/tr.json` and `go run ./hack/dashboard render --tests /tmp/tr.json --meta <fixture>` prints non-zero pass count.
- [x] Local debt pass: `go run ./hack/dashboard strip-deferred > .golangci.debt.yml && golangci-lint run -c .golangci.debt.yml --issues-exit-code 0 --show-stats=false --output.json.path=lint-debt.json` exits 0. *(Deviation: `jq '.Issues | length' lint-debt.json` reports 0, not > 400 — see Implementation Notes; the ">400" sanity check predates the 2026-08 deferred-linter cleanup this same plan documents.)*
- [x] `git check-ignore .golangci.debt.yml lint-debt.json test-report.json` lists all three.
- [x] `grep -rE 'uses: [^@]+@(v[0-9]|main|master)' .github/workflows/` finds nothing.

### Phase 3: dashboard.yml + update script + docs

Dependencies: Phases 1–2

The consumer: workflow, orchestration script, README. After this phase the issue goes live.

**Tasks**:
- [ ] New `hack/dashboard/update.sh` (executable, `set -euo pipefail`), env: `GH_TOKEN`, `REPO` (owner/name), `TRIGGER_HEAD_SHA` (optional, logging only). Stages, each echoed:
  1. For each (workflow file, artifact name) pair — (`ci.yml`, `coverage`), (`ci.yml`, `test-report`), (`quality.yml`, `lint-debt`) — list the last ~10 completed runs on main (`gh api "repos/$REPO/actions/workflows/<wf>/runs?branch=main&status=completed&per_page=10"`), pick the newest run whose artifact list contains the name, `gh run download <id> -n <name> -D <dir>`. Missing everywhere ⇒ warn, leave file absent. Record the head_sha and html_url of the run that supplied `coverage` — that commit keys the trend row and fills `--meta`.
  2. Security status: from the same quality.yml run list, find the newest run whose jobs (`gh api repos/$REPO/actions/runs/<id>/jobs`) contain a step named `Security scan` with conclusion `success` or `failure`; write `{status, completed_at, run_url}` to `security.json`. None found ⇒ skip (renders `n/a`).
  3. Previous body: `gh issue list --repo "$REPO" --label dashboard --state open --json number --jq '.[0].number'`; if an issue exists, save its body (`gh issue view --json body`) to `prev-body.md`.
  4. Assemble `meta.json` (`commit` = coverage-run head_sha or `n/a`, `run_url`, `updated` = `date -u`), run `go run ./hack/dashboard render …` to `body.md`.
  5. Upsert: if no issue, `gh label create dashboard --repo "$REPO" --force --description "Auto-updated quality dashboard" --color 0E8A16`, then `gh issue create --repo "$REPO" --title "Quality Dashboard" --label dashboard --body "initializing"` and capture the number; finally `gh issue edit <n> --repo "$REPO" --body-file body.md`.
- [ ] New `.github/workflows/dashboard.yml`:
  - Header comment in the house style (what it does, why workflow_run, why the token split).
  - `on: workflow_run: workflows: [CI, quality], types: [completed], branches: [main]` plus `workflow_dispatch` (manual rebuild).
  - `permissions: { contents: read, issues: write, actions: read }`; `concurrency: { group: dashboard, cancel-in-progress: true }`.
  - One job `update` (`timeout-minutes: 10`), gated `if: github.event_name == 'workflow_dispatch' || github.event.workflow_run.head_branch == 'main'` (belt-and-braces with the trigger filter): checkout (pinned SHA), setup-go with `go-version-file: go.mod` + `cache-dependency-path: "**/go.sum"` (pinned SHA), then `run: hack/dashboard/update.sh` with `env: GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}`, `REPO: ${{ github.repository }}`, `TRIGGER_HEAD_SHA: ${{ github.event.workflow_run.head_sha }}`.
- [ ] `README.md`: add a "Quality dashboard" paragraph to the development-harness section — what the issue shows, when it updates, that the body is bot-owned (edits are overwritten), that trend history is capped at 20 rows, and how to force a rebuild (`workflow_dispatch` on the dashboard workflow) or run `hack/dashboard/update.sh` locally with `gh` logged in.

**Automated Verification**:
- [ ] `actionlint` passes on all workflow files.
- [ ] `shellcheck hack/dashboard/update.sh` passes.
- [ ] `grep -rE 'uses: [^@]+@(v[0-9]|main|master)' .github/workflows/` finds nothing; `grep -rL 'timeout-minutes' .github/workflows/*.yml` finds nothing.
- [ ] `pre-commit run --all-files` passes.
- [ ] `make check` passes end to end.

**Manual Verification**:
- [ ] After merging to main: dashboard workflow runs green (twice — once per completed producer workflow); the issue "Quality Dashboard" exists with label `dashboard`, all four summary rows populated (no `n/a`), per-package and per-linter tables filled, one trend row for the merge commit.
- [ ] Trigger `workflow_dispatch` on the dashboard workflow: run green, `Last update` timestamp changes, still exactly one trend row for the same commit (dedup works).
- [ ] Trigger `workflow_dispatch` on the quality workflow (simulates the weekly scan): security row refreshes, no new trend row.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

### Phase 1

Used a `fable`-model agent as an independent advisor to review the code before writing tests. It found several real gaps against this plan's own spec; fixes applied:

- **Missing producer files now render `n/a` instead of failing.** The task list already documented "absent file ⇒ `n/a` rendering", but the first pass treated a missing path the same as a read error. `loadRenderInputs` now checks `os.Stat` per flag and treats `fs.ErrNotExist` as absent; a genuine read/parse error still fails loudly. `hack/dashboard/update.sh` (Phase 3) can therefore always pass all six flags unconditionally.
- **`.golangci.yml` currently has zero `# deferred:` entries** (the file's own header says the 2026-08 cleanup already fixed all 40). The plan's verification command `grep -c 'deferred' .golangci.debt.yml` counts that header prose too (2 matches), not just disable-entry markers — updated the check above to `grep -c '# deferred:'`, which is 0 as intended. `stripDeferred` itself was also tightened to match a comment *line* starting with `deferred:` (after trimming `#`/whitespace) rather than a bare substring, so a permanent disable's prose can never accidentally match (covered by `TestStripDeferredDoesNotMatchProseSubstring`).
- **Commit handling made self-consistent inside the tool**: `render.go` now truncates `Meta.Commit` to 7 chars once (`shortCommit`), used for both the header and the trend-row key, so header/trend never disagree regardless of whether a caller passes a full or short SHA. `loadMeta` also normalises a literal `"n/a"` commit to `""`, so Phase 3's update.sh is free to write either an empty string or `"n/a"` for "unknown" without producing a bogus non-hex trend row.
- **`trendDate` validates the sliced date** against `^\d{4}-\d{2}-\d{2}$` instead of just checking length, so an unpinned `updated` format (e.g. bare `date -u`'s default `Fri Aug 22 ...`) degrades to `n/a` instead of silently producing a row `trendRowPattern` can never parse back out (which would otherwise make that history entry vanish on the very next rebuild). Phase 3's update.sh must still pin `date -u '+%Y-%m-%d %H:%M UTC'` — this is a defensive fallback, not a substitute.
- **`parseTestJSON` skips an unparseable line instead of failing the whole render.** A job killed mid-`tee` can leave a truncated last line in `test-report.json`; per the "never hard-fails on absent data" acceptance criterion, that one line is now skipped and every event before it still counts.
- **`mergeTrend` trims to `maxTrendRows` on both the prepend and the dedup-skip path**, so a hand-edited body that exceeds the cap doesn't persist indefinitely across weekly refreshes that don't add a new row.
- **`flag.ContinueOnError` instead of `flag.ExitOnError`** in both subcommands, so the existing `if err != nil` branches around `flagSet.Parse` are live code (they were dead under `ExitOnError`, which calls `os.Exit` itself) and a bad flag gets the tool's own `error:` prefix and exit code.

Not changed, by design (flagged by the advisor, judged correct as specced): the trend merge intentionally *skips* adding a row when the commit is already present rather than updating it in place (decision 5) — a same-commit double-fire from CI-then-quality completing keeps whichever run wrote first; and the summary's "N deferred linters" count is `len(ByLinter)` (linters with ≥1 finding in the debt pass), which is what the lint-debt JSON can actually tell us, not a separately-tracked "declared deferred" count.

### Phase 2

The repo's deferred-linter debt is genuinely zero today (documented in this plan's own "Current State"/"History" text — the 2026-08 cleanup that installed the gates also fixed every deferred finding the same week). Two consequences, both cosmetic, not functional:

- The local debt pass (`golangci-lint run -c .golangci.debt.yml ...`) exits 0 with `lint-debt.json` containing 0 issues, not the ">400" the plan's verification step expected when it was written against the pre-cleanup repo. Confirmed working end to end with real data instead: `make test-unit GOTESTFLAGS=-json` on this repo produces 187 pass / 32 skip, and `hack/dashboard render --tests ...` renders that correctly.
- Until a linter is deferred again, the dashboard's "Lint debt" row and per-linter table will show "0 findings across 0 deferred linters" / an empty table — expected, not a bug.

## Assisted-by Trailer Breakdown

Most commits carry an `Assisted-by` trailer (per AGENTS.md attribution rules). Snapshot as of this plan (`git log --no-merges --numstat`, non-merge commits, LOC = added + deleted, one row counted per distinct tool on multi-tool commits):

| Model/Tool                  | Commits | Lines added | Lines deleted | LOC total |
|------------------------------|---------|-------------|----------------|-----------|
| Claude:claude-opus-4-8        | 49      | 22,853      | 871            | 23,724    |
| Claude:claude-fable-5          | 34      | 13,197      | 6,258          | 19,455    |
| Claude:claude-sonnet-5         | 26      | 14,209      | 455            | 14,664    |
| Claude:claude-opus-5           | 11      | 3,557       | 531            | 4,088     |
| Kimi:Kimi-K2.7-Code             | 1       | 79          | 40             | 119       |
| (no Assisted-by trailer)      | 2       | 20          | 328            | 348       |

Could feed a future dashboard row (`Assisted-by breakdown`) alongside coverage/lint-debt/tests/security — same producer-artifact pattern, computed via `git log --numstat` in a CI step rather than added to the Go tool's input surface. Out of scope for this plan; noted here for later consideration.

## References

- `docs/agents/plans/2026-08-21-quality-gates-overhaul.md` — the plan that installed the gates and data sources this dashboard surfaces
- `.github/workflows/ci.yml`, `.github/workflows/quality.yml` — producer workflows; action SHA pins to reuse
- `.golangci.yml` — deferred-disable block with `# deferred:` head comments (strip-deferred contract)
- `Makefile:76-86` — `test-unit` / `coverage` targets
- https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#workflow_run — trigger semantics (named workflows, `branches` filters on head branch)
- https://golangci-lint.run/docs/usage/configuration/#output-configuration — v2 `--output.json.path`
- https://pkg.go.dev/gopkg.in/yaml.v3#Node — comment-preserving YAML nodes
