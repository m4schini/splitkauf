---
date: 2026-08-01T10:40:00+00:00
git_commit: 5c46d5f
branch: main
topic: "M7: username/password authentication (operator-provisioned, no OIDC)"
tags: [plan, m7, auth, password, bcrypt, sessions, cli]
status: ready
---

# PLAN: M7 — Username/Password Authentication

Add a third authentication mode alongside dev-auth and OIDC: local
username/password accounts, for self-hosted instances that don't run an
identity provider. Accounts are **operator-provisioned via CLI** (no public
sign-up), passwords are bcrypt-hashed, and login reuses the existing scs
server-side session layer so the browser only ever holds the opaque HttpOnly
cookie.

## Product decisions (from the user)

1. **Operator-provisioned only (US-A.7).** A `splitkauf useradd <username>`
   command creates accounts; there is **no** registration endpoint or UI.
2. **Explicit enable flag.** `SPLITKAUF_AUTH_PASSWORD_ENABLED=true` turns on
   password mode. Selection precedence: OIDC (if configured) → password (if
   enabled) → dev-auth. Nothing changes when the flag is unset.
3. **Reuse the session infrastructure.** No new cookie/store: the password
   authenticator writes the same scs session the OIDC flow uses (minus the
   OAuth tokens), so `RequireAuth`, logout, and durable Postgres sessions all
   work identically.

## Acceptance Criteria

- **US-A.7:** `splitkauf useradd alex` prompts for a password (no echo) or reads
  it from stdin (`--password-stdin`), stores only a bcrypt hash, rejects
  duplicate usernames with a nonzero exit, and never writes the plaintext to
  the DB, logs, or argv.
- **US-A.6:** with `SPLITKAUF_AUTH_PASSWORD_ENABLED=true` and OIDC unset, the
  login screen shows a username+password form; a correct credential establishes
  a session (HttpOnly cookie only) and `GET /api/v1/me` returns the user; a
  wrong username and a wrong password both return the same 401 (no
  enumeration); logout destroys the session.
- Password verification is constant-time (bcrypt); a login for an unknown user
  still performs a bcrypt comparison against a dummy hash so response timing
  doesn't leak user existence.
- The frontend discovers the mode from a public `GET /api/auth/config`; OIDC and
  dev behaviour are unchanged.
- `make check` green; `go test -race` clean on touched packages; one commit per
  phase (migration in its own commit).

## Technical Key Decisions and Tradeoffs

1. **New `users` table, `members` unchanged.** `users` holds the credential
   (`id uuid`, `username` unique, `password_hash`, `name`, optional `email`,
   timestamps). On login the user is upserted into `members` (subject =
   `users.id` string) exactly like an OIDC account, so list/membership code is
   mode-agnostic. `User.ID` is the `users.id` UUID.
   - Why: keeps the credential store separate from the membership record and
     lets everything downstream stay identical across modes.
2. **bcrypt via `golang.org/x/crypto/bcrypt`.** Default cost. Verification uses
   `CompareHashAndPassword` (constant-time). Promote `x/crypto` to a direct
   dependency (already present transitively).
   - Tradeoff: bcrypt caps the password at 72 bytes; `useradd` rejects longer
     inputs with a clear message rather than silently truncating.
3. **Password authenticator satisfies the existing `Authenticator` interface.**
   `Login` handles a credential **POST** (JSON `{username,password}`): validate,
   `RenewToken` (session-fixation), write a `SessionData` with empty tokens and
   the user's subject/name/email, upsert the member, return `204`. A **GET**
   `Login` just redirects to `/` (the SPA renders the form). `Callback` 404s.
   `Logout` destroys the session. `RequireAuth` loads the session → user (no
   token refresh path).
   - Impact: `ports/rest` registers `POST /api/auth/login` in addition to the
     existing `GET`; both map to `authr.Login`. The POST body is small and the
     route is outside the API subrouter's body cap — add a local 4 KiB read
     limit in the handler.
4. **Anti-enumeration.** `GetByUsername` miss → compare the submitted password
   against a fixed dummy bcrypt hash, then fail — equal timing whether or not
   the user exists; identical 401 problem for both.
5. **Mode discovery endpoint.** `GET /api/auth/config` (public, no session)
   returns `{"mode":"password"|"oidc"|"dev"}` so the SPA renders the right login
   affordance. It's the only new public endpoint; there is deliberately no
   register/signup route.

## Current State

```
auth/
├── auth.go      Authenticator interface + New() selects dev|oidc from config
├── dev.go       devAuthenticator (injects fixed user)
├── oidc.go      oidcAuthenticator (BFF, scs sessions, now instrumented)
├── session.go   SessionData (JSON in scs) + get/put helpers
config/config.go  AuthConfig{OIDC,Session}; IsOIDCEnabled()
cmd/              cobra: serve, migrate (no user admin)
ports/rest/server.go  GET /api/auth/login, GET /callback, POST /logout
frontend/src/App.tsx   single "Log in" button -> GET /api/auth/login
members table     keyed by subject; upserted on login (OIDC) / at boot (dev)
```

## Desired End State

```
config: SPLITKAUF_AUTH_PASSWORD_ENABLED -> IsPasswordEnabled()
auth.New: OIDC configured ? oidc : password enabled ? password : dev
users table (migration 000006): username + bcrypt hash
CLI: splitkauf useradd <username> [--password-stdin]
POST /api/auth/login {username,password} -> 204 + session cookie (password mode)
GET  /api/auth/config -> {"mode": "..."}   (public)
frontend: fetch /api/auth/config -> render password form | OIDC button | (dev)
```

## Implementation

### Phase 1: Migration 000006 — users table (own commit)

- [x] `000006_password_users.up.sql`:
      `CREATE TABLE users (id uuid PK default gen_random_uuid(), username text NOT NULL UNIQUE, password_hash text NOT NULL, name text NOT NULL DEFAULT '', email text, created_at, updated_at)`.
- [x] `000006_password_users.down.sql`: `DROP TABLE users`.
- **Verify:** `go run . migrate` up to 6 and down against a disposable Postgres.

### Phase 2: users domain + bcrypt + repo + config flag

Dependencies: Phase 1

- [x] `users/users.go`: `User` domain (Id, Username, Name, Email), `Repository`
      (`Create(ctx, NewUser) (User,error)` with `ErrUsernameTaken`;
      `GetByUsername(ctx, string) (User, hash string, error)` with
      `ErrNotFound`). `HashPassword`/`CheckPassword` (bcrypt) + length guard.
- [x] `adapters/db/users.go`: Postgres repo; map unique-violation (23505) →
      `ErrUsernameTaken`, no-rows → `ErrNotFound`.
- [x] `config`: `IsPasswordEnabled()` from `auth.password.enabled`
      (`SPLITKAUF_AUTH_PASSWORD_ENABLED`); default false. Unit test the
      selection precedence.
- **Verify:** `go test ./users/... ./adapters/db/... ./config/...` (integration
      with DSN); `make lint`.

### Phase 3: password authenticator + mode endpoint + wiring

Dependencies: Phase 2

- [x] `auth/password.go`: `passwordAuthenticator` implementing `Authenticator`
      (Key Decision 3 + 4: constant-time, dummy-hash on miss, member upsert,
      RenewToken, 204). Reuse `SessionData`/put/get.
- [x] `auth/auth.go` `New`: add the password branch (OIDC → password → dev).
- [x] `ports/rest/server.go`: register `POST /api/auth/login` → `authr.Login`;
      add `GET /api/auth/config` returning the mode (needs the mode from config,
      passed into `New`). Keep both outside the API body-cap subrouter (login
      handler self-limits its body).
- [x] `cmd/serve.go`: build the users repo + password authenticator when the
      flag is set; pass the resolved mode string to the config endpoint.
- [x] Tests: happy login (204 + session), wrong password / unknown user both
      401 and indistinguishable, logout destroys session, `/api/auth/config`
      returns the right mode. **Security/go review** of `auth/password.go`.
- **Verify:** `go test -race ./auth/... ./ports/...`; `make lint`.

### Phase 4: CLI `useradd`

Dependencies: Phase 2

- [x] `cmd/useradd.go`: `useradd <username>`; read password from a no-echo TTY
      prompt (confirm twice) or `--password-stdin`; bcrypt; `repo.Create`;
      duplicate → nonzero exit with a clear message. No plaintext to logs/argv.
- [x] `cmd/useradd_test.go`: validation (empty username/password, >72 bytes),
      the stdin path.
- **Verify:** `go test ./cmd/...`; manual `useradd` against the disposable DB.

### Phase 5: frontend login form + mode discovery

Dependencies: Phase 3

- [x] `frontend/src/api.ts`: `getAuthConfig()` → `{mode}`;
      `passwordLogin(username,password)` → `POST /api/auth/login` (204).
- [x] `frontend/src/App.tsx`: fetch the mode; render a labelled
      username/password form (submit → passwordLogin → invalidate `['me']`) in
      password mode; keep the redirect button for OIDC; dev unchanged. Inline
      error on 401 (no enumeration wording). UX §6 (≥44px, ≥16px, AA, no
      blocking spinner).
- [x] Tests: form renders in password mode, submits and re-queries me; 401 shows
      an error; OIDC mode still shows the redirect button.
- **Verify:** `make frontend-check`; `make check` from a clean tree.

### Phase 6: docs + push

- [x] `deploy/README.md`: password mode env, `useradd`, "no public sign-up".
- [x] `docs/architecture.md`: auth section — third mode + users table.
- [x] Quadlet env example note.
- **Verify:** `make check` green; `make dist`; push.

## Implementation Notes

- **Delivered as planned**, all six phases, one commit each (migration and the
  useradd CLI's `x/term` bump in their own commits). Verified end-to-end with
  the built binary in password mode: SPA serves, `GET /api/auth/config` →
  `{"mode":"password"}`, `useradd` provisions an account (bcrypt `$2a$10$`),
  login → 204 + HttpOnly SameSite=Lax cookie, `GET /api/v1/me` → the user;
  wrong password / unknown user / no cookie → 401.
- **Security review outcome (crux SOUND):** enumeration/timing resistance,
  bcrypt handling, session-fixation ordering (`RenewToken` before
  `putSessionData`), RequireAuth, the 4 KiB body limit → clean 400, and the DB
  layer were all confirmed correct. One confirmed defect fixed: the password
  min-length guard combined a byte-length and a rune-count check with `&&`,
  making the rune term dead code and enforcing "8 bytes" not "8 characters" —
  now measured in runes (max stays in bytes, bcrypt's truncation unit).
  Applied improvements: `dummyHash` computed lazily via `sync.Once` (no bcrypt
  in package init for dev/OIDC/test binaries); `Recover` added to the
  hand-written auth route group for problem+json parity; the
  indistinguishability test now asserts equal response bodies byte-for-byte.
- **Known gaps (out of scope, noted for later):** no login rate-limiting /
  lockout (bcrypt cost is the only brute-force brake — add a per-IP+username
  limiter + a rejected-login metric before GA); no CSRF token on the login POST
  (acceptable given SameSite=Lax + the `application/json` +
  `DisallowUnknownFields` requirement, matching the existing plain-POST logout);
  no immediate session revocation when a `users` row is deleted (same tradeoff
  as the OIDC path — the session stays valid until its scs lifetime expires).

## References

- User stories: `docs/user-stories/US-A.6`, `US-A.7`.
- `auth/oidc.go`, `auth/session.go` (session reuse), `cmd/serve.go`
  (`sessionStore` precedence), `config/config.go` (`IsOIDCEnabled`).
- `golang.org/x/crypto/bcrypt`.
