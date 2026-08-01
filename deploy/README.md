# Deployment (Podman Quadlet)

This directory contains [Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit files that run Splitkauf and its PostgreSQL database as rootless
systemd-managed containers on a single server.

## Install

1. Copy the unit files into the systemd Quadlet directory:

   ```sh
   sudo mkdir -p /etc/containers/systemd
   sudo cp deploy/quadlet/*.network deploy/quadlet/*.volume deploy/quadlet/*.container \
       /etc/containers/systemd/
   ```

2. Create the environment files referenced by the units. These are kept
   outside of the unit files (and outside of git) so secrets never end up in
   version control:

   ```sh
   sudo mkdir -p /etc/splitkauf
   sudo tee /etc/splitkauf/db.env >/dev/null <<'EOF'
   POSTGRES_DB=splitkauf
   POSTGRES_USER=splitkauf
   POSTGRES_PASSWORD=change-me
   EOF

   sudo tee /etc/splitkauf/splitkauf.env >/dev/null <<'EOF'
   SPLITKAUF_DATABASE_HOST=splitkauf-db
   SPLITKAUF_DATABASE_PORT=5432
   SPLITKAUF_DATABASE_USER=splitkauf
   SPLITKAUF_DATABASE_PASSWORD=change-me
   SPLITKAUF_DATABASE_NAME=splitkauf
   SPLITKAUF_DATABASE_SSL_MODE=disable

   # OIDC login (Authentik). Leave all SPLITKAUF_AUTH_OIDC_* vars unset to run
   # with dev-auth instead — see "Authentication" below.
   SPLITKAUF_APP_BASE_URL=https://splitkauf.example.com
   SPLITKAUF_AUTH_OIDC_ISSUER=https://authentik.example.com/application/o/splitkauf/
   SPLITKAUF_AUTH_OIDC_CLIENT_ID=change-me
   SPLITKAUF_AUTH_OIDC_CLIENT_SECRET=change-me
   SPLITKAUF_AUTH_OIDC_REDIRECT_URL=https://splitkauf.example.com/api/auth/callback
   SPLITKAUF_AUTH_OIDC_POST_LOGOUT_REDIRECT_URL=https://splitkauf.example.com/
   SPLITKAUF_AUTH_SESSION_COOKIE_SECURE=true
   EOF

   sudo chmod 600 /etc/splitkauf/db.env /etc/splitkauf/splitkauf.env
   ```

   The two passwords must match.

   The Quadlet unit already loads `splitkauf.env` via
   `EnvironmentFile=/etc/splitkauf/splitkauf.env`, so adding the OIDC
   variables above requires no change to the unit file itself — just
   `systemctl restart splitkauf.service` after editing the env file.

3. Reload systemd so it picks up the generated `.service` units from the
   Quadlet files, then start the app (which pulls in the database via
   `Requires=`/`After=`):

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl start splitkauf.service
   sudo systemctl status splitkauf.service splitkauf-migrate.service splitkauf-db.service
   ```

   Enable both on boot with `sudo systemctl enable splitkauf.service
   splitkauf-db.service` (or rely on `[Install] WantedBy=default.target`
   plus `daemon-reload`, which already registers them).

## Authentication

Splitkauf selects its auth mode automatically from config — there is no
explicit mode toggle:

- If `SPLITKAUF_AUTH_OIDC_ISSUER`, `SPLITKAUF_AUTH_OIDC_CLIENT_ID`, and
  `SPLITKAUF_AUTH_OIDC_CLIENT_SECRET` are **all** set, the backend runs the
  OIDC BFF (backend-for-frontend) flow against that provider.
- If any of those three are unset, the backend falls back to dev-auth (a
  single hardcoded dev user, the same as local development). This is only
  suitable for local/test deployments — do not leave OIDC unset on a
  production host.

`SPLITKAUF_AUTH_OIDC_REDIRECT_URL` and
`SPLITKAUF_AUTH_OIDC_POST_LOGOUT_REDIRECT_URL` are required for the OIDC flow
to complete correctly, and `SPLITKAUF_APP_BASE_URL` should be set to the
externally reachable origin of the app (scheme + host) whenever OIDC is
enabled.

`SPLITKAUF_AUTH_SESSION_COOKIE_SECURE` controls the `Secure` flag on the
server-side session cookie. Keep it `true` in production/behind HTTPS
(the default assumed above); only set it to `false` for plain-HTTP local
testing, never on a real deployment.

### Session store and database availability at startup

In OIDC mode, sessions must persist to Postgres — an in-memory fallback would
silently lose login state (CSRF/PKCE state, and every member's session) on
any restart or brief database blip, and logins cannot complete without the
members table anyway. Because of this, `serve` fails fast at startup when
the database is unreachable and OIDC is configured: it logs
`sessions require a reachable database in OIDC mode` and exits nonzero
*before* contacting the OIDC issuer or binding the HTTP listener.

The Quadlet unit sets `Restart=always`, so systemd/podman simply restarts the
service on this exit; once the database (`splitkauf-db.service`, started
first via `Requires=`/`After=`) becomes reachable, the next restart succeeds
normally. No manual intervention is needed for a database that is merely
starting up slowly — only for one that stays down.

In dev-auth mode (no OIDC configured), the existing in-memory session-store
fallback is kept: the process still starts with the database down so local
development keeps working, with sessions held in process memory (lost on
restart) until the database comes back.

### Setting up the Authentik provider

Splitkauf's OIDC flow targets [Authentik](https://goauthentik.io/) as a
standard OIDC provider (any spec-compliant provider works, but Authentik is
the one this deployment path is tested against):

1. In Authentik, create an **OAuth2/OIDC Provider**:
   - Client type: **Confidential**.
   - Scopes: `openid profile email` (add `offline_access` too if you want
     the provider to issue refresh tokens; the backend refreshes the
     session server-side, so this is optional but recommended).
   - Redirect URI: `<SPLITKAUF_APP_BASE_URL>/api/auth/callback` (e.g.
     `https://splitkauf.example.com/api/auth/callback`), matching
     `SPLITKAUF_AUTH_OIDC_REDIRECT_URL` exactly.
   - Set the access token lifetime **short (5–15 minutes)**. Splitkauf
     refreshes tokens server-side using the refresh token, so there's no
     benefit to a long-lived access token, and a short one limits exposure
     if it ever leaks.
2. Create an **Application** bound to that provider, with its launch/logout
   URL set to `SPLITKAUF_AUTH_OIDC_POST_LOGOUT_REDIRECT_URL` (e.g.
   `https://splitkauf.example.com/`).
3. Copy the provider's **Client ID** and **Client Secret** into
   `SPLITKAUF_AUTH_OIDC_CLIENT_ID` / `SPLITKAUF_AUTH_OIDC_CLIENT_SECRET` in
   `/etc/splitkauf/splitkauf.env`.
4. Copy the provider's **issuer URL** (OpenID Configuration issuer, typically
   `https://<authentik-host>/application/o/<slug>/`) into
   `SPLITKAUF_AUTH_OIDC_ISSUER`.
5. Restart `splitkauf.service` to pick up the new environment.

Local development (`docker-compose.yaml`) intentionally keeps dev-auth and
does not set any OIDC variables — no Authentik instance is required to run
the app locally.

## Running migrations

Migrations run automatically as a one-shot unit, `splitkauf-migrate.service`
(from `splitkauf-migrate.container`): it applies every embedded up-migration
with the same application image and exits. `splitkauf.service`
`Requires=`/`After=` it, so `serve` only starts once the schema is current,
and the migrate unit itself waits for the database to be **healthy** —
`splitkauf-db` sets `Notify=healthy`, which requires **podman >= 5.0**.

So on a supported host, `systemctl start splitkauf.service` brings up the whole
stack in order (database → migrations → app) with no manual step.

On podman < 5.0 the health-gated ordering is unavailable; apply migrations
by hand before the first start and after each deploy that ships new ones:

```sh
sudo podman run --rm \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest migrate
```

## Updating

Pull the new image and restart. Because the migrate unit uses
`RemainAfterExit=yes` it does not re-run on its own, so restart it explicitly
to apply any new migrations before the app comes back up:

```sh
sudo podman pull ghcr.io/m4schini/splitkauf:latest
sudo systemctl restart splitkauf-migrate.service   # re-applies migrations (a no-op when there are none)
sudo systemctl restart splitkauf.service           # recreates the app on the pulled image
```

For non-migration changes, restarting `splitkauf.service` alone is enough.
