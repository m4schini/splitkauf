---
date: 2026-08-21T19:20:55Z
git_commit: df6a1d752a09fe8c2f43a7ae0b6c3d7f06bcd001
branch: main
topic: "Align OIDC authentication with local username/password login"
tags: [plan, auth, oidc, password, sessions]
status: ready
---

# PLAN: Align OIDC Authentication with Password Login

OIDC mode should behave exactly like password mode after sign-in: the IdP only
*authenticates* (it replaces the username/password input), and the resulting
server-side session is governed solely by the scs session lifetime — no stored
OAuth access/refresh tokens, no transparent token refresh, no IdP contact on
API requests. The only retained token is the ID token, kept exclusively as the
`id_token_hint` for RP-initiated logout (shared-device account switching,
US-O.2).

Based on research: `docs/agents/research/2026-08-21-oidc-authn-implementation.md`.

## Acceptance Criteria

- OIDC session after callback contains no access token, no refresh token, and
  no token expiry; only `UserID`, `IDToken` (logout hint), `Subject`, `Email`,
  `Name`.
- `RequireAuth` is one shared session-based implementation used by both OIDC
  and password modes: load session, read `UserID`, inject `auth.User`. It never
  contacts the IdP — no refresh path, no 503 path.
- Session lifetime in OIDC mode is governed solely by the scs lifetime
  (default 168h), same as password mode.
- The `offline_access` scope is no longer requested.
- RP-initiated logout still works (`id_token_hint` from the stored `IDToken`).
- Existing sessions without `UserID` yield a 401 (one-time re-login after
  deploy); they must never inject a zero-UUID user.
- All auth tests pass; `docs/architecture.md` §6 reflects the new model.

## Technical Key Decisions and Tradeoffs

1. **OIDC = credential check only:** drop token storage and the refresh
   machinery entirely.
   - Why: exact alignment with password login; the IdP's job ends at
     authenticating the user.
   - Impact: delete `refresh`, `refreshThreshold`, `errNoRefreshToken`,
     `isInvalidGrant`. IdP-side revocation only blocks the *next* login, not a
     live session — identical to password mode, where a deleted account's
     session also lives until scs expiry.
2. **Keep `IDToken` for RP-initiated logout:** the single retained token.
   - Why: without the hint, logout leaves the IdP SSO session alive and the
     next login silently re-signs the same account — breaks shared-device
     account switching (US-O.2).
   - Impact: `Logout` stays as-is; `SessionData` keeps the `id_token` field.
3. **Resolved `UserID` stored in `SessionData`, shared `RequireAuth`:** each
   mode resolves the user UUID once at login (OIDC: `subjectUUID(subject)`;
   password: the account UUID) and stores it.
   - Why: a single, identical `RequireAuth` code path for both modes.
   - Impact: `subjectUUID` derivation happens only in the callback; sessions
     created before this change lack `user_id` and 401 once (users re-log-in
     after deploy — accepted, no fallback shim).
4. **No frontend changes:** the SPA already switches between the password form
   and the OIDC redirect button via `/api/auth/config`.
5. **No migrations, no config changes:** the `sessions` table stores opaque
   JSON; `sessionStore` fail-fast policy (OIDC requires reachable DB) stays.

## Current State

```
                      OIDC mode                    Password mode
Login input           IdP redirect (code+PKCE)     username/password POST
Session established   RenewToken + SessionData     RenewToken + SessionData
SessionData content   access+refresh+ID tokens     subject/email/name only
                      + expiry + claims
RequireAuth           load session,                load session,
                      refresh when expiry <30s,    uuid.Parse(subject),
                      503 transient / 401          inject user
                      invalid_grant,
                      subjectUUID(subject)
Session lifetime      refresh-token lifetime       scs lifetime (168h)
Logout                destroy + RP-initiated       destroy + redirect home
                      logout (id_token_hint)
Scopes                openid profile email         n/a
                      offline_access
```

Key code (all verified against `df6a1d7`):

- `auth/session.go:33` — `SessionData` with `AccessToken`, `RefreshToken`,
  `IDToken`, `Expiry`, `Subject`, `Email`, `Name`.
- `auth/oidc.go:110` — `Login` (state/nonce/PKCE, unchanged by this plan).
- `auth/oidc.go:156` — `Callback` stores full token set (`auth/oidc.go:252-260`).
- `auth/oidc.go:331` — `RequireAuth` with refresh branch (`auth/oidc.go:351-375`).
- `auth/oidc.go:389` — `refresh`; `auth/oidc.go:422` — `isInvalidGrant`;
  `auth/oidc.go:28` — `refreshThreshold`; `auth/oidc.go:37` — `errNoRefreshToken`.
- `auth/oidc.go:69` — scopes include `oidc.ScopeOfflineAccess`.
- `auth/oidc.go:443` — `hasSessionCookie` (method on `oidcAuthenticator`).
- `auth/password.go:136-140` — password login stores token-less `SessionData`
  with `Subject = user.ID.String()`.
- `auth/password.go:182` — password `RequireAuth` parses `Subject` as UUID.
- `auth/oidc.go:292` — `Logout` reads `data.IDToken` for the hint (kept).

## Desired End State

```
                      OIDC mode                    Password mode
Login input           IdP redirect (code+PKCE)     username/password POST
Session established   RenewToken + SessionData     RenewToken + SessionData
SessionData content   UserID + IDToken(hint)       UserID + subject/email/name
                      + subject/email/name
RequireAuth           ── one shared requireSession middleware ──
                      load session, UserID == Nil ⇒ 401, inject user
Session lifetime      scs lifetime (168h)          scs lifetime (168h)
Logout                destroy + RP-initiated       destroy + redirect home
                      logout (id_token_hint)
Scopes                openid profile email         n/a
```

```go
// auth/session.go — new shape
type SessionData struct {
    UserID  uuid.UUID `json:"user_id"`
    IDToken string    `json:"id_token"` // retained solely as the RP-initiated-logout hint
    Subject string    `json:"subject"`  // diagnostics/logging only
    Email   string    `json:"email"`
    Name    string    `json:"name"`
}
```

Old sessions (JSON without `user_id`) unmarshal with `UserID == uuid.Nil` and
are rejected with the standard "no active session" 401.

## Abstractions and Code Reuse

Reuse: `SessionData` + `getSessionData`/`putSessionData`, `problem` responses,
`WithUser`/`UserFrom`, `safeReturnTo`, `randomToken`, members upsert — all
unchanged in shape or call pattern. New abstraction: one package-private shared
middleware constructor replacing both per-mode `RequireAuth` bodies.

- `auth/`
  - `session.go` — `SessionData` reshaped (add `UserID`; drop `AccessToken`,
    `RefreshToken`, `Expiry`); new shared middleware + cookie probe:
    - `requireSession(sm *scs.SessionManager, logger *zap.Logger) func(http.Handler) http.Handler`
      — load session, `UserID == uuid.Nil` ⇒ 401 problem, else inject
      `User{ID: data.UserID, Name: data.Name, Email: data.Email}`; keeps the
      diagnostic `incoming_session_cookie` logging.
    - `hasSessionCookie(sm, r)` — moved from `oidcAuthenticator` method to a
      package-level helper so `requireSession` can use it.
  - `oidc.go` —
    - `Callback` — store `SessionData{UserID: subjectUUID(subject), IDToken:
      rawIDToken, Subject, Email, Name}`; drop token-expiry log fields.
    - `RequireAuth` — delegate to `requireSession`.
    - Delete: `refresh`, `isInvalidGrant`, `refreshThreshold`,
      `errNoRefreshToken`, refresh branch in `RequireAuth`.
    - Scopes — remove `oidc.ScopeOfflineAccess`.
    - `Logout` — unchanged (still reads `data.IDToken`).
    - Struct/package comments — remove refresh claims.
  - `password.go` —
    - `Login` — add `UserID: user.ID` to the stored `SessionData`.
    - `RequireAuth` — delegate to `requireSession`; delete the `uuid.Parse`
      path.
  - `auth.go` — package/interface doc comments: drop "transparent token
    refresh".
  - `auth_test.go`, `password_test.go`, new `session_test.go` — see phases.
- `docs/architecture.md` — §6 auth description: session model, no refresh,
  scopes (including the §6 mermaid sequence diagram and the operational note
  on access-token lifetimes).
- `deploy/README.md` — IdP setup guidance: `offline_access` recommendation and
  server-side-refresh statement are now wrong.

## Logging & Observability

- `requireSession` keeps the OIDC-mode diagnostic on 401 (now for both modes):
  `requireauth: no active session, returning 401` with `path` and
  `incoming_session_cookie`; plus a distinct reason when the session parses but
  `UserID` is nil (pre-migration session): `requireauth: session has no user id
  (pre-alignment session), returning 401`.
- `callback: login complete, session established` drops
  `access_token_expiry`/`has_refresh_token`, keeps `subject`, `email`, `name`,
  `return_to`.
- Deleted with the refresh path: `requireauth: access token near expiry,
  refreshing`, `token refresh failed`, `requireauth: refresh token invalid...`.

## Implementation

### Phase 1: Session shape + shared RequireAuth

Dependencies: None.

Reshape `SessionData` around `UserID`, introduce the shared `requireSession`
middleware, adopt it in both modes, and delete the OIDC refresh machinery.
After this phase the whole acceptance behavior is in place except the
`offline_access` scope removal and doc updates.

**Tasks**:
- [x] `auth/session.go`: reshape `SessionData` — add `UserID uuid.UUID
  `json:"user_id"``, remove `AccessToken`, `RefreshToken`, `Expiry`; keep
  `IDToken`, `Subject`, `Email`, `Name`; update struct comment (ID token kept
  only as logout hint; `Subject` diagnostics-only).
- [x] `auth/session.go`: add `requireSession(sm, logger)` returning the shared
  middleware: `getSessionData` miss ⇒ 401 "no active session";
  `data.UserID == uuid.Nil` ⇒ 401 "no active session" (log pre-alignment
  reason); else inject `User{ID: data.UserID, Name: data.Name, Email:
  data.Email}` via `WithUser`.
- [x] `auth/session.go` (or `auth/auth.go`): move `hasSessionCookie` off
  `oidcAuthenticator` to a package-level `hasSessionCookie(sm
  *scs.SessionManager, r *http.Request) bool`; update the call sites in
  `Login`/`Callback` logging.
- [x] `auth/oidc.go`: `Callback` stores the new shape — `SessionData{UserID:
  subjectUUID(idToken.Subject), IDToken: rawIDToken, Subject: idToken.Subject,
  Email: claims.Email, Name: name}`; drop `access_token_expiry` and
  `has_refresh_token` from the completion log.
- [x] `auth/oidc.go`: `RequireAuth` becomes `return requireSession(a.sm,
  a.logger)(next)` (or store the middleware at construction); delete the
  refresh branch.
- [x] `auth/oidc.go`: delete `refresh`, `isInvalidGrant`, `refreshThreshold`,
  `errNoRefreshToken`; update `oidcAuthenticator` and `RequireAuth` doc
  comments (no refresh, no 503).
- [x] Drop imports that become unused after the deletions: `errors` and `time`
  in `auth/oidc.go`; `time` in `auth/session.go` (only `Expiry` used it);
  `github.com/google/uuid` in `auth/password.go` (only the deleted
  `uuid.Parse` branch used it — `auth/session.go` gains the `uuid` import for
  `UserID` instead).
- [x] `auth/password.go`: `Login` stores `SessionData{UserID: user.ID, Subject:
  user.ID.String(), Email: user.Email, Name: user.Name}`.
- [x] `auth/password.go`: `RequireAuth` delegates to `requireSession`; delete
  the `uuid.Parse` branch and its inline comment, and rewrite the
  `RequireAuth` doc comment (`auth/password.go:178-181`) — it currently says
  "a subject that is not a parseable user id" and "There is no token-refresh
  path", both stale after this change (the latter also trips Phase 2's
  `refresh` grep).
- [x] `auth/auth.go`: update package and `Authenticator` doc comments — remove
  "transparent token refresh", describe the uniform session model.
- [x] `auth/session_test.go` (new): table test for `requireSession` over a real
  scs manager — no session ⇒ 401 problem; stored JSON *without* `user_id`
  (pre-alignment session, raw `sm.Put` of hand-built JSON) ⇒ 401; valid
  `SessionData` with `UserID` ⇒ next handler runs and `UserFrom` returns the
  expected user.
- [x] `auth/auth_test.go`: update the `SessionData` JSON round-trip test
  (`TestSessionDataRoundTrip`, `auth/auth_test.go:180`) to the new shape;
  fix/remove any test referencing deleted symbols.
- [x] `auth/password_test.go`: extend the successful-login test to assert the
  stored session's `UserID` equals the account UUID and that a subsequent
  `RequireAuth`-wrapped request sees that user.

**Automated Verification**:
- [x] `go build ./...` passes.
- [x] `go vet ./...` passes.
- [x] `go test ./...` passes.
- [x] `grep -rn "refreshThreshold\|errNoRefreshToken\|isInvalidGrant" --include='*.go' .` returns nothing.
- [x] `grep -n "AccessToken\|RefreshToken" auth/*.go` returns nothing.

### Phase 2: OIDC flow trim + documentation

Dependencies: Phase 1.

Stop requesting `offline_access`, finish comment/log cleanup, and align the
architecture documentation.

**Tasks**:
- [ ] `auth/oidc.go`: remove `oidc.ScopeOfflineAccess` from
  `oauth2.Config.Scopes` (leaving `openid profile email`); update the
  construction comment.
- [ ] `auth/oidc.go`: sweep remaining comments/log messages for stale refresh
  or token-storage wording (`newOIDC` startup log, `Callback` doc comment,
  `Logout` doc comment).
- [ ] `auth/password.go`: update the `passwordAuthenticator` doc comment —
  "same scs session shape" no longer needs the "minus the OAuth tokens"
  qualifier.
- [ ] `docs/architecture.md` §6: describe the aligned model — IdP
  authenticates only; session = `UserID` + ID-token logout hint + claims; scs
  lifetime governs expiry in all modes; scopes `openid profile email`; note
  that IdP revocation takes effect at next login. Explicitly fix the §6
  mermaid sequence diagram ("ID/access/refresh tokens", "store tokens in
  Postgres session") and delete/rewrite the trailing "Operational note"
  recommending short access-token lifetimes (now irrelevant).
- [ ] `deploy/README.md:175-186`: remove the `offline_access` scope
  recommendation and the "Splitkauf refreshes tokens server-side" statement
  plus the short-access-token-lifetime rationale; state that session duration
  is governed by `auth.session.lifetime`.
- [ ] `auth/auth_test.go`: assert the configured scopes no longer contain
  `offline_access` — extend the mocked-discovery construction test
  (`TestNewSelectsOIDCWhenEnabled`, `auth/auth_test.go:111`; same package, so
  `oauth2Config.Scopes` is directly assertable).

**Automated Verification**:
- [ ] `go build ./...` passes.
- [ ] `go test ./...` passes.
- [ ] `grep -n "offline_access\|ScopeOfflineAccess" auth/*.go` returns nothing.
- [ ] `grep -rn "refresh" auth/*.go` returns nothing (case-sensitive, so
  `RenewToken`/capitalized identifiers never match; any hit is a stale
  lowercase comment mention).
- [ ] `grep -n "offline_access\|refreshes tokens" docs/architecture.md deploy/README.md` returns nothing.

**Manual Verification**:
- [ ] Against a real IdP (Zitadel or Keycloak dev instance): sign in via the
  OIDC button, use the app past the IdP's access-token lifetime (typically
  5–15 min) and confirm API requests keep working with no refresh calls in the
  IdP logs.
- [ ] Log out and confirm the browser lands on the IdP's logged-out state
  (RP-initiated logout still works); a second login can pick a different
  account.
- [ ] A session established before the deploy 401s once and re-login succeeds.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

## References

- Research: `docs/agents/research/2026-08-21-oidc-authn-implementation.md`
- Design research: `docs/agents/research/2026-07-21-oidc-go-pwa-integration.md`
- Prior plans: `docs/agents/plans/2026-07-21-m2-oidc-auth.md`,
  `docs/agents/plans/2026-08-01-m7-username-password-auth.md`
- `docs/architecture.md` §6 (auth), §9 (backchannel logout — still open, this
  plan does not change that stance)
- User stories: US-A.1–A.7, US-O.2 (shared-device logout), US-Q.6 (fail-fast
  sessions)
