# The backup job as an image: Postgres (pg_dump, pg_restore, bash) with the
# job scripts inside, for stacks deployed without this checkout on the
# server (Portainer). Build context: deploy/.
#
#   docker build -f deploy/backup/Dockerfile --build-arg POSTGRES_VERSION=<as in .env> deploy
ARG POSTGRES_VERSION
FROM postgres:${POSTGRES_VERSION}
COPY --chmod=0755 scripts/backup-job.sh scripts/lib.sh /opt/backup/
