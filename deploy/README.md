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

   # Auth mode (see "Authentication" below). For local username/password
   # accounts instead of OIDC, leave the OIDC vars unset and set:
   #   SPLITKAUF_AUTH_PASSWORD_ENABLED=true
   # then provision accounts with `useradd` (see "Password authentication").
   # Setting BOTH (all OIDC vars + PASSWORD_ENABLED) enables the combined
   # mode: the login page offers the password form and a "Sign in with SSO"
   # button side by side.
   #
   # OIDC login (Authentik). Leave all SPLITKAUF_AUTH_OIDC_* vars unset to run
   # with password or dev-auth instead.
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

Splitkauf selects its auth mode automatically from config:

- If `SPLITKAUF_AUTH_OIDC_ISSUER`, `SPLITKAUF_AUTH_OIDC_CLIENT_ID`, and
  `SPLITKAUF_AUTH_OIDC_CLIENT_SECRET` are **all** set, the backend runs the
  OIDC BFF (backend-for-frontend) flow against that provider. If
  `SPLITKAUF_AUTH_PASSWORD_ENABLED=true` is **also** set, the backend runs
  the combined mode instead: OIDC and local username/password sign-in are
  both offered, and the login page shows the password form plus a
  "Sign in with SSO" button. Either method establishes the same session.
- Else, if `SPLITKAUF_AUTH_PASSWORD_ENABLED=true`, the backend runs local
  username/password authentication only (see "Password authentication"
  below) — the option for a self-hosted instance without an identity
  provider.
- Else, the backend falls back to dev-auth (a single hardcoded dev user, the
  same as local development). This is only suitable for local/test
  deployments — do not leave both OIDC and password unset on a production host.

### Password authentication

Set `SPLITKAUF_AUTH_PASSWORD_ENABLED=true` to run local accounts (on its own,
or alongside OIDC for the combined mode). There is **no public sign-up**: the operator provisions
every account with the `useradd` command, which stores only a bcrypt hash.

```sh
# Interactive (prompts for the password, twice, with no echo):
sudo podman run --rm -it \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest useradd alex

# Non-interactive (e.g. from a secret store), password on stdin:
printf '%s' "$PASSWORD" | sudo podman run --rm -i \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest useradd alex --password-stdin
```

`useradd` creates exactly one account per invocation, so provisioning several
users means one run each:

```sh
# Two accounts, interactive:
sudo podman run --rm -it \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest useradd alex --name "Alex"

sudo podman run --rm -it \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest useradd bob --name "Bob"
```

Prefer the interactive form for ad-hoc provisioning; a password passed through a
shell variable ends up in the environment and possibly the shell history. When
automating, pipe it straight from a secret store into `--password-stdin`.

A wrong username and a wrong password are rejected identically (same 401), so
the login page never reveals which usernames exist. Keep
`SPLITKAUF_AUTH_SESSION_COOKIE_SECURE=true` behind HTTPS, exactly as for OIDC.

### Listing accounts

`userls` lists every identity known to the app — local accounts (whether or
not they have ever logged in), OIDC members, and the dev user:

```sh
sudo podman run --rm \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest userls
```

```
KIND   IDENTIFIER       USER_ID                               NAME     EMAIL             LAST_LOGIN
local  alex             0d9c1e64-…                            Alex     alex@example.com  2026-08-20 19:04
local  maria            7be20a11-…                            Maria    —                 never
oidc   238941579532     a3f8c9d2-…                            Alex S.  alex@schink.xyz   2026-08-21 08:12
```

`KIND` is `local` (username/password account), `oidc` (provider-backed
member), or `dev` (the fixed dev user). `IDENTIFIER` is the username for
local accounts and the auth subject otherwise — it is the value used in
`usermerge` selectors (`local:<username>` / `oidc:<subject>`). `LAST_LOGIN`
comes from the login-time member record and shows `never` for a local account
that has not signed in yet.

### Merging identities (`usermerge`)

When one person ends up with two identities — most commonly a local account
from before an identity provider existed, plus the OIDC account they use now
— their history is split across two user ids. `usermerge` unifies them by
rewriting all attribution (`lists.created_by`, `items.added_by`,
`items.bought_by`) from the source identity's user id to the target's, then
cleaning up the source: its member record is deleted, and a local source's
account is deleted too (its login stops working). Everything runs in one
database transaction.

Selectors take the form `local:<username>`, `oidc:<subject>`, or
`uuid:<user_id>` — copy the values from `userls`. An `oidc:` identity must
have logged in at least once (its subject is only known after the first
login); `uuid:` is the escape hatch that addresses any raw user id.

The typical local → OIDC migration:

1. Create the person's account in the identity provider.
2. Have them sign in to Splitkauf once via OIDC (this records their subject).
3. Run `userls` and note the local username and the new OIDC subject.
4. Merge:

   ```sh
   sudo podman run --rm -it \
       --network splitkauf.network \
       --env-file /etc/splitkauf/splitkauf.env \
       ghcr.io/m4schini/splitkauf:latest usermerge local:alex oidc:238941579532
   ```

The command prints the resolved identities and per-column row counts, then
asks for a `y/N` confirmation before writing anything; answering no changes
nothing. `--yes` skips the prompt for automation (and is required when stdin
is not a terminal).

Two caveats:

- **Merging away an OIDC identity does not block it.** When the *source* is
  an `oidc:` identity, the person can still log in at the provider — the next
  OIDC login derives the same user id again and recreates the member record.
  The command prints a warning in this case. Remove or disable the account in
  the identity provider if the person should lose access.
- **Live sessions of the source keep working until they expire.** Sessions
  are not queryable per user, so the merge does not invalidate them — the
  same limitation as deleting an account. They end at the configured session
  lifetime (`SPLITKAUF_AUTH_SESSION_LIFETIME`, default 168h).

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
   - Scopes: `openid profile email`. The provider only authenticates the
     user at login; Splitkauf stores no access or refresh token, so no
     further scopes are needed. Session duration is governed by
     `auth.session.lifetime` (`SPLITKAUF_AUTH_SESSION_LIFETIME`), not by any
     provider token lifetime.
   - Redirect URI: `<SPLITKAUF_APP_BASE_URL>/api/auth/callback` (e.g.
     `https://splitkauf.example.com/api/auth/callback`), matching
     `SPLITKAUF_AUTH_OIDC_REDIRECT_URL` exactly.
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

The app unit sets `AutoUpdate=registry`, so `podman auto-update` will pull a
newer `:latest` and restart it. Enable the periodic check with:

```sh
sudo systemctl enable --now podman-auto-update.timer
```

Auto-update only swaps the app image; it does **not** run migrations. For a
release that ships new migrations, apply them first (restart
`splitkauf-migrate.service`, or run the manual `migrate` command above) so the
app never starts against an out-of-date schema.
