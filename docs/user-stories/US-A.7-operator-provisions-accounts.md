# US-A.7 — Operator provisions password accounts

**Milestone:** M7
**Depends on:** —

**As an** operator of a self-hosted instance, **I want** to create password
accounts from the command line, **so that** I control exactly who has access
without exposing a public sign-up.

## Acceptance criteria

- A `splitkauf user add <username>` command creates an account, reading the
  password from an interactive no-echo prompt or, for automation, from stdin
  (`--password-stdin`).
- The password is stored only as a bcrypt hash; the plaintext is never written
  to the database, logs, argv, or the terminal.
- Usernames are unique; adding a duplicate fails with a clear error and a
  nonzero exit.
- There is **no** public registration endpoint or UI — accounts exist only if
  the operator created them.
- The command runs against the same database as the server (reusing the
  connection config) and is safe to run before the server starts.

## Related

Account **consolidation** (merging a provisioned local account into another
identity, e.g. after an identity provider is introduced) is covered by the
`user merge` operator command together with `user ls` for discovery — see the
operator guide in `deploy/README.md`. No separate user story.
