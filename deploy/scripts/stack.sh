#!/usr/bin/env bash
# Zazzzor в Docker Swarm: сборка образов и `docker stack deploy`.
#
# Использование (из корня репозитория):
#   bash deploy/scripts/stack.sh deploy        собрать образы и развернуть/обновить стек
#   bash deploy/scripts/stack.sh build         только собрать образы (печатает их теги)
#   bash deploy/scripts/stack.sh config        итоговая конфигурация (в ней секреты, не публикуйте)
#   bash deploy/scripts/stack.sh ps            задачи стека и их состояние
#   bash deploy/scripts/stack.sh logs <сервис> [флаги docker service logs, например --follow]
#                                              сервисы: api, postgres, livekit, backup и
#                                              caddy, duckdns (edge) или web (traefik)
#   bash deploy/scripts/stack.sh rm            остановить стек (данные в томах остаются)
#   bash deploy/scripts/stack.sh backup        снять резервную копию сейчас
#   bash deploy/scripts/stack.sh restore <копия> --yes   заменить все данные копией
#   bash deploy/scripts/stack.sh restore <копия> --yes --resume   продолжить восстановление
#   bash deploy/scripts/stack.sh reconcile     сверить изображения с базой
#
# Вариант: ZAZZZOR_EDGE=caddy (по умолчанию) или traefik (deploy/README.md §17).
# GHCR: ZAZZZOR_API_REPOSITORY / ZAZZZOR_CADDY_REPOSITORY и оба *_TAG.
# Настройки: deploy/.env (другой файл: ENV_FILE=...). Имя стека: zazzzor
# (другое: STACK_NAME=... — но тогда и тома другие, то есть база пустая).
# Каталог копий ZAZZZOR_BACKUP_DIR должен существовать и принадлежать UID
# 65532; deploy это проверяет и печатает команду, которая это исправит.
# Тег образа — хеш его содержимого (слои и конфигурация), поэтому deploy
# обновляет сервис, только если образ действительно изменился. Уже собранные
# образы можно развернуть без сборки: ZAZZZOR_API_TAG=... ZAZZZOR_CADDY_TAG=...
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy_dir="$(cd "$script_dir/.." && pwd)"
repo_dir="$(cd "$deploy_dir/.." && pwd)"
env_file="${ENV_FILE:-$deploy_dir/.env}"
stack="${STACK_NAME:-zazzzor}"
stack_files=(-c "$deploy_dir/docker-stack.yml")

# shellcheck source=deploy/scripts/lib.sh
source "$script_dir/lib.sh"

# Версия для `zazzzor --version`. Тег образа от неё не зависит, поэтому
# ошибка git (например, запуск через sudo: «dubious ownership») не мешает
# обновлению, а лишь даёт версию unknown.
app_version() {
  git -c safe.directory="$repo_dir" -C "$repo_dir" describe --tags --always --dirty 2>/dev/null || echo unknown
}

require_swarm() {
  local state
  state="$(docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null || true)"
  if [[ "$state" != active ]]; then
    die "Docker не в режиме Swarm (состояние: ${state:-неизвестно}). Включите его: docker swarm init (если у сервера несколько адресов — docker swarm init --advertise-addr <IP в локальной сети>)"
  fi
  if [[ "$(docker info --format '{{.Swarm.ControlAvailable}}')" != true ]]; then
    die "стек разворачивается с узла-менеджера Swarm, а этот узел — рабочий"
  fi
}

# build_image <имя> <аргументы docker build...>: собирает образ и печатает тег
# по его содержимому. ID образа для этого не годится: в нём время сборки и
# метаданные, и он меняется при каждой сборке, даже из кэша. Слои и конфигурация
# образа зависят только от содержимого.
build_image() {
  local name="$1" tag
  shift
  docker build -t "$name:building" "$@" >&2
  tag="$(docker image inspect --format '{{json .RootFS.Layers}}{{json .Config}}' "$name:building" |
    sha256sum | cut -c1-12)"
  docker tag "$name:building" "$name:$tag"
  docker rmi "$name:building" >/dev/null
  printf '%s' "$tag"
}

# Стандартные ли имена образов. Сборка создаёт только их, и только их знает
# edge-стек docker-stack.yml.
default_repositories() {
  [[ "${ZAZZZOR_API_REPOSITORY:-zazzzor-api}" == zazzzor-api && "${ZAZZZOR_CADDY_REPOSITORY:-zazzzor-caddy}" == zazzzor-caddy &&
    "${ZAZZZOR_BACKUP_REPOSITORY:-zazzzor-backup}" == zazzzor-backup ]]
}

build() {
  default_repositories || die "для образов GHCR задайте оба тега; сборка создаёт только локальные образы"
  local version
  version="$(app_version)"
  echo "Сборка образов (версия $version)…" >&2
  ZAZZZOR_API_TAG="$(build_image zazzzor-api -f "$repo_dir/server/Dockerfile" \
    --build-arg "VERSION=$version" "$repo_dir/server")"
  ZAZZZOR_CADDY_TAG="$(build_image zazzzor-caddy -f "$deploy_dir/caddy/Dockerfile" \
    --build-arg "CADDY_VERSION=${CADDY_VERSION:?заполните CADDY_VERSION в $env_file}" \
    --build-arg "DUCKDNS_PLUGIN_VERSION=${DUCKDNS_PLUGIN_VERSION:?заполните DUCKDNS_PLUGIN_VERSION в $env_file}" \
    "$repo_dir")"
  ZAZZZOR_BACKUP_TAG="$(build_image zazzzor-backup -f "$deploy_dir/backup/Dockerfile" \
    --build-arg "POSTGRES_VERSION=${POSTGRES_VERSION:?заполните POSTGRES_VERSION в $env_file}" "$deploy_dir")"
  export ZAZZZOR_API_TAG ZAZZZOR_CADDY_TAG ZAZZZOR_BACKUP_TAG
  echo "Образы: zazzzor-api:$ZAZZZOR_API_TAG zazzzor-caddy:$ZAZZZOR_CADDY_TAG zazzzor-backup:$ZAZZZOR_BACKUP_TAG" >&2
}

# Образы из окружения (ZAZZZOR_API_TAG, ZAZZZOR_CADDY_TAG) или сборка.
# ZAZZZOR_BACKUP_TAG без значения берётся равным ZAZZZOR_API_TAG: реестр
# публикует все три образа под одним тегом. Образ копий нужен только стеку
# за Traefik; edge-стек читает скрипты из этого каталога.
images() {
  if [[ -n "${ZAZZZOR_API_TAG:-}" || -n "${ZAZZZOR_CADDY_TAG:-}" ]]; then
    [[ -n "${ZAZZZOR_API_TAG:-}" && -n "${ZAZZZOR_CADDY_TAG:-}" ]] ||
      die "задайте оба тега: ZAZZZOR_API_TAG и ZAZZZOR_CADDY_TAG"
    ZAZZZOR_BACKUP_TAG="${ZAZZZOR_BACKUP_TAG:-$ZAZZZOR_API_TAG}"
    local image
    local -a wanted=("${ZAZZZOR_API_REPOSITORY:-zazzzor-api}:$ZAZZZOR_API_TAG" "${ZAZZZOR_CADDY_REPOSITORY:-zazzzor-caddy}:$ZAZZZOR_CADDY_TAG")
    [[ "$ZAZZZOR_EDGE" != traefik ]] || wanted+=("${ZAZZZOR_BACKUP_REPOSITORY:-zazzzor-backup}:$ZAZZZOR_BACKUP_TAG")
    for image in "${wanted[@]}"; do
      if ! docker image inspect "$image" >/dev/null 2>&1; then
        # Имя с «/» — образ из реестра (GHCR, Docker Hub): его можно скачать.
        if [[ "${image%:*}" == */* ]]; then
          docker pull "$image"
        else
          die "нет образа $image (bash deploy/scripts/stack.sh build)"
        fi
      fi
    done
    export ZAZZZOR_API_TAG ZAZZZOR_CADDY_TAG ZAZZZOR_BACKUP_TAG
  else
    build
  fi
}

# Выбирает вариант стека и вычисляет значения, которые Swarm не подставляет.
prepare() {
  load_env
  case "${ZAZZZOR_EDGE:-caddy}" in
    caddy)
      default_repositories || die "переопределение репозиториев доступно только при ZAZZZOR_EDGE=traefik; для caddy оставьте стандартные имена"
      ;;
    traefik)
      stack_files=(-c "$deploy_dir/docker-stack.traefik.yml")
      [[ -n "${LIVEKIT_NODE_IP:-}" ]] || die "задайте LIVEKIT_NODE_IP: внешний IPv4 для медиа"
      [[ -n "${TRAEFIK_SUBNET:-}" ]] || die "задайте TRAEFIK_SUBNET: CIDR внешней сети Traefik"
      case "${LIVEKIT_TURN_ENABLED:-false}" in
        true)
          [[ -n "${LIVEKIT_TURN_DOMAIN:-}" && "$LIVEKIT_TURN_DOMAIN" != "$ZAZZZOR_DOMAIN" ]] || die "задайте отдельный LIVEKIT_TURN_DOMAIN для TURN"
          stack_files+=(-c "$deploy_dir/docker-stack.traefik-turn.yml")
          ;;
        false) ;;
        *) die "LIVEKIT_TURN_ENABLED должен быть true или false" ;;
      esac
      ;;
    *) die "ZAZZZOR_EDGE должен быть caddy или traefik" ;;
  esac
  export ZAZZZOR_EDGE="${ZAZZZOR_EDGE:-caddy}"
  ZAZZZOR_NODE_ID="$(docker info --format '{{.Swarm.NodeID}}')"
  export ZAZZZOR_NODE_ID
  # docker stack has no nested defaults or ${VAR:+...}; compute them here.
  [[ -n "${ZAZZZOR_HTTPS_PORT:-}" ]] || die "заполните ZAZZZOR_HTTPS_PORT в $env_file (обычно 443)"
  export ZAZZZOR_PUBLIC_PORT="${ZAZZZOR_PUBLIC_PORT:-$ZAZZZOR_HTTPS_PORT}"
  [[ -n "${ZAZZZOR_BACKUP_DIR:-}" ]] || die "заполните ZAZZZOR_BACKUP_DIR в $env_file (каталог копий на хосте, см. deploy/README.md)"
  # The backup service mounts the job scripts from this checkout.
  export ZAZZZOR_DEPLOY_DIR="$deploy_dir"
  if [[ -n "${LIVEKIT_NODE_IP:-}" ]]; then
    export LIVEKIT_USE_EXTERNAL_IP=false
  else
    export LIVEKIT_USE_EXTERNAL_IP=true
  fi
}

# TURN выключен по умолчанию, но у ручного стека и прежних deploy он может
# работать, и на него рассчитывают друзья за VPN. Если LIVEKIT_TURN_ENABLED
# не задан, а у работающего livekit есть TCP-router TURN, deploy не выключает
# TURN молча и просит решить явно, до любых изменений.
require_turn_decision() {
  [[ "$ZAZZZOR_EDGE" == traefik && -z "${LIVEKIT_TURN_ENABLED:-}" ]] || return 0
  local service="${stack}_livekit" services rule
  services="$(docker service ls --filter "name=$service" --format '{{.Name}}')"
  grep -Fxq -- "$service" <<<"$services" || return 0
  rule="$(docker service inspect --format '{{index .Spec.Labels "traefik.tcp.routers.zazzzor-turn.rule"}}' "$service")"
  [[ -z "$rule" ]] || die "в работающем стеке включён TURN ($rule), а LIVEKIT_TURN_ENABLED в $env_file не задан. Чтобы сохранить TURN, раскомментируйте LIVEKIT_TURN_ENABLED=true и LIVEKIT_TURN_DOMAIN=<домен из правила>; чтобы выключить, задайте LIVEKIT_TURN_ENABLED=false (deploy/README.md §17). Стек не изменён"
}

cmd="${1:-}"
case "$cmd" in
  deploy)
    require_swarm
    prepare
    require_turn_decision
    images
    require_backup_dir "$ZAZZZOR_BACKUP_DIR"
    echo "Развёртывание стека $stack…"
    # Образы уже подготовлены images(): локальные или скачанные из GHCR.
    # --detach=false ждёт, пока задачи запустятся (и пройдут healthcheck), и
    # показывает прогресс.
    docker stack deploy --prune --resolve-image never --detach=false \
      "${stack_files[@]}" "$stack"
    docker stack ps "$stack" --filter desired-state=running \
      --format 'table {{.Name}}\t{{.CurrentState}}\t{{.Error}}'
    ;;
  backup)
    exec bash "$script_dir/backup.sh" swarm backup
    ;;
  restore)
    exec bash "$script_dir/backup.sh" swarm restore "${@:2}"
    ;;
  reconcile)
    exec bash "$script_dir/backup.sh" swarm reconcile
    ;;
  build)
    prepare
    build
    ;;
  config)
    prepare
    export ZAZZZOR_API_TAG="${ZAZZZOR_API_TAG:-<собирается при deploy>}"
    export ZAZZZOR_CADDY_TAG="${ZAZZZOR_CADDY_TAG:-<собирается при deploy>}"
    export ZAZZZOR_BACKUP_TAG="${ZAZZZOR_BACKUP_TAG:-$ZAZZZOR_API_TAG}"
    # Без Swarm (например, в CI) у узла нет ID: показываем заглушку.
    export ZAZZZOR_NODE_ID="${ZAZZZOR_NODE_ID:-<ID этого узла Swarm>}"
    docker stack config "${stack_files[@]}"
    ;;
  ps)
    docker stack ps "$stack" --format 'table {{.Name}}\t{{.DesiredState}}\t{{.CurrentState}}\t{{.Error}}'
    ;;
  logs)
    [[ -n "${2:-}" ]] || die "укажите сервис: api, postgres, livekit, backup, а также caddy и duckdns (edge) или web (traefik)"
    docker service logs --raw --tail "${TAIL:-200}" "${@:3}" "${stack}_$2"
    ;;
  rm)
    docker stack rm "$stack"
    # Ждём, пока контейнеры и сети стека действительно исчезнут: иначе
    # следующий запуск (compose или новый deploy) упрётся в ещё занятые порты.
    # --detach=false этого не гарантирует: он возвращается раньше.
    for _ in $(seq 1 60); do
      if [[ -z "$(docker ps -q --filter "label=com.docker.stack.namespace=$stack")" &&
            -z "$(docker network ls -q --filter "label=com.docker.stack.namespace=$stack")" ]]; then
        echo "Стек $stack остановлен."
        exit 0
      fi
      sleep 1
    done
    die "стек $stack не остановился за 60 секунд: docker ps --filter label=com.docker.stack.namespace=$stack"
    ;;
  *)
    sed -n '2,25p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
