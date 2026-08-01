# US-A.6 — Log in with username and password

**Milestone:** M7
**Depends on:** US-A.3 (durable sessions), US-A.7 (operator-provisioned accounts)

**As a** member of a self-hosted instance without an OIDC provider, **I want** to
log in with a username and password, **so that** the group can use the app with
real per-person accounts without standing up an identity provider.

## Acceptance criteria

- A dedicated password auth mode, enabled by `SPLITKAUF_AUTH_PASSWORD_ENABLED`.
  Selection precedence is unchanged: OIDC when configured, else password when
  enabled, else dev-auth.
- The login screen renders a username + password form (not the OIDC redirect
  button) when the server reports password mode; the frontend discovers the
  mode from a public endpoint.
- Credentials are submitted to the backend; on success the browser receives
  only the existing HttpOnly, server-side session cookie — the password is
  never stored client-side and no token reaches the frontend.
- Passwords are verified against a bcrypt hash in constant time; a wrong
  username and a wrong password are indistinguishable (same 401, no user
  enumeration).
- A signed-in password user is a first-class member (upserted like an OIDC
  account) and can log out (session destroyed, cookie cleared).
- Inherits the UX §6 guardrails for the form (≥44px targets, ≥16px inputs, AA
  contrast in both themes, labelled fields, no blocking spinner).
