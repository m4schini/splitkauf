#!/usr/bin/env bash
# SPDX-License-Identifier: CC0-1.0
#
# update.sh — rebuild the "Quality Dashboard" GitHub issue from the latest
# CI/quality producer artifacts.
#
# Called by .github/workflows/dashboard.yml after CI or quality complete on
# main, and by workflow_dispatch for a manual rebuild. Runnable locally with
# `gh` logged in:
#
#   REPO=owner/repo GH_TOKEN="$(gh auth token)" hack/dashboard/update.sh
#
# Stages (each logged to stdout):
#   1. Download the latest coverage / test-report / lint-debt artifacts from
#      the last RUNS_PER_WORKFLOW completed runs of ci.yml / quality.yml on
#      main. A producer that never uploaded (missing everywhere — a first
#      run, a failed job) is left absent: hack/dashboard render treats an
#      absent input as "n/a", never as a hard failure.
#   2. Find the latest quality.yml run with a "Security scan" step outcome
#      and record its status/timestamp/run URL.
#   3. Fetch the previous dashboard issue body, if one exists (found by the
#      `dashboard` label — no hardcoded issue number).
#   4. Render the new body with `go run ./hack/dashboard render`.
#   5. Create the issue and label if missing, then overwrite the body.
#
# Env:
#   REPO              owner/name (required)
#   GH_TOKEN          gh CLI auth token (required)
#   TRIGGER_HEAD_SHA  the triggering workflow_run's head SHA; logging only,
#                     not load-bearing (the coverage-run's own head_sha keys
#                     the trend row — see stage 4)
#
# Exit codes: 0 on a successful rebuild; propagates the first failing `gh`/
# `go`/`jq` command otherwise (set -euo pipefail).
#
# Must be run from the repository root (it invokes `go run ./hack/dashboard`).

set -euo pipefail

EXIT_ERROR=1

RUNS_PER_WORKFLOW=10
DASHBOARD_LABEL="dashboard"
DASHBOARD_TITLE="Quality Dashboard"
SECURITY_STEP_NAME="Security scan"

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
err() { printf 'error: %s\n' "$*" >&2; }

if [ -z "${REPO:-}" ]; then
  err "REPO (owner/name) must be set"
  exit "$EXIT_ERROR"
fi

if [ -z "${GH_TOKEN:-}" ]; then
  err "GH_TOKEN must be set"
  exit "$EXIT_ERROR"
fi

TRIGGER_HEAD_SHA="${TRIGGER_HEAD_SHA:-}"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

log "repo=$REPO trigger_head_sha=${TRIGGER_HEAD_SHA:-<none>}"

# runs_on_main WORKFLOW_FILE
# Writes "id\thead_sha\thtml_url" (newest first) for the last
# RUNS_PER_WORKFLOW completed runs of WORKFLOW_FILE on main.
runs_on_main() {
  local workflow="$1"

  gh api "repos/$REPO/actions/workflows/$workflow/runs?branch=main&status=completed&per_page=$RUNS_PER_WORKFLOW" \
    --jq '.workflow_runs[] | [(.id | tostring), .head_sha, .html_url] | @tsv'
}

# run_has_artifact RUN_ID ARTIFACT_NAME
# Succeeds iff RUN_ID uploaded an artifact named exactly ARTIFACT_NAME.
run_has_artifact() {
  local run_id="$1" artifact="$2"
  local names
  names="$(gh api "repos/$REPO/actions/runs/$run_id/artifacts" --jq '.artifacts[].name')"

  grep -qxF "$artifact" <<<"$names"
}

# download_artifact RUNS_TSV ARTIFACT_NAME DEST_FILE
# Downloads ARTIFACT_NAME's single file to DEST_FILE from the newest run in
# RUNS_TSV that has it, and prints that run's "id\thead_sha\thtml_url". A
# miss in every listed run is not fatal: DEST_FILE is left absent, a warning
# is logged, and nothing is printed.
download_artifact() {
  local runs="$1" artifact="$2" dest="$3"
  local id sha url

  while IFS=$'\t' read -r id sha url; do
    [ -z "$id" ] && continue

    if ! run_has_artifact "$id" "$artifact"; then
      continue
    fi

    local dldir="$WORKDIR/dl-$artifact-$id"
    gh run download "$id" --repo "$REPO" -n "$artifact" -D "$dldir"

    local src
    src="$(find "$dldir" -type f -print -quit)"
    if [ -z "$src" ]; then
      warn "'$artifact' artifact from run $id had no files"
      return 0
    fi

    cp "$src" "$dest"
    log "$artifact <- run $id (commit ${sha:0:7})"
    printf '%s\t%s\t%s\n' "$id" "$sha" "$url"

    return 0
  done <<<"$runs"

  warn "no run in the last $RUNS_PER_WORKFLOW has a '$artifact' artifact"
}

# find_security_status RUNS_TSV DEST_FILE
# Writes {status, completed_at, run_url} to DEST_FILE for the newest run in
# RUNS_TSV whose jobs contain a "$SECURITY_STEP_NAME" step that concluded
# success or failure. No match: DEST_FILE is left absent.
find_security_status() {
  local runs="$1" dest="$2"
  local id sha url

  while IFS=$'\t' read -r id sha url; do
    [ -z "$id" ] && continue

    local step_json
    step_json="$(
      gh api "repos/$REPO/actions/runs/$id/jobs" \
        | jq -c --arg name "$SECURITY_STEP_NAME" \
          '[.jobs[].steps[]? | select(.name == $name and (.conclusion == "success" or .conclusion == "failure"))][0] // empty'
    )"

    if [ -z "$step_json" ]; then
      continue
    fi

    local status completed_at
    status="$(jq -r '.conclusion' <<<"$step_json")"
    completed_at="$(jq -r '.completed_at' <<<"$step_json")"
    completed_at="${completed_at%%T*}"

    jq -n --arg status "$status" --arg completed_at "$completed_at" --arg run_url "$url" \
      '{status: $status, completed_at: $completed_at, run_url: $run_url}' >"$dest"

    log "security scan status=$status <- run $id"

    return 0
  done <<<"$runs"

  warn "no run in the last $RUNS_PER_WORKFLOW has a '$SECURITY_STEP_NAME' step outcome"
}

COVERAGE_FILE="$WORKDIR/coverage.out"
TESTS_FILE="$WORKDIR/test-report.json"
LINT_DEBT_FILE="$WORKDIR/lint-debt.json"
SECURITY_FILE="$WORKDIR/security.json"
META_FILE="$WORKDIR/meta.json"
PREV_BODY_FILE="$WORKDIR/prev-body.md"
BODY_FILE="$WORKDIR/body.md"

log "stage 1: producer artifacts"

CI_RUNS="$(runs_on_main ci.yml)"
QUALITY_RUNS="$(runs_on_main quality.yml)"

COVERAGE_RUN="$(download_artifact "$CI_RUNS" coverage "$COVERAGE_FILE")"
download_artifact "$CI_RUNS" test-report "$TESTS_FILE" >/dev/null
download_artifact "$QUALITY_RUNS" lint-debt "$LINT_DEBT_FILE" >/dev/null

log "stage 2: security status"

find_security_status "$QUALITY_RUNS" "$SECURITY_FILE"

log "stage 3: previous issue body"

ISSUE_NUMBER="$(gh issue list --repo "$REPO" --label "$DASHBOARD_LABEL" --state open --json number --jq '.[0].number // empty')"

if [ -n "$ISSUE_NUMBER" ]; then
  log "found existing issue #$ISSUE_NUMBER"
  gh issue view "$ISSUE_NUMBER" --repo "$REPO" --json body --jq '.body' >"$PREV_BODY_FILE"
else
  log "no existing dashboard issue"
fi

log "stage 4: render"

COMMIT="n/a"
RUN_URL="n/a"
if [ -n "$COVERAGE_RUN" ]; then
  IFS=$'\t' read -r _ COMMIT RUN_URL <<<"$COVERAGE_RUN"
fi

UPDATED="$(date -u '+%Y-%m-%d %H:%M UTC')"

jq -n --arg commit "$COMMIT" --arg run_url "$RUN_URL" --arg updated "$UPDATED" \
  '{commit: $commit, run_url: $run_url, updated: $updated}' >"$META_FILE"

# Every flag is passed unconditionally: hack/dashboard render treats a
# missing path the same as an unset flag (both render "n/a"), so there is no
# need to build the argument list conditionally here.
go run ./hack/dashboard render \
  --coverage "$COVERAGE_FILE" \
  --tests "$TESTS_FILE" \
  --lint-debt "$LINT_DEBT_FILE" \
  --security "$SECURITY_FILE" \
  --meta "$META_FILE" \
  --prev-body "$PREV_BODY_FILE" \
  >"$BODY_FILE"

log "stage 5: upsert issue"

if [ -z "$ISSUE_NUMBER" ]; then
  gh label create "$DASHBOARD_LABEL" --repo "$REPO" --force \
    --description "Auto-updated quality dashboard" --color 0E8A16

  issue_url="$(gh issue create --repo "$REPO" --title "$DASHBOARD_TITLE" \
    --label "$DASHBOARD_LABEL" --body "initializing")"
  ISSUE_NUMBER="${issue_url##*/}"
  log "created issue #$ISSUE_NUMBER"
fi

gh issue edit "$ISSUE_NUMBER" --repo "$REPO" --body-file "$BODY_FILE"

log "updated issue #$ISSUE_NUMBER"
