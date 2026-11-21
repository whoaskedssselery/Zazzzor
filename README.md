# Zazzzor

English · [Русский](README.ru.md)

[![CI](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/ci.yml)
[![Images](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/images.yml/badge.svg)](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/images.yml)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A Discord of your own for a group of friends, on one home server: servers
and channels, chat, voice and 1080p60 screen sharing, with no cloud
services and no VPN. Everything runs in Docker; images are published to
Docker Hub.

Download the Windows desktop client from [Releases](https://github.com/whoaskedssselery/Zazzzor/releases). See the [Windows installation guide](deploy/desktop.md).

## Features

**Talking**
- Servers with text and voice channels, categories and invites.
- Messages with replies, edits, deletion, reactions, mentions, emoji,
  attachments and link previews; message search.
- Friends, direct messages and group chats; blocking.
- Typing indicators, unread state, mention notifications with sound.

**Voice and screen**
- Voice channels and private calls that ring the other side.
- Screen sharing up to 1080p at 60 fps, dropping to 720p when 30 fps can't
  be held; tab audio in Chrome and Edge.
- Volume for each person and each screen share, from 0 to 200 %.
- Noise suppression on the user's own computer: DeepFilterNet3, with
  RNNoise as the fallback.
- A speaking ring driven by the sound actually heard; join, leave and
  mute sounds.

**Running it**
- Roles and permissions per server and per channel, an audit log.
- Instance administration: users, registration, settings.
- One Docker stack: API, Postgres, LiveKit and Caddy with HTTPS.
- Scheduled backups with a tested restore.
- Images for every green commit on `main`.

## Stack

| Part | Technologies |
|---|---|
| Server | Go (modular monolith), PostgreSQL, sqlc, goose, WebSocket ([coder/websocket](https://github.com/coder/websocket)) |
| Voice and video | [LiveKit](https://livekit.io) (SFU, TURN) |
| Web client | React 19, TypeScript, Vite, TanStack Router and Query, Tailwind CSS; Feature-Sliced Design |
| Contract | OpenAPI 3.1 in `api/openapi.yaml`: the client's types are generated from it, and the server's tests check every response against it |
| Deployment | Docker Compose or Swarm, Caddy (Let's Encrypt), Traefik |
| Checks | Go race tests against real Postgres (testcontainers), Vitest, Playwright in Chromium and Firefox, govulncheck, gitleaks |

## Status

| Stage | What | State |
|---|---|---|
| 1.1 | Connectivity check from friends' networks: LiveKit, Caddy, a test page | done |
| 1.2 | Backend and web client: servers, channels, chat, roles, administration | done |
| 1.3 | Voice, calls and screen sharing in the browser | done, used on the live server |
| 1.4 | Windows desktop client (Tauri, native screen and audio capture) | released |

The web client runs in current Chrome, Edge and Firefox. Browsers share
screen audio only from a Chrome or Edge tab; full window and game audio
comes with the desktop client.

## Layout

| Path | What |
|---|---|
| [`server/`](server) | API and realtime gateway (`cmd/zazzzor`, modules in `internal/`) |
| [`web/`](web) | Web client (`@zazzzor/web`), Playwright scenarios in `web/e2e/` |
| [`api/openapi.yaml`](api/openapi.yaml) | The REST and gateway contract |
| [`deploy/`](deploy) | Docker stacks, Caddy, the backup image, scripts and the [server guide](deploy/README.md) |
| [`tools/connectivity-test/`](tools/connectivity-test) | A page that checks the connection to the server |
| [`tools/agents/`](tools/agents) | The shared setup for coding agents: rules, checks, hooks, skills |

## Running a server

Step by step, from installing Docker to the first administrator and
backups: [deploy/README.md](deploy/README.md). Ready-made images:

```
ssselery/zazzzor-api
ssselery/zazzzor-caddy
ssselery/zazzzor-backup
```

tagged `sha-<commit>`, `main` and `latest`; the same images are in
`ghcr.io/whoaskedssselery/`.

## Development

You need Go (the version in `server/go.mod`), Node 22 with pnpm
(`corepack enable`) and Docker: the server's tests start a real Postgres.

### Server

```bash
cd server
go vet ./...
go test ./...
```

Running it locally with Postgres in Docker (port 55432, so it doesn't
clash with another Postgres on 5432):

```bash
docker run -d --name zazzzor-dev-pg -e POSTGRES_USER=zazzzor -e POSTGRES_PASSWORD=dev \
  -e POSTGRES_DB=zazzzor -p 127.0.0.1:55432:5432 postgres:18.6-alpine

cd server
ZAZZZOR_DATABASE_URL='postgres://zazzzor:dev@127.0.0.1:55432/zazzzor?sslmode=disable' \
ZAZZZOR_ALLOWED_ORIGINS=http://localhost:5173 \
ZAZZZOR_COOKIE_SECURE=false \
ZAZZZOR_DATA_DIR=/tmp/zazzzor-data \
go run ./cmd/zazzzor
```

The server listens on `:8080` (API) and `:9090` (Prometheus metrics) and
applies its migrations itself. Check it with
`curl http://127.0.0.1:8080/api/v1/readyz`, or end to end with
`go run ./cmd/zazzzor smoke http://127.0.0.1:8080`. The other settings are
described in `server/internal/config`.

### Web client

From the repository root, next to a running server:

```bash
pnpm install
pnpm dev        # http://localhost:5173
```

Vite forwards `/api` and the gateway's WebSocket to `http://127.0.0.1:8080`,
so the server must run with `ZAZZZOR_ALLOWED_ORIGINS=http://localhost:5173`
and `ZAZZZOR_COOKIE_SECURE=false`. Open `http://localhost:5173` exactly.

The checks CI runs too:

```bash
pnpm lint       # ESLint
pnpm test       # Vitest in every package
pnpm build      # type check and build
pnpm gen:api    # regenerate the client types from api/openapi.yaml
```

After changing `api/openapi.yaml`, run `pnpm gen:api` and commit
`web/src/shared/api/schema.gen.ts`: CI compares it with the contract.

### Browser tests

Playwright scenarios run in Chromium and Firefox against the built client,
the real API and LiveKit. From `web/`:

```bash
pnpm exec playwright install chromium firefox   # once
pnpm e2e:up     # Postgres, API and LiveKit from web/e2e/compose.yml
pnpm e2e        # build, vite preview with Caddy's CSP, every scenario
pnpm e2e:down   # remove the stack with its database
```

### Coding agents

The repository carries a shared setup for Claude Code and Codex: the rules
in [`AGENTS.md`](AGENTS.md), the house style, a check script, git hooks and
pinned skills. After a clone:

```bash
bash tools/agents/setup.sh
```

Details: [`tools/agents/README.md`](tools/agents/README.md).

## License

[Apache License 2.0](LICENSE).
