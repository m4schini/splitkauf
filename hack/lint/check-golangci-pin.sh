#!/usr/bin/env bash
# SPDX-License-Identifier: CC0-1.0
#
# check-golangci-pin.sh — prove the two golangci-lint pins agree.
#
# The version is pinned in two places, and it has to be:
#
#   - .pre-commit-config.yaml `rev:` — pins the hook *and* the tool it runs,
#     which is what makes a contributor's commit-time lint deterministic. This
#     is the authoritative pin.
#   - .github/workflows/quality.yml `go install .../golangci-lint@vX.Y.Z` —
#     what CI's quality gates install and run.
#
# If those drift, a commit passes the hook and fails CI (or the reverse) for
# reasons nobody can see in the diff. This script is wired into
# .pre-commit-config.yaml as a local hook so drift is caught at the commit
# that introduces it.
#
# Usage:
#   hack/lint/check-golangci-pin.sh
#   WORKFLOW_FILE=path/to/fixture.yml hack/lint/check-golangci-pin.sh
#
# WORKFLOW_FILE overrides the workflow location so the script is testable
# against a fixture.
#
# Exit codes: 0 pins agree, 1 pins disagree or could not be read.

set -euo pipefail

EXIT_OK=0
EXIT_ERROR=1

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

PRE_COMMIT_CONFIG="$REPO_ROOT/.pre-commit-config.yaml"
WORKFLOW_FILE="${WORKFLOW_FILE:-$REPO_ROOT/.github/workflows/quality.yml}"

err() { printf 'error: %s\n' "$*" >&2; }

if [ ! -f "$PRE_COMMIT_CONFIG" ]; then
  err "missing .pre-commit-config.yaml"
  exit "$EXIT_ERROR"
fi

# TEMPORARY (until quality.yml lands): a missing default workflow file is a
# warning, not an error, so this hook can ship before the workflow does.
# Hardened to an error together with .github/workflows/quality.yml.
if [ ! -f "$WORKFLOW_FILE" ]; then
  printf 'warning: workflow file not found (%s); skipping pin drift check\n' "$WORKFLOW_FILE" >&2
  exit "$EXIT_OK"
fi

# Walk the pre-commit config looking for the golangci-lint repo entry, then
# take the first `rev:` that follows it. Deliberately not a YAML parser: the
# file is ours, its shape is stable, and a hook that needs PyYAML installed to
# check a pin would be a bootstrapping problem of its own.
precommit_version="$(
  awk '
    /^[[:space:]]*-?[[:space:]]*repo:[[:space:]]*/ {
      in_golangci = ($0 ~ /golangci\/golangci-lint/)
    }
    in_golangci && /^[[:space:]]*rev:[[:space:]]*/ {
      sub(/^[[:space:]]*rev:[[:space:]]*/, "")
      gsub(/["'"'"']/, "")
      sub(/[[:space:]]*#.*$/, "")
      gsub(/[[:space:]]+$/, "")
      print
      exit
    }
  ' "$PRE_COMMIT_CONFIG"
)"

# The version CI installs: `go install .../golangci-lint/v2/cmd/golangci-lint@vX.Y.Z`.
workflow_version="$(
  grep -Eo 'golangci-lint/v2/cmd/golangci-lint@v[0-9]+\.[0-9]+\.[0-9]+' "$WORKFLOW_FILE" \
    | head -n1 \
    | sed 's/.*@//'
)"

if [ -z "$precommit_version" ]; then
  err "could not read the golangci-lint rev from .pre-commit-config.yaml"
  exit "$EXIT_ERROR"
fi

if [ -z "$workflow_version" ]; then
  err "could not read the golangci-lint go install version from $WORKFLOW_FILE"
  exit "$EXIT_ERROR"
fi

if [ "$precommit_version" != "$workflow_version" ]; then
  err "golangci-lint pin drift:"
  err "  .pre-commit-config.yaml rev = $precommit_version"
  err "  ${WORKFLOW_FILE#"$REPO_ROOT"/} go install = $workflow_version"
  err ""
  err "Set both to the same version. They pin the same tool: the hook lints"
  err "commits, CI's quality gates lint everything else."
  exit "$EXIT_ERROR"
fi

exit "$EXIT_OK"
