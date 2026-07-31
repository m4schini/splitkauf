#!/usr/bin/env bash
# format-file.sh <file>
#
# Dispatches formatting/linting for a single file based on its extension.
# Intended to be called from the Claude Code PostToolUse hook (.claude/settings.json)
# right after an Edit/Write, but works standalone too:
#
#   hack/format-file.sh path/to/file.go
#
# Go files are formatted with gofumpt. Frontend files (under frontend/) are
# formatted with prettier and, for .ts/.tsx, additionally linted (with
# autofix) via oxlint. If the linter still reports errors after autofix, the
# script exits non-zero with a message on stderr so the caller sees the
# failure.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <file>" >&2
  exit 1
fi

file="$1"

# Resolve the repo root so we can normalize absolute paths (as passed by the
# Claude Code hook) to repo-relative ones, and so the script works regardless
# of the caller's current working directory.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

case "$file" in
  "$repo_root"/*)
    file="${file#"$repo_root"/}"
    ;;
esac

# Nothing to do for files outside the repo tree or that no longer exist
# (e.g. deleted by the tool that triggered the hook).
if [ ! -f "$repo_root/$file" ]; then
  exit 0
fi

cd "$repo_root"

case "$file" in
  *.go)
    go run mvdan.cc/gofumpt@v0.7.0 -w "$file"
    ;;
  frontend/*.ts | frontend/*.tsx | frontend/*.css | frontend/*.json | frontend/*.md)
    # npm --prefix frontend exec does NOT change the child process's cwd, so
    # paths are passed repo-relative (i.e. including the frontend/ prefix),
    # not frontend-relative.
    npm --prefix frontend exec -- prettier --write "$file"
    case "$file" in
      frontend/*.ts | frontend/*.tsx)
        if ! npm --prefix frontend exec -- oxlint --fix "$file"; then
          echo "hack/format-file.sh: oxlint reported errors in $file after --fix; please address them manually" >&2
          exit 1
        fi
        ;;
    esac
    ;;
  *)
    # No formatter registered for this file type; nothing to do.
    exit 0
    ;;
esac
