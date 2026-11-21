# Deploying Zazzzor on a server

**English** · [Русский](README.ru.md)

This guide takes you from a clean Linux server to a running Docker stack.
Friends can follow the separate [Windows client installation guide](desktop.md).

| Service | What it does | Access |
|---|---|---|
| `caddy` | Terminates HTTPS with a Let's Encrypt certificate via DuckDNS and serves the web client, API and connectivity test page | `ZAZZZOR_HTTPS_PORT` (443) |
| `api` | Serves REST at `/api/v1` and WebSocket at `/api/v1/gateway` | `127.0.0.1:${ZAZZZOR_API_PORT}` only |
| `postgres` | Stores data | Private network shared with `api` |
| `livekit` | Carries voice and screen sharing | 7881/tcp and 7882/udp. Signaling on 7880 is local only |
| `duckdns` | Updates the IP in DuckDNS every 5 minutes | Outbound only |

## What works today

- **Web and API.** Sign-up, servers, channels, categories, roles, permissions,
  invitations, chat with replies and edits, voice tokens, moderation, the
  realtime WebSocket gateway, profile settings and an admin panel. Open
  `https://<domain>/` in a current version of Chrome, Edge or Firefox. See
  section 9 for the API smoke test and section 14 for LiveKit diagnostics.
- **Voice and screen sharing (phase 1.3).** Voice channels, speaking indicators,
  microphone and audio controls, push-to-talk while the window is focused,
  device selection, voice moderation, 1080p60 screen sharing and a Watch
  button for other people's streams. Chrome and Edge can share tab audio.
  On Windows, sharing the entire screen in Chrome or Edge captures system
  audio with voice chat excluded. Sharing a window or using Firefox shares
  video only. The default Enhanced noise suppression runs DeepFilterNet3
  in the browser. The server delivers the roughly 23 MB model on the first
  call and the browser caches it. The client falls back to RNNoise when
  needed and reports the switch in **Voice & video**. Use the
  [connectivity test page](#connectivity-test-phase-11) to test calls separately.
- **Windows app (phase 1.4).** The Windows installation guide covers the
  desktop client, including global shortcuts and window audio.

Friends sign up at `https://<domain>/register` while registration is open
(section 10). Then send them an invite from the server menu under **Invite people**.

For Docker Swarm (`docker stack`), use [section 15](#15-docker-swarm-docker-stack).
Sections 1–7 also apply to Swarm.

Run all commands from the **repository root** (the `zazzzor` directory).
If Docker requires `sudo`, use it for `docker ...` and
`bash deploy/scripts/...` commands.

## 1. Requirements

- Linux on x86_64. Ubuntu and Debian have been tested.
- Docker Engine **25 or later** and Docker Compose **v2.24 or later**.
  The stack has been tested with Engine 29.6 and Compose 5.3.
  Run `docker version` and `docker compose version` to check.
- Free ports 443/tcp (or an alternate HTTPS port, see section 14), 7881/tcp
  and 7882/udp. Keep the private 7880/tcp and 8080/tcp (API) free as well.
- About 3.5 GB in combined container memory limits (LiveKit 1 GB, Postgres
  1 GB, API 1 GB, Caddy 256 MB, DuckDNS 64 MB). A 12 GB host has room to spare.
- A public IP address and access to the router's port forwarding settings.
- `git`, `openssl` for generating secrets and `ss` from `iproute2`.

## 2. Install Docker (Ubuntu or Debian)

If Docker is installed, check its versions against section 1 and skip to
section 3.

See the official Docker installation guide for [Ubuntu](https://docs.docker.com/engine/install/ubuntu/)
or [Debian](https://docs.docker.com/engine/install/debian/). To use the
official installer script:

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
# To use Docker without sudo, sign out and back in after this command.
sudo usermod -aG docker "$USER"
docker compose version
```

If Docker Hub downloads fail, which can happen from Russia, configure a
registry mirror in `/etc/docker/daemon.json`
(`"registry-mirrors": ["https://<mirror>"]`) and restart Docker with
`sudo systemctl restart docker`.

## 3. Get the source

```bash
git clone https://github.com/whoaskedssselery/Zazzzor.git zazzzor
cd zazzzor
```

## 4. Create a DuckDNS subdomain and copy the token

1. Sign in at <https://www.duckdns.org> using GitHub, Google or another provider.
2. Create a subdomain such as `zazzzor`, which gives you `zazzzor.duckdns.org`.
3. Copy the `token` from the top of the page.

The `duckdns` container updates the IP every 5 minutes.

## 5. Configure `deploy/.env`

```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
nano deploy/.env
```

| Variable | Value |
|---|---|
| `ZAZZZOR_DOMAIN` | Full hostname, such as `zazzzor.duckdns.org` |
| `DUCKDNS_SUBDOMAIN` | `zazzzor`, without `.duckdns.org` |
| `DUCKDNS_TOKEN` | The token from duckdns.org |
| `ACME_EMAIL` | Your email address for Let's Encrypt |
| `LIVEKIT_API_KEY` | Output of `openssl rand -hex 16` |
| `LIVEKIT_API_SECRET` | Output of `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | Output of `openssl rand -hex 24` (hex characters only) |
| `ZAZZZOR_ADMIN_USERNAMES` | Your future username, such as `ivan` (section 10) |
| `ZAZZZOR_BACKUP_DIR` | Host directory for backups. You can keep `/srv/zazzzor-backups` and create it as shown in section 12, step 1 |

Run each `openssl` command separately and paste its output into the
corresponding line. The port settings, `ZAZZZOR_API_PORT` and image versions
can keep their defaults.

Postgres initializes the database with `POSTGRES_PASSWORD` on the first
start. Changing `.env` afterward leaves the password in the database in
place and breaks API access. See section 13 for recovery.

Compose reports any required setting that is missing. For example:

```
required variable POSTGRES_PASSWORD is missing a value: заполните POSTGRES_PASSWORD в deploy/.env - результат openssl rand -hex 24
```

Keep `deploy/.env` private. Git ignores it. The adjacent `deploy/.env.ci`
uses placeholders (a `.invalid` domain and a zero token) for validating
the configuration without secrets. Use it for this check only.

```bash
docker compose --env-file deploy/.env.ci -f deploy/docker-compose.yml config --quiet
```

## 6. Check that the ports are free

Before starting Zazzzor, check whether other services on the host already
hold the required ports.

```bash
sudo bash deploy/scripts/check-ports.sh
```

- `Все порты свободны` means that all ports are available.
- `ЗАНЯТ: 443/tcp (процесс ...)` means that a port is in use. For port 443,
  see [If port 443 is in use](#if-port-443-is-in-use). You can change
  `LIVEKIT_TCP_PORT`, `LIVEKIT_UDP_PORT` and `ZAZZZOR_API_PORT` in
  `deploy/.env` to available ports. Keep the internal LiveKit port at 7880.

`sudo` lets the script identify the process holding the port. Run the
check before the first start, while Zazzzor's ports are still free.

## 7. Forward ports on your router

Forward these ports to the server's LAN address, using these defaults or
your configured ports.

| Public port | Protocol | Server port | Purpose |
|---|---|---|---|
| 443 | TCP | `ZAZZZOR_HTTPS_PORT` (443) | HTTPS, including the API, web client and LiveKit signaling |
| 7881 | TCP | `LIVEKIT_TCP_PORT` (7881) | Media fallback when UDP is blocked |
| 7882 | UDP | `LIVEKIT_UDP_PORT` (7882) | Primary media transport for voice, video and screen sharing |

The certificate is issued through DuckDNS, so leave port 80 closed.
Keep the API (8080), LiveKit signaling (7880) and Postgres private.

If the host has a firewall such as `ufw` or `firewalld`, open the same
ports there. For example:

```bash
sudo ufw allow 443/tcp
sudo ufw allow 7881/tcp
sudo ufw allow 7882/udp
```

Increase the UDP receive buffer to avoid a warning from LiveKit.

```bash
echo 'net.core.rmem_max=5000000' | sudo tee /etc/sysctl.d/90-zazzzor.conf
sudo sysctl --system
```

## 8. Start the stack

Create the backup directory first (section 12, step 1).

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

The first build of the Caddy and API images takes several minutes.
The API applies database migrations when it starts.

## 9. Verify the deployment

```bash
docker compose -f deploy/docker-compose.yml ps
```

The `postgres`, `api` and `backup` services should show `(healthy)` in
`STATUS`. The API can take up to a minute. Other services should show `Up`.
The API's only published port should be `127.0.0.1:8080->8080/tcp`.

DNS certificate issuance takes 1–3 minutes. Look for
`certificate obtained successfully` in the Caddy logs.

```bash
docker compose -f deploy/docker-compose.yml logs -f caddy
```

Run the end-to-end API smoke test.

```bash
bash deploy/scripts/smoke-api.sh
```

Inside the `api` container, the script signs up a `smoke_<random characters>`
user, creates a server, sends and reads a message, opens WebSocket
`/api/v1/gateway`, waits up to 10 seconds for HELLO and READY, then deletes
the server it created. The last line is `OK` on success. On failure it
reports the step and reason. For example:

```
ERROR: step "sign-up": status 403 registration_closed instead of 201 (registration is closed in the server settings; open it for the check)
```

- Sign-up is limited to 3 requests per hour per address. Further runs may
  get a 429. Wait an hour before trying again.
- Open registration for the test (section 10).
- The smoke accounts remain in the database. The app does not support
  deleting accounts.

Run the check through Caddy to verify HTTPS, the certificate and the
`/api/*` route.

```bash
bash deploy/scripts/smoke-api.sh https://zazzzor.duckdns.org
```

Add the voice check explicitly.

```bash
docker compose -f deploy/docker-compose.yml exec -T api /zazzzor smoke --voice http://127.0.0.1:8080
```

In Swarm, run `/zazzzor smoke --voice <base_url>` inside the API container.
The smoke test uses the LiveKit address returned by the API for the first
origin in `ZAZZZOR_ALLOWED_ORIGINS`. The container must be able to reach
that address and the `/rtc` route. The test waits up to 15 seconds each for
the join and leave webhooks, then deletes the test server even on failure.
This verifies signaling, token issuance and webhook delivery. Test sound
quality, RTP through the participants' NATs and screen sharing separately.
The normal smoke test and restore test run with voice disabled.

If the public check fails to connect and the check without a URL passes,
your router may lack hairpin NAT. Open `https://<domain>/api/v1/healthz`
on a phone using mobile data and look for `{"status":"ok"}`.

Verify the listener addresses.

```bash
sudo ss -lntup | grep -E ':(443|7880|7881|7882|8080)\b'
```

The API (8080/tcp) should bind to `127.0.0.1` only. LiveKit signaling
(7880/tcp) should bind to `127.0.0.1` and the Compose bridge gateway
`172.31.250.1` (see section 14). Ports 443/tcp, 7881/tcp and 7882/udp
should bind to all addresses (`*`, `0.0.0.0` or `[::]`).
Substitute your actual HTTPS and API ports if you changed them. If 7880 or
8080 appears on a wildcard address, correct the configuration before
continuing.

Open `https://<domain>/` (the sign-in page), `https://<domain>/api/v1/healthz`
and `https://<domain>/connectivity-test/`. The web client responds with 200
for paths such as `/channels/…` and routes them in the browser. Paths under
`/api/…`, `/rtc…` and `/connectivity-test…` have their own handlers.

## 10. Create the first administrator and manage registration

1. Set your username in `ZAZZZOR_ADMIN_USERNAMES` in `deploy/.env`.
   Update the API using the same deployment command you used to start the
   stack.
2. Create the account in a terminal. The prompt hides the password and
   passes it to the command through stdin.

   ```bash
   read -rsp 'Password: ' PASS; echo
   printf '%s\n' "$PASS" |
     docker compose --env-file deploy/.env -f deploy/docker-compose.yml exec -T api /zazzzor create-admin ivan
   unset PASS
   ```

   In Swarm, replace the Compose command with
   `docker exec -i <api-container> /zazzzor create-admin ivan` in the same
   pipeline. If the account already exists, sign in with its password.
3. Sign in using the browser. A fresh database has registration closed.
   To allow new accounts, go to **User settings** → **Administration** →
   **Zazzzor server**. Updates preserve this setting on existing databases.

Administrators can look up an account by username, reset its password,
block it or unblock it from the **Users** tab.

## 11. Update

```bash
git pull
# Compare the settings and copy new variables from .env.example into .env.
diff deploy/.env.example deploy/.env
docker compose -f deploy/docker-compose.yml up -d --build
```

When upgrading to a release with voice channels, `up -d` recreates
`zazzzor_api` with the fixed subnet `172.31.250.0/24` (section 14).
The API and LiveKit restart for a few seconds. The API reads the existing
`LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET`.

When upgrading to a release with backups, set `ZAZZZOR_BACKUP_DIR` in
`deploy/.env` and create the directory before `up` (section 12, step 1).
All Compose commands require this variable.

The API applies migrations at startup. To have `zazzzor --version` report
the build version, build with:

```bash
ZAZZZOR_VERSION=$(git describe --tags --always) docker compose -f deploy/docker-compose.yml up -d --build
```

`docker image prune` removes dangling images across all projects on the host.

## 12. Data and backups

| Docker volume | Contents |
|---|---|
| `zazzzor_pgdata` | Postgres data: users, servers and messages |
| `zazzzor_apidata` | Uploaded files, including avatars and server icons |
| `zazzzor_caddy_data` | HTTPS certificates |

The volumes survive `down`, restarts and updates.
**`docker compose ... down -v` permanently deletes their data.** Take a
backup before using `-v`.

### Automatic backups

The `backup` service copies the database and uploaded files daily in
Compose and Swarm.

1. Before first start, create the host directory named by
   `ZAZZZOR_BACKUP_DIR` in `deploy/.env`. The example uses
   `/srv/zazzzor-backups`. Give the directory to UID 65532, which runs
   the backup service and API.

   ```bash
   sudo install -d -o 65532 -g 65532 -m 0700 /srv/zazzzor-backups
   ```

   Backups contain messages, sessions and password hashes. Restrict
   directory access to the service account.
2. Backups run at 04:30 in `TZ=Europe/Warsaw`. The service keeps the latest
   backup for each of the last 7 days and 4 weeks. To change the schedule
   or retention, edit `TZ`, `ZAZZZOR_BACKUP_TIME`,
   `ZAZZZOR_BACKUP_KEEP_DAILY` and `ZAZZZOR_BACKUP_KEEP_WEEKLY` in
   `deploy/.env` (see `.env.example`). Apply with
   `docker compose -f deploy/docker-compose.yml up -d` or `stack.sh deploy`.
   If the server misses 04:30 while off, it backs up immediately at startup.
   Failed attempts retry after 30 minutes.
3. Each backup is a directory such as `20261027T023000Z-AbCd1234`, named
   by start time in UTC. It contains `database.dump`, `apidata.tar.gz`
   (uploads and trash), `manifest.json` and `SHA256SUMS`. Incomplete backups
   remain in `.partial-*` directories and are excluded from restores.
4. Monitor service health. It becomes `unhealthy` if its last backup is
   over 26 hours old or its directory is unwritable.

   ```bash
   docker compose -f deploy/docker-compose.yml ps backup
   docker compose -f deploy/docker-compose.yml logs --tail 100 backup
   # In Swarm, an unhealthy task restarts and appears in the task list.
   bash deploy/scripts/stack.sh ps
   docker service ps zazzzor_backup
   bash deploy/scripts/stack.sh logs backup
   ```

   Backup errors report the cause and how to fix it.
5. To run a backup immediately:

   ```bash
   bash deploy/scripts/backup.sh compose backup
   # Swarm:
   bash deploy/scripts/stack.sh backup
   ```

Deleted or replaced images stay in `trash` in the uploads volume for
8 days and are no longer served. This lets a backup taken before the
deletion retain the files it references. `ZAZZZOR_FILE_RETENTION` sets
the period and must exceed one day. The API cleans the trash hourly.
Let the API handle cleanup.

Each user has 2 GiB of attachment storage, including unsent uploads.
Attachments stop counting toward the quota when their messages are deleted.
Uploads require at least 2 GiB free on the files volume. To change these
limits, uncomment `ZAZZZOR_ATTACHMENT_QUOTA_BYTES` and
`ZAZZZOR_MIN_FREE_BYTES` in `deploy/.env`. Both are in bytes. Zero
disables the corresponding limit.

The same volume stores attachments in their original form. The limits
are 25 MiB per file, 10 files per message and 30 uploads per minute per
user, with batches of up to 10 and up to two batches in flight. Unsent
files become inaccessible after an hour and the background job removes
them on its next pass. Deleted attachments follow the same trash
retention as pictures. Budget disk space for original uploads, trash and
backups.

### Restore

The targets are up to 24 hours of data loss (RPO) and about an hour to
recover (RTO). Both are still untested on the target hardware.

**A restore replaces all server data with the selected backup.** Changes
made after that backup will be lost.

1. If the server is still running, take a fresh backup using step 5 above.
2. Select a backup with `sudo ls /srv/zazzzor-backups`.
3. Restore it.

   ```bash
   bash deploy/scripts/backup.sh compose restore 20261027T023000Z-AbCd1234 --yes
   # Swarm:
   bash deploy/scripts/stack.sh restore 20261027T023000Z-AbCd1234 --yes
   ```

   You can supply a directory name or path. The script verifies the
   checksums and backup schema, stops the API, restores the database and
   uploads, then runs `files reconcile --strict` against the database.
   Missing referenced files fail the check. After a successful check,
   the script removes the old files and starts the API.
4. Verify sign-in, messages, avatars, icons, attachments and emoji.

If the script stops with an error:

- A failure before the API stops leaves the data intact. If the backup
  is newer than the API image, use the image named in the error or update
  the source and rebuild.
- `Данные не изменены` means the restore stopped before making changes.
  The script restarts the API. Fix the issue and retry.
- If the lock is held, wait for the other backup or restore to finish.
  The script leaves the API stopped.
- If the database restore, file swap or reconciliation fails, the API
  stays stopped. Keep `.restore-old-*` in the files volume. Fix the
  problem and resume with the same backup.

  ```bash
  bash deploy/scripts/backup.sh compose restore 20261027T023000Z-AbCd1234 --yes --resume
  # Swarm:
  bash deploy/scripts/stack.sh restore 20261027T023000Z-AbCd1234 --yes --resume
  ```

  If an older release left an interrupted restore with no journal,
  save the old files separately before attempting a manual recovery.

A newer image can restore an older database schema. The API applies
pending migrations at startup.

Backups cover the database and uploads. Caddy reissues certificates
and LiveKit calls disconnect during a restore. Keep a separate copy of
`deploy/.env` with its secrets. Store these unencrypted backups securely.

### Restore on a clean server

1. Install Docker and clone the repository (sections 2 and 3).
   Restore `deploy/.env` from its separate backup.
2. Create the backup directory as in step 1 of
   [Automatic backups](#automatic-backups).
3. Copy the backup into the directory and give UID 65532 ownership so
   the service can read it.

   ```bash
   rsync -a backup-user@backup-host:/srv/backups/zazzzor/20261027T023000Z-AbCd1234 /tmp/
   sudo cp -a /tmp/20261027T023000Z-AbCd1234 /srv/zazzzor-backups/
   sudo chown -R 65532:65532 /srv/zazzzor-backups/20261027T023000Z-AbCd1234
   ```

4. Build the images and start Postgres alone.

   ```bash
   docker compose -f deploy/docker-compose.yml build
   docker compose -f deploy/docker-compose.yml up -d --wait postgres
   ```

5. Restore the backup and start the remaining services.

   ```bash
   bash deploy/scripts/backup.sh compose restore 20261027T023000Z-AbCd1234 --yes
   docker compose -f deploy/docker-compose.yml up -d
   ```

   With Swarm, replace steps 4 and 5 with `stack.sh deploy` (section 15)
   and then `stack.sh restore <backup> --yes`.

### Off-site copies

A disk failure can take out local data and all its local backups. Copy
completed backups to another machine or removable drive. The backup
service leaves this transfer to the owner. For example:

```bash
# To another server over SSH
sudo rsync -a --exclude '.partial-*' --exclude '.lock' --exclude '.last-scheduled' \
  /srv/zazzzor-backups/ backup-user@backup-host:/srv/backups/zazzzor/
# To cloud storage via rclone (set up the remote with rclone config)
sudo rclone copy /srv/zazzzor-backups remote:zazzzor-backups \
  --exclude '.partial-*/**' --exclude '.lock' --exclude '.last-scheduled'
```

Verify the checksum after copying with `cd <backup> && sha256sum -c SHA256SUMS`.

### Test the restore

Test the restore every few months and after major updates. It requires
Docker and Bash.

```bash
bash deploy/scripts/restore-test.sh
```

The script starts a temporary Compose project. Through the API, it
creates a user with an avatar, a server with an icon and emoji, and a
message with an attachment. It backs up the project and restores it twice,
first over a database with an extra table and then to empty volumes.
It checks sign-in, the message and the SHA-256 hashes of all four files.
At the end it removes the temporary project and prints
`restore test: OK`. The production stack stays running. CI runs the same
test.

### Back up manually if the scripts fail

Run the commands from the repository root. **The API stops during this
backup** to keep the database and uploaded files consistent.

```bash
PG_IMAGE=postgres:$(grep -E '^POSTGRES_VERSION=' deploy/.env | cut -d= -f2)
mkdir -p ~/zazzzor-manual
docker compose -f deploy/docker-compose.yml stop api
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_dump -U zazzzor -d zazzzor -Fc > ~/zazzzor-manual/database.dump
docker run --rm -v zazzzor_apidata:/data:ro "$PG_IMAGE" \
  tar czf - -C /data avatars icons attachments trash > ~/zazzzor-manual/apidata.tar.gz
docker compose -f deploy/docker-compose.yml start api
(cd ~/zazzzor-manual && sha256sum database.dump apidata.tar.gz > SHA256SUMS)
```

**The following restore replaces all server data.**

```bash
PG_IMAGE=postgres:$(grep -E '^POSTGRES_VERSION=' deploy/.env | cut -d= -f2)
(cd ~/zazzzor-manual && sha256sum -c SHA256SUMS)
docker compose -f deploy/docker-compose.yml stop api
docker compose -f deploy/docker-compose.yml exec -T postgres psql -U zazzzor -d zazzzor \
  -v ON_ERROR_STOP=1 -c 'DROP SCHEMA public CASCADE' -c 'CREATE SCHEMA public'
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_restore -U zazzzor -d zazzzor --exit-on-error --no-owner < ~/zazzzor-manual/database.dump
docker run --rm -i --user 65532:65532 -v zazzzor_apidata:/data "$PG_IMAGE" \
  sh -c 'rm -rf /data/avatars /data/icons /data/attachments /data/trash && tar xzf - -C /data' \
  < ~/zazzzor-manual/apidata.tar.gz
docker compose -f deploy/docker-compose.yml run --rm --no-deps api files reconcile
docker compose -f deploy/docker-compose.yml start api
```

## 13. Logs, restarts and troubleshooting

```bash
# View logs for a service (api, postgres, caddy, livekit, duckdns, backup)
docker compose -f deploy/docker-compose.yml logs -f api
# Restart a service
docker compose -f deploy/docker-compose.yml restart api
# Apply .env or source changes
docker compose -f deploy/docker-compose.yml up -d --build
# Stop the stack and keep its data
docker compose -f deploy/docker-compose.yml down
```

Container logs rotate at up to 3 files of 10 MB per service. The API logs
one JSON line per request and omits passwords, tokens and cookies.

- **`docker compose` reports `required variable ... is missing a value`.**
  Fill in the variable named by the error in `deploy/.env`. After an update,
  compare with `.env.example` for new settings.
- **A port is in use** (`address already in use` or `port is already allocated`).
  `sudo bash deploy/scripts/check-ports.sh` shows the process. Choose a free
  port in `.env`. See section 14 for HTTPS.
- **Certificate issuance fails.** Check `DUCKDNS_TOKEN` and `ZAZZZOR_DOMAIN`
  in `.env` and check the Caddy logs. `deploy/caddy/Caddyfile` sets
  `resolvers 1.1.1.1 8.8.8.8` for DNS-01 because ISP and router caches can
  return stale TXT records and stall verification. If outbound DNS to those
  resolvers is blocked, remove that line and run
  `docker compose -f deploy/docker-compose.yml up -d --build`.
- **The API is `unhealthy` or `Restarting`.** Run
  `docker compose -f deploy/docker-compose.yml logs --tail 50 api`.
  The first `"level":"ERROR"` line gives the cause. `load config` names a
  configuration error and its variable. `open database` means Postgres is
  unreachable or the password is wrong.
- **`POSTGRES_PASSWORD` changed after the first run.** The API logs report
  `password authentication failed for user "zazzzor"`. Restore the old
  password in `.env` or update the database password after `up -d` passes
  the new value to the container's environment. Then restart the API.

  ```bash
  docker compose -f deploy/docker-compose.yml up -d
  docker compose -f deploy/docker-compose.yml exec postgres \
    sh -c 'psql -U zazzzor -d zazzzor -c "ALTER USER zazzzor PASSWORD '\''$POSTGRES_PASSWORD'\''"'
  docker compose -f deploy/docker-compose.yml restart api
  ```

- **`https://<domain>/api/...` returns 502.** Caddy is running and cannot
  reach the API. Check `ps` and `logs api`. If `ZAZZZOR_API_PORT` changed,
  run `up -d` so Caddy receives the new port.
- **The website does not open.** Check that `nslookup <domain>` returns
  your public IP, port 443 is forwarded and Caddy issued a certificate.
  Test from a phone using mobile data.

## 14. Run alongside other services

The Compose deployment exposes HTTPS, 7881/tcp and 7882/udp. Port 7880
and `ZAZZZOR_API_PORT` are private, with the Compose voice subnet access
described below. Postgres has no published port, so an existing Postgres
on 5432 can keep running.

Zazzzor containers, volumes and networks use `zazzzor-*` or `zazzzor_*`
names. Before using `docker system prune` or `docker volume prune`,
remember that these Docker commands affect **all** projects on the host.

### If port 443 is in use

If another service such as nginx holds port 443 on the host, configure
`deploy/.env` as follows.

```
ZAZZZOR_HTTPS_PORT=8443
```

Forward **public 443 → internal 8443** over TCP on your router. For
connectivity links and API origins without a port suffix, uncomment this
setting in the same file.

```
ZAZZZOR_PUBLIC_PORT=443
```

Apply with `docker compose -f deploy/docker-compose.yml up -d`.

If a different service already uses public 443, forward public 8443 to
internal 8443 and leave `ZAZZZOR_PUBLIC_PORT` unset. URLs then include
`:8443`. Some networks block nonstandard ports.

For requests that change state, Compose allows the origins
`https://<domain>` and `https://<domain>:<public-port>`. The public port
comes from `ZAZZZOR_PUBLIC_PORT` or, when unset, `ZAZZZOR_HTTPS_PORT`.

### Networking and client addresses

Caddy uses host networking and connects to the API at
`127.0.0.1:${ZAZZZOR_API_PORT}`. Docker forwards the connection from the
`zazzzor_api` bridge gateway `172.31.250.1`. The API trusts Caddy's
`X-Forwarded-For` only from addresses in `ZAZZZOR_TRUSTED_PROXIES`,
which defaults to `172.16.0.0/12,127.0.0.1/32` and includes this gateway.
Per-address limits such as 3 registrations per hour use the forwarded
client address. If the proxy is untrusted, users behind it share the
same limit.

Only processes on the host, including Caddy, can connect to the
published API port.

### Voice channels

The API issues LiveKit tokens and tracks voice channel membership. Voice
state uses two connections.

1. **API → LiveKit** lists rooms and disconnects participants. LiveKit
   uses host networking and listens on port 7880 on `127.0.0.1` for Caddy
   and on the private `zazzzor_api` bridge gateway `172.31.250.1`. The
   API connects via `http://172.31.250.1:7880`.
2. **LiveKit → API** sends signed webhooks for joins, leaves and screen
   sharing to
   `http://127.0.0.1:${ZAZZZOR_API_PORT}/api/v1/livekit/webhook`.
   The signature uses the LiveKit credentials for `LIVEKIT_API_KEY`.
   The API rejects unsigned requests.

Caddy returns 404 on the public `/api/v1/livekit/webhook` route, including
for signed requests. LiveKit uses the private loopback URL.

The services use `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` from
`deploy/.env`. If neither is set, the API starts, logs `voice is off`
once and returns `503 voice_unavailable` for voice requests. With only
one set, API startup fails and names the missing variable.

Check the connections after start.

```bash
# LiveKit listens on both addresses.
docker compose -f deploy/docker-compose.yml logs livekit | grep bindAddresses
# The API reaches LiveKit when reconciling rooms at startup and every 5 minutes.
docker compose -f deploy/docker-compose.yml logs livekit | grep ListRooms
# A webhook arrives when someone joins a voice channel.
docker compose -f deploy/docker-compose.yml logs api | grep livekit/webhook
```

Webhook log entries should show `"status":204`. A 401 indicates different
credentials. Run `up -d` so both services reload `deploy/.env`.

The private API metric
`zazzzor_voice_reconcile_last_success_timestamp_seconds` records the last
complete successful reconciliation in Unix seconds. Zero means voice is
disabled or reconciliation has yet to succeed. If one room fails, the
remaining rooms are still checked and the timestamp stays where it was.
If it is older than two `ReconcileInterval` periods (defined in the
server code), check the API logs and LiveKit availability. Text chat and
`/api/v1/readyz` stay available during a LiveKit outage.

With host-networked Swarm (section 15), check metrics from the server.
Adjust `9464` if you changed `ZAZZZOR_METRICS_PORT`.

```bash
curl --fail http://127.0.0.1:9464/metrics | grep zazzzor_voice_reconcile
```

The Compose metrics listener stays inside the Docker network. Keep it
private.

If LiveKit cannot bind `172.31.250.1` after a reboot, reapply the Compose
configuration and inspect the logs.

```bash
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml logs --tail 50 livekit
```

If another network already uses `172.31.250.0/24`,
`docker compose up` reports `Pool overlaps with other one on this address space`.
Change all three references in `deploy/docker-compose.yml`: the subnet
and gateway under `networks.api`, LiveKit's `bind_addresses` and the API's
`ZAZZZOR_LIVEKIT_URL`. Keep the gateway in `172.16.0.0/12` or add it to
`ZAZZZOR_TRUSTED_PROXIES`.

After an API restart, LiveKit restores voice state within seconds. Clients
resend their microphone and speaker status.

Server mute and deafen flags live in API memory. A restart or rolling
update clears them and may let someone unmute in an active call.
Reapply moderation after an update if needed. Leaving a voice channel
preserves the flags. Leaving the server or being kicked or banned clears
them.

## 15. Docker Swarm (`docker stack`)

For a server already running Docker Swarm, use
`deploy/docker-stack.yml` and `deploy/scripts/stack.sh`.
Follow sections 1–7 for Docker, source, DuckDNS, `.env` and port setup.
Use the following commands in place of those in sections 8–13. Stop
Compose before starting the stack since both configurations use the same
ports.

### How the stack differs from Compose

Swarm cannot restrict published ports to `127.0.0.1`. Postgres and the
API use host networking and bind to loopback directly to stay private.

| Service | Listeners |
|---|---|
| `caddy` | `ZAZZZOR_HTTPS_PORT` (443) on all addresses |
| `livekit` | 7881/tcp and 7882/udp on all addresses. 7880 on `127.0.0.1` for Caddy and the API |
| `api` | `127.0.0.1:${ZAZZZOR_API_PORT}` (8080), metrics at `127.0.0.1:${ZAZZZOR_METRICS_PORT}` (9464) |
| `postgres` | `127.0.0.1:${POSTGRES_PORT}` (55432 to avoid conflict with Postgres on 5432) |
| `duckdns` | Outbound only |

- `stack.sh` builds images on the server and pins all services to that
  node. The builds use content-based tags, so an update to API code
  restarts only the API while the Caddy image remains the same.
- Swarm does not support `depends_on`. The API waits up to 60 seconds
  for Postgres and logs `database not ready, retrying`. Swarm restarts it
  if the wait times out. A bad password fails immediately.
- The API and LiveKit reach each other on `127.0.0.1` through host
  networking. The API calls port 7880, while LiveKit sends webhooks to
  `127.0.0.1:${ZAZZZOR_API_PORT}`. The stack has no need for `172.31.250.0/24`.
- Swarm does not support `no-new-privileges`. The API runs as a non-root
  user with a read-only filesystem and `cap_drop: ALL`.
- API metrics (`127.0.0.1:9464/metrics`) and Postgres
  (`127.0.0.1:55432`) are accessible to processes on the host. They are
  private to Docker networking under Compose. Neither endpoint is
  accessible externally or from ordinary containers. Postgres requires a
  password and uses `scram-sha-256`.
- The volume names match Compose, including `zazzzor_pgdata`. To switch
  to Swarm, run `docker compose -f deploy/docker-compose.yml down`
  **without `-v`** and deploy the stack. Keep the same `STACK_NAME` to
  keep using the existing volumes.

### Deploy

If Swarm is not active (check `docker info` for `Swarm: active`),
initialize it once.

```bash
docker swarm init
```

On a host with multiple addresses Docker may ask for one explicitly.
Use `docker swarm init --advertise-addr <server-LAN-IP>`.

The port check in section 6 also checks `POSTGRES_PORT` and
`ZAZZZOR_METRICS_PORT` in Swarm mode. Add `--stack` if you check before
running `docker swarm init`. Deploy the stack.

```bash
bash deploy/scripts/stack.sh deploy
```

The script loads `deploy/.env`, builds `zazzzor-api` and `zazzzor-caddy`,
runs `docker stack deploy` and waits for service health checks. The first
build takes a few minutes. All tasks in the resulting table should show
`Running`.

### Verify

```bash
bash deploy/scripts/stack.sh ps
```

```bash
docker ps --filter label=com.docker.stack.namespace=zazzzor --format 'table {{.Names}}\t{{.Status}}'
```

Look for `(healthy)` on `postgres` and `api`. Follow the other checks
from section 9. `bash deploy/scripts/smoke-api.sh` finds the Swarm API
container. `sudo ss -lntup` should show ports 8080, 9464, 55432 and 7880
bound only to `127.0.0.1`.

### View logs, update and stop

```bash
bash deploy/scripts/stack.sh logs api
```

Replace `api` with `caddy`, `livekit`, `postgres` or `duckdns` for other
service logs. To follow a log, run
`bash deploy/scripts/stack.sh logs caddy --follow`.

To update:

```bash
git pull && { diff deploy/.env.example deploy/.env; bash deploy/scripts/stack.sh deploy; }
```

Swarm replaces changed services one at a time, stopping the old task
before starting the new one. It rolls back a service that fails to start.
Database migrations stay applied and must remain compatible with the
previous version. Use the same `deploy` command to apply `.env` changes.

Keep the previous image for rollbacks. Remove older images by tag
with `docker image ls zazzzor-api` and `docker rmi zazzzor-api:<tag>`.
`docker image prune -a` also removes the rollback image and unused
images from other projects.

To stop the stack and wait for it to exit, run the following. The data
volumes are retained.

```bash
bash deploy/scripts/stack.sh rm
```

### Swarm backups and database password

The `backup` service and section 12 procedures also apply to Swarm.
Run them through `stack.sh`.

```bash
bash deploy/scripts/stack.sh backup
bash deploy/scripts/stack.sh restore 20261027T023000Z-AbCd1234 --yes
bash deploy/scripts/stack.sh reconcile
```

Before deploying, create `ZAZZZOR_BACKUP_DIR` and assign it to UID 65532.
If the directory setup is wrong, `deploy` stops and prints the command
to fix it. For manual backups or restores from section 12, stop and start
the API with `docker service scale zazzzor_api=0` and
`docker service scale zazzzor_api=1`. Replace
`docker compose ... exec -T postgres` with `docker exec -i "$PG"` after
setting `PG=$(docker ps -q --filter label=com.docker.swarm.service.name=zazzzor_postgres)`.

To rotate `POSTGRES_PASSWORD`, change the password inside Postgres while
the old deployment is running. Then redeploy with the new `.env`. If you
deploy first, the new API fails to connect and Swarm rolls it back to the
old password. Changing Postgres after that leaves the API down. Enter
the new value in `deploy/.env`, then run:

```bash
PG=$(docker ps -q --filter label=com.docker.swarm.service.name=zazzzor_postgres)
NEWPW=$(grep -E '^POSTGRES_PASSWORD=' deploy/.env | cut -d= -f2) \
  docker exec -e NEWPW "$PG" sh -c 'psql -U zazzzor -d zazzzor -c "ALTER USER zazzzor PASSWORD '\''$NEWPW'\''"'
bash deploy/scripts/stack.sh deploy
```

`docker exec -e NEWPW` passes the password to Docker through the
environment to avoid putting it in the Docker command's arguments.

## 16. Prebuilt images (GitHub Container Registry)

The `.github/workflows/images.yml` workflow builds these images and
publishes them to GHCR. You can deploy without building on the server.

| Image | Contents |
|---|---|
| `ghcr.io/whoaskedssselery/zazzzor-api` | API (`server/Dockerfile`) |
| `ghcr.io/whoaskedssselery/zazzzor-caddy` | Caddy with DuckDNS support and static pages (`deploy/caddy/Dockerfile`) |
| `ghcr.io/whoaskedssselery/zazzzor-backup` | Postgres and backup scripts (`deploy/backup/Dockerfile`) for the Traefik stack's `backup` service |

With the repository variable `DOCKERHUB_USERNAME` and a write-enabled
`DOCKERHUB_TOKEN` repository secret, the workflow also publishes images
with the same tags to Docker Hub under `<DOCKERHUB_USERNAME>/zazzzor-api`,
`<DOCKERHUB_USERNAME>/zazzzor-caddy` and `<DOCKERHUB_USERNAME>/zazzzor-backup`.
Public images require no login.

| Event | Tags |
|---|---|
| Successful CI on `main` | `sha-<12 commit characters>`, `main`, `latest` |
| Git tag `v1.2.3` on a `main` commit with successful CI | `sha-<...>`, `1.2.3`, `1.2` |
| Emergency publish from Actions → Images → Run workflow, with a reason | `sha-<...>` |

Deploy by SHA tag or version to pin a specific image and enable reliable
rollbacks. `main` and `latest` move between commits. Swarm with
`--resolve-image never` cannot detect these changes.

To get the tag for the current commit:

```bash
echo "sha-$(git rev-parse --short=12 HEAD)"
```

New GHCR packages are private by default. Make the package public in
GitHub → Packages → package → Package settings → Change visibility, or
sign in on the server with a token that has `read:packages`.

```bash
docker login ghcr.io -u whoaskedssselery
```

In the stack file, use the registry image, for example
`image: ghcr.io/whoaskedssselery/zazzzor-api:${API_TAG}` with
`API_TAG=sha-<...>`. Drop `--resolve-image never` so that Swarm pulls
the image and pins its digest.

---

## 17. Deploy behind Traefik (Docker Swarm)

Use `deploy/docker-stack.traefik.yml` if Traefik already runs in Swarm.
Use the same `stack.sh` commands. Traefik retains TLS and port 443.
The stack omits edge Caddy and DuckDNS. The web container serves the
client over HTTP on port 8080 inside the overlay network. The port stays
private. The stack uses `zazzzor_pgdata` and `zazzzor_apidata`.

> **Existing deployments.** If `docker stack ls` already lists a
> manually deployed `zazzzor` stack, read
> [Migrate from a manually deployed stack](#migrate-from-a-manually-deployed-stack).
> Reuse the existing `POSTGRES_PASSWORD` to keep access to the database.

1. On the manager, ensure the external overlay network `traefik` exists
   and Traefik is connected to it. Traefik must read Swarm service labels.
   The defaults are HTTPS entrypoint `websecure` and certresolver
   `letsencrypt`. If your names differ, uncomment and edit `TRAEFIK_NETWORK`,
   `TRAEFIK_ENTRYPOINT` and `TRAEFIK_CERTRESOLVER` in `deploy/.env`.
   Traefik v3 uses its Swarm provider (`--providers.swarm`) and reads
   `traefik.swarm.network`. Keep `traefik.docker.*` labels off these
   services. If both label sets are present, Traefik v3 skips the service
   and returns 404 (see `both Docker and Swarm labels are defined`).
   With Traefik v2 (`--providers.docker.swarmMode`), set the network
   through `--providers.docker.network`.
2. For a new installation, create `deploy/.env` as in section 5. Set
   `ZAZZZOR_DOMAIN` to the app domain and fill in `LIVEKIT_API_KEY`,
   `LIVEKIT_API_SECRET`, `POSTGRES_PASSWORD` and `ZAZZZOR_ADMIN_USERNAMES`.
   Leave `DUCKDNS_*` and `ACME_EMAIL` blank. Create the backup directory
   (section 12, step 1). In `deploy/.env`, **uncomment and edit** these
   existing lines.

   <!-- traefik-env -->
   ```bash
   ZAZZZOR_EDGE=traefik
   LIVEKIT_NODE_IP=<server public IPv4>
   TRAEFIK_SUBNET=<Traefik network CIDR>
   ZAZZZOR_API_REPOSITORY=ghcr.io/whoaskedssselery/zazzzor-api
   ZAZZZOR_CADDY_REPOSITORY=ghcr.io/whoaskedssselery/zazzzor-caddy
   ZAZZZOR_BACKUP_REPOSITORY=ghcr.io/whoaskedssselery/zazzzor-backup
   ```

   Edit the existing lines in place. Duplicate keys cause `stack.sh` to
   stop and report both line numbers.

   Find the Traefik network CIDR with:

   ```bash
   docker network inspect traefik --format '{{range .IPAM.Config}}{{.Subnet}} {{end}}'
   ```

   Use the Traefik subnet CIDR and avoid the broader `10.0.0.0/8`. The
   API trusts `X-Forwarded-For` only from this range. Leave Traefik's
   `forwardedHeaders.insecure` off.
   - A container on the `traefik` network is within the CIDR and can
     reach `api:8080` directly and spoof `X-Forwarded-For`. Attach only
     trusted services to that network.
   - Publish port 443 using `host` mode. In `ingress` mode Swarm
     replaces the client IP, so all your users share the same API limits.
     Check that port 443 has `"PublishMode":"host"` in this output.

     ```bash
     docker service inspect <traefik-service> --format '{{json .Endpoint.Ports}}'
     ```

   Postgres, metrics and Caddy's admin API stay private. Set
   `LIVEKIT_NODE_IP` explicitly. Media flows through `docker_gwbridge`
   (`rtc.ips.includes: 172.16.0.0/12`), and STUN does not override the
   address. If the public IP changes, update the variable and redeploy.
3. Save the previous tags for rollback. Deploy matching images published
   for one commit (section 16).

   ```bash
   export ZAZZZOR_API_TAG=sha-<12-character-commit-SHA>
   export ZAZZZOR_CADDY_TAG=sha-<same-12-character-commit-SHA>
   bash deploy/scripts/stack.sh deploy
   bash deploy/scripts/stack.sh ps
   bash deploy/scripts/stack.sh logs web --follow
   ```

   The script pulls missing GHCR images. For private images, run
   `docker login ghcr.io`. All services and local volumes are pinned to
   this manager, so only this node needs the images. Use immutable
   `sha-…` tags. For local builds, leave `*_REPOSITORY` settings commented
   out and leave the tags unset. The script uses content-based tags.
   `*_REPOSITORY` applies only with `ZAZZZOR_EDGE=traefik`. The Caddy
   edge mode rejects other repositories. Compose's `ZAZZZOR_API_IMAGE`
   still takes a complete image reference including its tag.
   Keep the output of `config` private. It contains secrets.
4. To update, pull the source, set the new tag pair and run `deploy`
   again. The `backup` service uses the scripts in `zazzzor-backup` with
   the API tag by default. Override it with `ZAZZZOR_BACKUP_TAG`.
   A stack deployed from a rendered file (for example through Portainer)
   does not need the checkout on the server. A failed update rolls back
   the affected service. To roll back manually, set the previous two
   tags and redeploy. Database migrations move forward only, so the older
   version must understand the current schema. See section 12 to restore
   data. `ps`, `logs <api|web|postgres|livekit|backup>` and `rm` work as in
   section 15. The external Traefik network survives removing the stack.
5. Configure MikroTik port forwarding and the firewall as in section 7.
   Send 443/TCP to Traefik and 7881/TCP plus 7882/UDP directly to the
   LiveKit node in host mode. Keep 5432, 8080, 7880 and 9464 private.
   `/rtc` routes to LiveKit and `/api/` routes to the API, including its
   gateway. The lower-priority web router handles the remaining paths.
6. If any friends are on networks that block UDP, such as a VPN or a
   restrictive network, enable TURN/TLS. Create a separate DNS record
   pointing to the same IP. Uncomment and edit in `deploy/.env`.

   <!-- traefik-turn-env -->
   ```bash
   LIVEKIT_TURN_ENABLED=true
   LIVEKIT_TURN_DOMAIN=<separate TURN domain>
   ```

   Redeploy. The script adds `docker-stack.traefik-turn.yml` with an SNI
   TCP router on `websecure`, TLS at Traefik and `external_tls: true` at
   LiveKit. The TURN listener on port 443 stays internal while Traefik
   serves public 443. Give TURN a different domain from the application
   so the TCP router leaves the app's HTTPS requests alone. Verify that
   TURN started.

   ```bash
   bash deploy/scripts/stack.sh logs livekit | grep 'Starting TURN server'
   ```

   Look for `"turn.portTLS": 443`. If the logs instead show
   `bind: permission denied`, the node's
   `net.ipv4.ip_unprivileged_port_start` differs from Docker's default
   of 0 and LiveKit lacks capabilities to bind 443. Report this to the
   developers.

   To turn it off, set `LIVEKIT_TURN_ENABLED=false` and redeploy. The
   deploy removes the TCP router labels. If TURN is active and the
   setting is commented out, `deploy` stops and asks for an explicit
   value.
7. Check `https://<domain>/api/v1/healthz`, the home page and sign-in.
   In DevTools → Network → WS, look for `/api/v1/gateway` with status
   101. Leave the tab open for several minutes and check that heartbeats
   continue. Then open `/connectivity-test/` and follow the connectivity
   test below, including on a network with blocked UDP if TURN is on.

   Traefik upgrades WebSockets automatically. The default
   `respondingTimeouts.readTimeout=60s` applies to reading the HTTP
   request. `writeTimeout=0s` leaves response writes unrestricted.
   Keep the gateway free of middleware with a request-wide deadline.
   If you override timeouts, check the `websecure` entrypoint and its
   `serversTransport`. Keep `writeTimeout=0s` and `responseHeaderTimeout=0s`,
   and set transport read and write timeouts to `0s` if configured.
   `idleConnTimeout` applies to idle keep-alive connections. The Traefik
   owner controls these settings outside this stack.
   [WebSocket](https://doc.traefik.io/traefik/user-guides/websocket/),
   [entrypoints](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/),
   [serversTransport](https://doc.traefik.io/traefik/reference/routing-configuration/http/load-balancing/serverstransport/).
8. Back up or restore as in section 12 through `stack.sh backup`,
   `stack.sh restore <backup> --yes` and `stack.sh reconcile`. With
   `ZAZZZOR_EDGE=traefik`, the jobs join the internal attachable
   `<STACK_NAME>_db` network and connect to `postgres:5432`. The network
   is private to the API, Postgres and backup services.
