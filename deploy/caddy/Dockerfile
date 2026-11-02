# syntax=docker/dockerfile:1
# Build context: repository root (see deploy/docker-compose.yml).

# Compose always passes CADDY_VERSION and DUCKDNS_PLUGIN_VERSION from
# deploy/.env; the defaults only keep a bare `docker build` valid. Keep them
# equal to .env.example.
ARG CADDY_VERSION=2.11.7
ARG NODE_VERSION=22.23.3-bookworm-slim
# Release tag of github.com/caddy-dns/duckdns (v0.5.0 targets libdns 1.x,
# which Caddy 2.10+ uses).
ARG DUCKDNS_PLUGIN_VERSION=v0.5.0

# 1. Static builds of the web client and the connectivity test page.
FROM node:${NODE_VERSION} AS web
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
WORKDIR /app
# Manifests first, so a change to sources only reuses the installed layer.
# .pnpmfile.cjs takes part in the lockfile check (it adjusts some manifests).
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .pnpmfile.cjs ./
COPY tools/connectivity-test/package.json ./tools/connectivity-test/
COPY web/package.json ./web/
RUN corepack enable && pnpm install --frozen-lockfile
COPY tools ./tools
COPY web ./web
RUN pnpm --filter @zazzzor/connectivity-test --filter @zazzzor/web build

# 2. Caddy with the DuckDNS DNS provider (for DNS-01 certificates).
FROM caddy:${CADDY_VERSION}-builder AS caddy-builder
ARG DUCKDNS_PLUGIN_VERSION
RUN xcaddy build --with github.com/caddy-dns/duckdns@${DUCKDNS_PLUGIN_VERSION}

# 3. Runtime image.
FROM caddy:${CADDY_VERSION}
COPY --from=caddy-builder /usr/bin/caddy /usr/bin/caddy
COPY --from=web /app/tools/connectivity-test/dist /srv/connectivity-test
COPY --from=web /app/web/dist /srv/web
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
COPY deploy/caddy/Caddyfile.web /etc/caddy/Caddyfile.web
COPY deploy/caddy/static.caddy /etc/caddy/static.caddy
