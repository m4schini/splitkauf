# splitkauf

## Development harness

This repo enforces its quality gates at three points: edit time (a Claude
Code hook), commit time (`pre-commit`), and on demand (`make check`).

### One-time setup

```sh
pre-commit install --install-hooks
```

`.pre-commit-config.yaml` sets `default_install_hook_types: [pre-commit, commit-msg]`,
so this single command installs both the pre-commit hook (file hygiene,
secret scanning, `gofumpt`/`golangci-lint` on staged Go files,
`prettier`/`oxlint` on staged frontend files) and the commit-msg hook
(enforces a `feat|fix|chore` conventional-commit subject line).

### Running the gates manually

```sh
pre-commit run --all-files   # everything pre-commit checks, across the whole tree
make check                   # fmt-check, lint, lint-vet, tidy-check, test-unit, security, frontend-check
```

`make check`'s `fmt-check` step runs `gofumpt -w .` and then diffs the whole
working tree; on a tree with unrelated uncommitted changes it can report a
false positive even when the Go code itself is correctly formatted — verify
with `gofumpt -l .` (should print nothing) if `fmt-check` fails on a dirty
tree.

`make security` runs `govulncheck`. It may report vulnerabilities that stem
from the *host's installed Go toolchain* (stdlib CVEs fixed in a later Go
patch release) rather than from repository code — check whether the reported
module is `stdlib` before treating a `security` failure as a real issue.

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
