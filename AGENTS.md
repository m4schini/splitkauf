# AGENTS.md

Guidance for AI coding assistants (Cursor, GitHub Copilot, OpenCode, Claude, etc.) contributing to this project.

License: CC0-1.0

---

## Attribution

### For AI-assisted commits

AI assistants MUST NOT add `Signed-off-by` tags — only a human can certify the DCO. The human committer is responsible for:

- Reviewing all AI-generated code.
- Ensuring licensing compliance.
- Adding their own `Signed-off-by`.
- Taking full responsibility for the contribution.

AI assistants MUST NOT add `Co-authored-by` tags.

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

Follow this standard for naming commits and to verify the titles of PRs: https://www.conventionalcommits.org/en/v1.0.0/ Restrict to the core types — `feat`, `fix`, `chore` — plus a `!` after the type/scope or a `BREAKING CHANGE:` footer to flag breaking changes. Do not use other types (`docs`, `refactor`, `style`, `perf`, `test`, `build`, `ci`, etc.).

Commits that add or update research documents use `chore(research)`. Commits that add or update implementation plans use `chore(plans)`.

Each addition, change, or deletion of a research document or plan MUST be its own commit, containing only that research document or plan — do not mix it with code changes or with other research/plan files.

When implementing a plan, commit after each completed phase — one commit per phase, made once the phase's verification passes. Do not batch multiple phases into a single commit, and do not leave a finished phase uncommitted while starting the next one.

Database migrations MUST be committed in their own commit, separate from application/code changes.

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

## Implementation Plans

Before starting implementation of a plan phase, check whether any of its steps are independent of each other and could be parallelised by spawning subagents. If so, offer the user the choice between:

- Spawning subagents to work on the independent steps in parallel.
- Continuing normally, implementing the steps sequentially yourself.

Do not spawn subagents for this purpose without the user's explicit consent first.

---

## Code Review

When reviewing Go code or a Go pull request, use the `go-review` skill rather than an ad-hoc review.

---

## Things to Avoid

- ❌ Adding `Signed-off-by` on behalf of a human.
- ❌ Adding `Co-authored-by` tags.
- ❌ Writing or posting PR comments on behalf of a human.
- ❌ Committing secrets, tokens, or credentials.
- ❌ Spawning subagents to parallelise plan steps without the user's explicit consent.
