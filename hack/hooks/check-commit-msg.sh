#!/usr/bin/env bash
# SPDX-License-Identifier: CC0-1.0
#
# check-commit-msg.sh — validate a commit message subject (or PR title)
# against this repository's Conventional Commits rules.
#
# One validator, two callers: the pre-commit commit-msg hook and
# .github/workflows/pr-title.yml both run this script, so a local commit and a
# squash-merge PR title can never be judged by two different implementations.
#
# The rules come from AGENTS.md (https://www.conventionalcommits.org/en/v1.0.0/,
# restricted):
#   - Allowed types: feat, fix, chore only.
#   - Optional lowercase scope in parentheses, e.g. feat(cli): ...
#   - Optional "!" after the type/scope to flag a breaking change.
#
# What it strips before validating:
#   - everything from git's scissors line onwards (`git commit --verbose`
#     appends the full diff below it, and the hook sees the file before git's
#     own cleanup removes it)
#   - comment lines
#   - leading blank lines
#
# What it lets through unvalidated: Merge, Revert, fixup! and squash! subjects.
# git generates the first two itself (and runs the commit-msg hook for them),
# and the last two are consumed by an interactive rebase before they ever
# reach the default branch. These exemptions are harmless for PR titles.
#
# Usage (pre-commit passes the path automatically at the commit-msg stage):
#   hack/hooks/check-commit-msg.sh .git/COMMIT_EDITMSG
#
# Exit codes: 0 valid, 1 invalid, 2 usage error.

set -euo pipefail

EXIT_OK=0
EXIT_INVALID=1
EXIT_USAGE=2

SUBJECT_RE='^(feat|fix|chore)(\([a-z0-9./-]+\))?!?: .+'

err() { printf 'error: %s\n' "$*" >&2; }

if [ $# -ne 1 ]; then
  err "usage: hack/hooks/check-commit-msg.sh COMMIT_MSG_FILE"
  exit "$EXIT_USAGE"
fi

MSG_FILE="$1"

if [ ! -f "$MSG_FILE" ]; then
  err "commit message file not found: $MSG_FILE"
  exit "$EXIT_USAGE"
fi

# Take the first line that is neither a comment nor blank, stopping at the
# scissors line that `git commit --verbose` uses to separate the message from
# the diff below it.
SUBJECT="$(
  awk '
    /^# ------------------------ >8 ------------------------$/ { exit }
    /^#/ { next }
    /^[[:space:]]*$/ { next }
    { print; exit }
  ' "$MSG_FILE"
)"

# An empty message is git's own error to report; saying it twice helps nobody.
if [ -z "$SUBJECT" ]; then
  exit "$EXIT_OK"
fi

# Subjects git generates itself, or that an interactive rebase consumes.
case "$SUBJECT" in
  "Merge "* | "Revert \""* | "fixup! "* | "squash! "*)
    exit "$EXIT_OK"
    ;;
esac

if [[ "$SUBJECT" =~ $SUBJECT_RE ]]; then
  exit "$EXIT_OK"
fi

err "invalid commit subject: '$SUBJECT'"
err "expected: type(scope)!: description"
err "  - type must be one of: feat fix chore"
err "  - scope is optional: lowercase letters, digits, \". / -\""
err "  - \"!\" is optional and flags a breaking change"
err "examples:"
err "  feat(cli): add user list command"
err "  fix: correct rounding in split calculation"
err "  chore!: drop the legacy config format"
exit "$EXIT_INVALID"
