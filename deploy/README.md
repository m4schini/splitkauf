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
   EOF

   sudo chmod 600 /etc/splitkauf/db.env /etc/splitkauf/splitkauf.env
   ```

   The two passwords must match.

3. Reload systemd so it picks up the generated `.service` units from the
   Quadlet files, then start the app (which pulls in the database via
   `Requires=`/`After=`):

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl start splitkauf.service
   sudo systemctl status splitkauf.service splitkauf-db.service
   ```

   Enable both on boot with `sudo systemctl enable splitkauf.service
   splitkauf-db.service` (or rely on `[Install] WantedBy=default.target`
   plus `daemon-reload`, which already registers them).

## Running migrations

Migrations are applied via a one-shot `migrate` invocation of the same
application image, connected to the same network and database as the running
stack:

```sh
sudo podman run --rm \
    --network splitkauf.network \
    --env-file /etc/splitkauf/splitkauf.env \
    ghcr.io/m4schini/splitkauf:latest migrate
```

Run this once after installing and after every deployment that ships new
migrations, before (re)starting `splitkauf.service`.

## Updating

Pulling a new image and restarting the service is sufficient for
non-migration changes:

```sh
sudo podman pull ghcr.io/m4schini/splitkauf:latest
sudo systemctl restart splitkauf.service
```

For changes that include new migrations, run the one-shot `migrate` command
above before restarting.
