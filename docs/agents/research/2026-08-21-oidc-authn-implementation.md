---
date: 2026-08-21T19:12:33Z
git_commit: df6a1d752a09fe8c2f43a7ae0b6c3d7f06bcd001
branch: main
topic: "OIDC and authentication (authn) — current implementation"
tags: [research, codebase, auth, oidc, password, sessions, bff, scs]
status: complete
---

# Research: OIDC and Authentication (authn) — Current Implementation

## Research Question

How OIDC and authentication work in the splitkauf codebase as implemented today.

Note: `docs/agents/research/2026-07-21-oidc-go-pwa-integration.md` is the *design*
research that preceded the implementation (written when no auth code existed).
This document maps the code that now exists. The implementation follows that
design with two committed deviations: sessions live in **PostgreSQL** (not
Redis), and a third mode — local **username/password** auth — was added in M7.

## Summary

Authentication is a **Backend-for-Frontend (BFF)** port in the top-level `auth`
package. A single `Authenticator` interface (`Login`, `Callback`, `Logout`,
`RequireAuth`) has three implementations, selected once at startup by
`auth.New` from config with the precedence **OIDC → password → dev-auth**
(`config.Mode()`):

1. **OIDC** (`auth/oidc.go`) — confidential-client Authorization Code + PKCE
   (S256) via `coreos/go-oidc/v3` + `golang.org/x/oauth2`. All tokens stay
   server-side; the browser holds only an opaque `HttpOnly`/`SameSite=Lax`
   session cookie. Access tokens are refreshed transparently in `RequireAuth`
   when within 30s of expiry.
2. **Password** (`auth/password.go`) — operator-provisioned local accounts
   (`splitkauf useradd` CLI, bcrypt hashes in a `users` table), no public
   sign-up. Establishes the *same* scs session shape as OIDC, minus tokens.
3. **Dev-auth** (`auth/dev.go`) — a fixed hardcoded user injected on every
   request; login/logout are inert redirects.

Sessions are managed by `alexedwards/scs/v2` with `postgresstore` (the
`sessions` table). In OIDC mode a reachable database at startup is mandatory
(fail-fast, US-Q.6); dev-auth mode falls back to scs's in-memory store so local
development keeps serving with the DB down.

Every sign-in (any mode) upserts a row in the `members` table, which maps the
auth subject to a stable `user_id` UUID used for attribution
(`lists.created_by`, `items.added_by/bought_by`).

### Key files

```
auth/                       — the authentication port
├── auth.go                 — User, Authenticator interface, WithUser/UserFrom, New (mode selection)
├── oidc.go                 — OIDC BFF: Login/Callback/Logout/RequireAuth + refresh
├── password.go             — password mode: credential POST, timing-equalized 401
├── dev.go                  — dev-auth: fixed DevUser, no-op endpoints
├── session.go              — SessionData struct + scs get/put helpers, pre-login keys
├── token.go                — randomToken(): 256-bit state/nonce
├── redirect.go             — safeReturnTo(): open-redirect allowlist
├── auth_test.go            — mode selection, return_to, token, dev, session round-trip
└── password_test.go        — login flow, indistinguishable 401, body limits

config/
├── config.go               — AuthConfig/OIDCConfig/PasswordConfig/SessionConfig,
│                             IsOIDCEnabled, IsPasswordEnabled, Mode()
├── defaults.go             — auth defaults (empty OIDC → dev; 168h session lifetime)
└── validation.go           — issuer set ⇒ client_id/secret/redirect_url required

cmd/
├── serve.go                — sessionStore() decision, newSessionManager(), auth.New wiring
└── useradd.go              — operator account provisioning CLI

ports/rest/server.go        — /api/auth/* routes, RequireAuth middleware wiring,
                              selective LoadAndSave, publicHealth bypass, /api/auth/config

users/users.go              — local-account domain: bcrypt Hash/Verify, password policy
members/members.go          — subject → user_id membership domain (JIT upsert)
adapters/db/users.go        — Postgres users adapter
adapters/db/members.go      — Postgres members upsert adapter

database/migrations/
├── 000003_auth.up.sql      — sessions (scs) + members tables
├── 000006_password_users.up.sql — users table
└── 000007_attribution.up.sql    — members.user_id + backfill (uuid_generate_v5)

frontend/src/
├── api.ts                  — apiFetch (RFC 9457), getAuthConfig, login, passwordLogin, logout
├── LoginForm.tsx           — username/password form (password mode)
└── App.tsx                 — auth gate on useMe(); mode-dependent signed-out UI
```

### Request flow

```
Browser                      Go binary (BFF)                    OIDC IdP
  |                                |                                |
  |-- GET /api/auth/login -------->|  state+nonce (randomToken)     |
  |                                |  PKCE verifier (oauth2)        |
  |                                |  put in scs session            |
  |<-- 302 authorize URL ----------|                                |
  |-------------- authorize + user login ------------------------->|
  |<------------- 302 /api/auth/callback?code&state ---------------|
  |-- GET /api/auth/callback ----->|  constant-time state check     |
  |                                |  clear pre-login keys          |
  |                                |-- exchange code + verifier --->|
  |                                |<-- ID/access/refresh tokens ---|
  |                                |  verify ID token + nonce       |
  |                                |  RenewToken (fixation)         |
  |                                |  putSessionData (Postgres)     |
  |                                |  members.Upsert                |
  |<-- Set-Cookie + 302 return_to--|                                |
  |                                |                                |
  |-- /api/v1/* (cookie) --------->|  RequireAuth:                  |
  |                                |   getSessionData               |
  |                                |   expiry < 30s ⇒ refresh ----->|
  |                                |   inject auth.User in ctx      |
```

## Detailed Findings

### 1. The `auth` package — port and mode selection

- `auth.User` (`auth/auth.go:32`) is the authenticated principal: `ID uuid.UUID`,
  `Name`, `Email`. `ID` is stable: the fixed dev UUID, the local account's own
  UUID (password mode), or a UUIDv5 derived from the OIDC subject.
- `Authenticator` interface (`auth/auth.go:40`): `Login`, `Callback`, `Logout`
  HTTP handlers plus `RequireAuth(next http.Handler) http.Handler` middleware.
- Context helpers `WithUser`/`UserFrom` (`auth/auth.go:61-71`) use a private
  `userContextKey{}`; REST handlers read the user uniformly in all modes
  (e.g. `ports/rest/v1/handlers_lists.go:260`).
- `auth.New` (`auth/auth.go:80`) applies the precedence:
  `cfg.IsOIDCEnabled()` → `newOIDC` (does provider discovery over `ctx`),
  else `cfg.IsPasswordEnabled()` → `newPassword`, else `newDev()`.

### 2. OIDC mode (`auth/oidc.go`)

**Construction** (`newOIDC`, `auth/oidc.go:58`):
- `oidc.NewProvider(ctx, issuer)` — discovery via `.well-known/openid-configuration`.
- `oauth2.Config` as confidential client with scopes
  `openid profile email offline_access` (`auth/oidc.go:64-70`).
- `end_session_endpoint` pulled from the discovery document via
  `provider.Claims(&meta)` (`auth/oidc.go:74-79`) for RP-initiated logout.
- ID-token verifier: `provider.Verifier(&oidc.Config{ClientID})` (`auth/oidc.go:96`).
- Extensive structured startup log of the resolved endpoints and cookie config.

**Login** (`auth/oidc.go:110`):
- `randomToken()` for state and nonce (32 bytes crypto/rand, base64url —
  `auth/token.go:17`); `oauth2.GenerateVerifier()` for PKCE.
- `safeReturnTo(r.URL.Query().Get("return_to"))` validates the post-login path.
- All four values stored under pre-login scs keys (`auth/session.go:21-26`:
  `auth_state`, `auth_nonce`, `auth_pkce_verifier`, `auth_return_to`).
- Redirect to `AuthCodeURL(state, S256ChallengeOption, oidc.Nonce(nonce))`.

**Callback** (`auth/oidc.go:156`):
1. State compared with `subtle.ConstantTimeCompare` (`auth/oidc.go:168`).
2. Pre-login keys read into locals, then removed immediately — every subsequent
   path (failure or success) leaves no lingering state (`auth/oidc.go:189-192`).
3. Code exchange with `oauth2.VerifierOption(verifier)` (`auth/oidc.go:204`).
4. ID token extracted from `token.Extra("id_token")`, verified, nonce compared
   constant-time (`auth/oidc.go:211-229`).
5. Claims read: `email`, `name`, `preferred_username` (name falls back to
   preferred_username).
6. `sm.RenewToken(ctx)` — session-fixation prevention (`auth/oidc.go:247`).
7. `SessionData` stored (tokens + expiry + subject/email/name).
8. `members.Upsert` with `UserID: subjectUUID(subject)` (`auth/oidc.go:266-275`).
9. 302 to the validated `return_to`.

**RequireAuth** (`auth/oidc.go:331`):
- Missing/unparseable session ⇒ 401 RFC 9457 problem (with diagnostic logging
  of whether the session cookie was present at all).
- `time.Until(data.Expiry) < refreshThreshold` (30s, `auth/oidc.go:28`) triggers
  `refresh` (`auth/oidc.go:389`): a `TokenSource` seeded with the refresh token
  and an already-expired expiry forces a refresh; rotated refresh tokens and a
  returned new ID token are persisted back into the session.
- `isInvalidGrant` (`auth/oidc.go:422`) — `oauth2.RetrieveError` with
  `invalid_grant`, or a session with no refresh token at all
  (`errNoRefreshToken`) ⇒ session destroyed, 401 "session expired". Any other
  refresh failure is transient ⇒ 503, session kept.
- Success: `User{ID: subjectUUID(subject), Name, Email}` injected into context.

**Logout** (`auth/oidc.go:292`): reads the ID token for the hint, destroys the
session (clears cookie), then — when the provider advertises an
`end_session_endpoint` — redirects there with `id_token_hint`, `client_id`, and
`post_logout_redirect_uri` (config); otherwise redirects home.

**Subject → UUID**: `subjectUUID` (`auth/oidc.go:435`) is
`uuid.NewSHA1(subjectNamespace, subject)` with a fixed public namespace
(`6f9619ff-8b86-d011-b42d-00c04fc964ff`, `auth/oidc.go:33`) — the same
derivation migration 000007 replicates in SQL via `uuid_generate_v5`.

### 3. Password mode (`auth/password.go`)

- `Login` handles POST (credentials) and GET (plain redirect home — the SPA owns
  the form). Body self-limited to 4 KiB via `http.MaxBytesReader`
  (`auth/password.go:25,95`) because `/api/auth/*` sits outside the `/api/v1`
  MaxBody middleware; JSON decoded with `DisallowUnknownFields`.
- **Username-enumeration resistance**: unknown user and wrong password both
  return the identical 401 problem, and the unknown-user path still runs a
  bcrypt comparison against a lazily-computed `dummyHash()`
  (`auth/password.go:33-49,115`) so timing matches the found-user path. A real
  DB failure returns 503, not 401 (`auth/password.go:107-111`).
- On success: `sm.RenewToken` (fixation), then the **same `SessionData` shape as
  OIDC with no tokens** — `Subject` is the user's UUID string
  (`auth/password.go:136-140`) — then `members.Upsert`, then 204.
- `RequireAuth` (`auth/password.go:182`): loads the session, `uuid.Parse`s the
  subject (unparseable ⇒ 401), injects `User{ID: parsed, Name, Email}`. No
  refresh path — the scs session lifetime governs expiry.
- `Callback` 404s; `Logout` destroys the session and redirects home.

Supporting domain (`users/users.go`): password policy 8 runes minimum / 72
bytes maximum (bcrypt's truncation limit, rejected instead of truncated);
`HashPassword` (bcrypt default cost), `VerifyPassword`
(`bcrypt.CompareHashAndPassword`, returns plain bool). `Repository.GetByUsername`
returns the hash *separately* from the domain `User` so it never rides on the
struct. Provisioning is the `useradd` CLI only (`cmd/useradd.go:28`):
interactive no-echo double prompt or `--password-stdin`; plaintext never stored
or logged.

### 4. Dev-auth mode (`auth/dev.go`)

- `DevUser` = fixed UUID `00000000-0000-0000-0000-000000000001` (matches the M1
  middleware user so pre-M2 data stays attributed).
- `RequireAuth` unconditionally injects `DevUser`; `Login`/`Logout` redirect
  home; `Callback` 404s.
- Since the dev user never logs in, `cmd/serve.go:152-158` upserts
  `auth.DevMember()` into `members` once at startup (best-effort — a DB failure
  only warns).

### 5. Sessions (`auth/session.go`, `cmd/serve.go`)

- `SessionData` (`auth/session.go:33`): access/refresh/ID tokens, expiry,
  subject, email, name — JSON under the single scs key `auth_session`. Password
  and dev sessions reuse the shape with empty token fields.
- Store decision is the pure function `sessionStore(oidcEnabled, dbReachable)`
  (`cmd/serve.go:70`), unit-tested in `cmd/serve_test.go:24`:
  - OIDC + DB down ⇒ **fatal** ("sessions require a reachable database in OIDC
    mode") — evaluated *before* OIDC discovery so an unreachable DB never
    contacts the issuer (`cmd/serve.go:124`).
  - Otherwise Postgres when reachable; in-memory fallback only in
    non-OIDC modes (process-local, lost on restart).
- `newSessionManager` (`cmd/serve.go:86`): `postgresstore.New(conn)` or scs
  default memstore; `Lifetime` from config (default 168h);
  `HttpOnly=true`, `Secure` from config (default true), `SameSite=Lax`.
- OIDC provider discovery in `auth.New` is bounded by a 15s timeout
  (`cmd/serve.go:142`) so a hung issuer cannot block startup indefinitely.
- The `sessions` table (migration 000003): `token text PK, data bytea,
  expiry timestamptz` + expiry index — scs's postgresstore schema.

### 6. HTTP wiring (`ports/rest/server.go`)

- `/api/auth/*` endpoints (`ports/rest/server.go:36-48`) are hand-written,
  mounted **outside** `/api/v1` and its OpenAPI request-validation middleware
  (they are browser-facing redirect endpoints, not JSON API resources), wrapped
  in their own `Recover`:
  - `GET /api/auth/login` (OIDC redirect / password+dev redirect home)
  - `POST /api/auth/login` (password credentials)
  - `GET /api/auth/callback`, `POST /api/auth/logout`
  - `GET /api/auth/config` — public, session-free: `{"mode":"oidc|password|dev"}`
    from `config.C.Mode()` so the SPA picks its login UI
    (`ports/rest/server.go:139`).
- `/api/v1` middleware order (slice order, last = outermost;
  `ports/rest/server.go:93-98`): metrics → logging → OpenAPI validator →
  `publicHealth(RequireAuth)` → handler. `RequireAuth` innermost, so metrics and
  logging observe auth failures.
- `publicHealth` (`ports/rest/server.go:151`) exempts exactly
  `GET /api/v1/health` from RequireAuth for probes; everything else — including
  other methods on that path — stays guarded.
- The SSE endpoint `/api/v1/events` is registered by hand with
  `apiRouter.With(authr.RequireAuth)` (`ports/rest/server.go:81`), bypassing the
  validator (streams can't be modeled by oapi-codegen) but never public.
- **Selective session middleware** (`ports/rest/server.go:115-134`): only
  `/api/auth/*` (minus `/api/auth/config`) and `/api/v1*` run through
  `sm.LoadAndSave`. Static assets/docs bypass it — scs's response writer on the
  embedded file server churned `Set-Cookie`/`Vary` per asset and raced on the
  header map under the service worker's parallel precache fetches.

### 7. Configuration (`config/`)

- `AuthConfig` = `OIDC` + `Password` + `Session` (`config/config.go:44`).
- `IsOIDCEnabled` requires issuer **and** client_id **and** client_secret
  (`config/config.go:77`); `Mode()` resolves the precedence chain
  (`config/config.go:103`).
- Validation (`config/validation.go:49-59`): setting an issuer makes client_id,
  client_secret, and redirect_url required.
- Defaults (`config/defaults.go:38-50`): all OIDC fields empty (⇒ dev-auth),
  `auth.password.enabled=false`, session lifetime 168h, `cookie_secure=true`.
- Env override via `SPLITKAUF_` prefix (e.g. `SPLITKAUF_AUTH_PASSWORD_ENABLED`,
  `SPLITKAUF_AUTH_OIDC_ISSUER`).

### 8. Membership and attribution (`members/`, migration 000007)

- `members.Member` keyed by `Subject` (text, PK): dev/password subjects are the
  user UUID string, OIDC subjects the raw provider subject. `UserID uuid` is the
  stable auth UUID — what the API reports and what the attribution columns
  store (`members/members.go:20-32`).
- JIT population: upserted on every OIDC callback and password login, plus the
  dev startup upsert. There is no in-app membership administration.
- Migration 000007 added `user_id` with a backfill that mirrors
  `auth.subjectUUID` in SQL (`uuid_generate_v5` with the same namespace) and
  deliberately no FK from attribution columns to members — a missing member row
  resolves the display name to NULL instead of failing writes.

### 9. Frontend (`frontend/src/`)

- Auth gate: `App.tsx` renders signed-in UI when `useMe()` (GET `/api/v1/me`)
  succeeds; a 401 (detected by `isUnauthorized`, `api.ts:63`) shows the
  signed-out view. `getAuthConfig()` decides between the `LoginForm` (password
  mode) and the OIDC redirect button; cached with `staleTime: Infinity`, no
  retry, falling back to the OIDC button when unresolved.
- `login()` (`api.ts:86`) is a top-level navigation (not fetch) so the browser
  follows the 302 to the IdP; `return_to` defaults to the current path+query.
- `passwordLogin()` (`api.ts:97`) POSTs credentials; on success `LoginForm`
  invalidates the `['me']` query — no full-page reload.
- `logout()` (`api.ts:118`) clears the React Query cache *and awaits* the
  IndexedDB persister removal (shared-device privacy, US-O.2), then submits a
  hidden form POST to `/api/auth/logout` — a real navigation so the browser
  follows the redirect to the IdP's RP-initiated logout.
- No tokens ever reach the frontend; all state hangs off the HttpOnly cookie.

### 10. Tests

- `auth/auth_test.go`: mode selection (dev vs OIDC with a mocked discovery
  server), 16 open-redirect cases for `safeReturnTo`, token randomness/size,
  context round-trip, `SessionData` JSON round-trip, `subjectUUID` stability,
  dev-auth behavior.
- `auth/password_test.go`: full login flow over a real scs manager, the
  indistinguishable-401 property, GET redirect, malformed/oversized body.
- `config/auth_config_test.go`: OIDC validation, `IsOIDCEnabled` conditions,
  mode precedence.
- `cmd/serve_test.go:24`: `sessionStore` decision matrix.
- `ports/rest/server_test.go`: static assets bypass session middleware; public
  `/api/auth/config`.

## Code References

- `auth/auth.go:40` — `Authenticator` interface
- `auth/auth.go:80` — `New`: OIDC → password → dev selection
- `auth/oidc.go:110` — OIDC `Login` (PKCE + state + nonce)
- `auth/oidc.go:156` — OIDC `Callback` (exchange, verify, session, upsert)
- `auth/oidc.go:331` — OIDC `RequireAuth` (401 / proactive refresh / 503)
- `auth/oidc.go:389` — `refresh` (rotation-aware)
- `auth/oidc.go:435` — `subjectUUID` (UUIDv5 of OIDC subject)
- `auth/password.go:86` — password `Login` (timing-equalized 401)
- `auth/password.go:182` — password `RequireAuth`
- `auth/dev.go:62` — dev `RequireAuth` (fixed user)
- `auth/session.go:33` — `SessionData`
- `auth/redirect.go:22` — `safeReturnTo`
- `config/config.go:77-112` — `IsOIDCEnabled`/`IsPasswordEnabled`/`Mode`
- `cmd/serve.go:70` — `sessionStore` fail-fast policy
- `cmd/serve.go:86` — `newSessionManager` (postgresstore / memstore)
- `cmd/useradd.go:28` — account provisioning CLI
- `ports/rest/server.go:36-48` — `/api/auth/*` routes
- `ports/rest/server.go:93-98` — middleware ordering with `RequireAuth` innermost
- `ports/rest/server.go:115-134` — selective `LoadAndSave`
- `ports/rest/server.go:151` — `publicHealth`
- `users/users.go:82-110` — password policy + bcrypt helpers
- `members/members.go:20-43` — `Member` + repository port
- `database/migrations/000003_auth.up.sql` — `sessions` + `members`
- `database/migrations/000006_password_users.up.sql` — `users`
- `database/migrations/000007_attribution.up.sql` — `members.user_id` + backfill
- `frontend/src/api.ts:63-128` — `isUnauthorized`, `getAuthConfig`, `login`,
  `passwordLogin`, `logout`
- `frontend/src/LoginForm.tsx:14` — password login form

## Architecture Documentation

- **BFF pattern**: browser never sees tokens; opaque HttpOnly cookie only.
  Matches the design research (`2026-07-21-oidc-go-pwa-integration.md`) with the
  Redis recommendation replaced by Postgres (`scs/postgresstore`) to keep the
  deployment at one external dependency.
- **One session shape, three modes**: password and dev reuse the OIDC
  `SessionData`, so `RequireAuth`, logout, and durability are uniform.
- **Provider-agnostic**: `coreos/go-oidc` + `x/oauth2`, no IdP-specific SDK;
  Zitadel vs Keycloak is a deployment decision (architecture.md §9).
- **AuthZ split**: the IdP (or the users table) only *authenticates*; splitkauf
  owns identity mapping (`members`) and attribution. No roles from IdP claims.
- **Errors**: every auth failure is an RFC 9457 problem via
  `ports/rest/problem` (401 unauthorized, 503 unavailable, 400 validation).
- Related docs: `docs/architecture.md` §6, plans
  `2026-07-21-m2-oidc-auth.md`, `2026-08-01-m7-username-password-auth.md`,
  `2026-08-01-m5-hardening-fixes.md` (US-Q.6), user stories US-A.1–A.7, US-Q.6.

## Open Questions

- **Backchannel logout** — deliberately undecided (architecture.md §9); a
  server-side IdP-initiated logout does not invalidate splitkauf sessions today.
- **Multi-tenancy** — out of scope for the single-instance model.
- **CSP / security headers middleware** — listed 🔜 in architecture.md §7; not
  implemented.
- The frontend falls back to the OIDC redirect button when
  `/api/auth/config` fails to resolve; behavior in dev mode signed-out state is
  effectively unreachable since `getMe` always succeeds there.
