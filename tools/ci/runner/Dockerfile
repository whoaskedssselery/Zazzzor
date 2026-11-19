FROM ghcr.io/actions/actions-runner:2.338.2
USER root

# The runner's bundled Node also runs Playwright while building the image.
ENV PATH="/home/runner/externals/node24/bin:${PATH}"
RUN apt-get update && apt-get install -y --no-install-recommends \
    gh build-essential unzip jq zstd xz-utils \
    && rm -rf /var/lib/apt/lists/*
COPY --from=docker/compose-bin:v5.6.1 /docker-compose /usr/local/lib/docker/cli-plugins/docker-compose

# Resolve the exact version from the committed lockfile, not npm's latest.
COPY pnpm-lock.yaml /tmp/runner/pnpm-lock.yaml
RUN version="$(node -e 'const fs = require("node:fs"); const lock = fs.readFileSync("/tmp/runner/pnpm-lock.yaml", "utf8"); const versions = [...new Set([...lock.matchAll(/^  playwright-core@(\d+\.\d+\.\d+):/gm)].map(m => m[1]))]; if (versions.length !== 1) throw new Error("expected one locked playwright-core version"); process.stdout.write(versions[0]);')" \
    && npx --yes --package="playwright-core@$version" playwright-core install-deps chromium firefox \
    && mkdir -p /opt/zazzzor-runner \
    && printf '%s\n' "$version" > /opt/zazzzor-runner/playwright-version \
    && rm -rf /tmp/runner /root/.npm /var/lib/apt/lists/*

RUN echo 'root ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers
ENV RUNNER_ALLOW_RUNASROOT=1
COPY tools/ci/runner/start.sh /start.sh
RUN chmod +x /start.sh
ENTRYPOINT ["/start.sh"]
