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

# 1. Static build of the connectivity test page.
FROM node:${NODE_VERSION} AS web
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
WORKDIR /app
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY tools ./tools
RUN corepack enable \
  && pnpm install --frozen-lockfile \
  && pnpm --filter @zazzzor/connectivity-test build

# 2. Caddy with the DuckDNS DNS provider (for DNS-01 certificates).
FROM caddy:${CADDY_VERSION}-builder AS caddy-builder
ARG DUCKDNS_PLUGIN_VERSION
RUN xcaddy build --with github.com/caddy-dns/duckdns@${DUCKDNS_PLUGIN_VERSION}

# 3. Runtime image.
FROM caddy:${CADDY_VERSION}
COPY --from=caddy-builder /usr/bin/caddy /usr/bin/caddy
COPY --from=web /app/tools/connectivity-test/dist /srv/connectivity-test
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
