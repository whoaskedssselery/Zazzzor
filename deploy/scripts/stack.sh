#!/usr/bin/env bash
# Zazzzor в Docker Swarm: сборка образов и `docker stack deploy`.
#
# Использование (из корня репозитория):
#   bash deploy/scripts/stack.sh deploy        собрать образы и развернуть/обновить стек
#   bash deploy/scripts/stack.sh build         только собрать образы (печатает их теги)
#   bash deploy/scripts/stack.sh config        итоговая конфигурация (в ней секреты, не публикуйте)
#   bash deploy/scripts/stack.sh ps            задачи стека и их состояние
#   bash deploy/scripts/stack.sh logs <сервис> [флаги docker service logs, например --follow]
#                                              сервисы: caddy, livekit, duckdns, postgres, api
#   bash deploy/scripts/stack.sh rm            остановить стек (данные в томах остаются)
#
# Настройки: deploy/.env (другой файл: ENV_FILE=...). Имя стека: zazzzor
# (другое: STACK_NAME=... — но тогда и тома другие, то есть база пустая).
# Тег образа — хеш его содержимого (слои и конфигурация), поэтому deploy
# обновляет сервис, только если образ действительно изменился. Уже собранные
# образы можно развернуть без сборки: ZAZZZOR_API_TAG=... ZAZZZOR_CADDY_TAG=...
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy_dir="$(cd "$script_dir/.." && pwd)"
repo_dir="$(cd "$deploy_dir/.." && pwd)"
env_file="${ENV_FILE:-$deploy_dir/.env}"
stack="${STACK_NAME:-zazzzor}"
stack_file="$deploy_dir/docker-stack.yml"

die() { echo "Ошибка: $*" >&2; exit 2; }

# Читает deploy/.env так же, как docker compose, но не исполняет его:
# KEY=VALUE, необязательный "export ", пробелы вокруг ключа и значения,
# кавычки, комментарий " #..." после значения без кавычек. Переменные, уже
# заданные в окружении, как и в compose, важнее файла.
load_env() {
  [[ -f "$env_file" ]] || die "не найден файл настроек $env_file (cp deploy/.env.example deploy/.env)"
  local line key value n=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    n=$((n + 1))
    line="${line%$'\r'}"
    [[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
    if [[ ! "$line" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=[[:space:]]*(.*)$ ]]; then
      die "$env_file, строка $n: ожидается ИМЯ=значение"
    fi
    key="${BASH_REMATCH[2]}"
    value="${BASH_REMATCH[3]}"
    if [[ "$value" =~ ^\"(.*)\"[[:space:]]*(#.*)?$ || "$value" =~ ^\'(.*)\'[[:space:]]*(#.*)?$ ]]; then
      value="${BASH_REMATCH[1]}"
    else
      value="${value%%[[:space:]]#*}"
      value="${value%"${value##*[![:space:]]}"}"
    fi
    [[ -n "${!key+set}" ]] && continue
    export "$key=$value"
  done < "$env_file"
}

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

build() {
  local version
  version="$(app_version)"
  echo "Сборка образов (версия $version)…" >&2
  ZAZZZOR_API_TAG="$(build_image zazzzor-api -f "$repo_dir/server/Dockerfile" \
    --build-arg "VERSION=$version" "$repo_dir/server")"
  ZAZZZOR_CADDY_TAG="$(build_image zazzzor-caddy -f "$deploy_dir/caddy/Dockerfile" \
    --build-arg "CADDY_VERSION=${CADDY_VERSION:?заполните CADDY_VERSION в $env_file}" \
    --build-arg "DUCKDNS_PLUGIN_VERSION=${DUCKDNS_PLUGIN_VERSION:?заполните DUCKDNS_PLUGIN_VERSION в $env_file}" \
    "$repo_dir")"
  export ZAZZZOR_API_TAG ZAZZZOR_CADDY_TAG
  echo "Образы: zazzzor-api:$ZAZZZOR_API_TAG zazzzor-caddy:$ZAZZZOR_CADDY_TAG" >&2
}

# Образы из окружения (ZAZZZOR_API_TAG, ZAZZZOR_CADDY_TAG) или сборка.
images() {
  if [[ -n "${ZAZZZOR_API_TAG:-}" || -n "${ZAZZZOR_CADDY_TAG:-}" ]]; then
    [[ -n "${ZAZZZOR_API_TAG:-}" && -n "${ZAZZZOR_CADDY_TAG:-}" ]] ||
      die "задайте оба тега: ZAZZZOR_API_TAG и ZAZZZOR_CADDY_TAG"
    local image
    for image in "zazzzor-api:$ZAZZZOR_API_TAG" "zazzzor-caddy:$ZAZZZOR_CADDY_TAG"; do
      docker image inspect "$image" >/dev/null 2>&1 || die "нет образа $image (bash deploy/scripts/stack.sh build)"
    done
    export ZAZZZOR_API_TAG ZAZZZOR_CADDY_TAG
  else
    build
  fi
}

prepare() {
  load_env
  ZAZZZOR_NODE_ID="$(docker info --format '{{.Swarm.NodeID}}')"
  export ZAZZZOR_NODE_ID
  # docker stack has no nested defaults or ${VAR:+...}; compute them here.
  [[ -n "${ZAZZZOR_HTTPS_PORT:-}" ]] || die "заполните ZAZZZOR_HTTPS_PORT в $env_file (обычно 443)"
  export ZAZZZOR_PUBLIC_PORT="${ZAZZZOR_PUBLIC_PORT:-$ZAZZZOR_HTTPS_PORT}"
  if [[ -n "${LIVEKIT_NODE_IP:-}" ]]; then
    export LIVEKIT_USE_EXTERNAL_IP=false
  else
    export LIVEKIT_USE_EXTERNAL_IP=true
  fi
}

cmd="${1:-}"
case "$cmd" in
  deploy)
    require_swarm
    prepare
    images
    echo "Развёртывание стека $stack…"
    # Образы локальные: реестр не спрашиваем. --detach=false ждёт, пока
    # задачи запустятся (и пройдут healthcheck), и показывает прогресс.
    docker stack deploy --prune --resolve-image never --detach=false \
      -c "$stack_file" "$stack"
    docker stack ps "$stack" --filter desired-state=running \
      --format 'table {{.Name}}\t{{.CurrentState}}\t{{.Error}}'
    ;;
  build)
    prepare
    build
    ;;
  config)
    prepare
    export ZAZZZOR_API_TAG="${ZAZZZOR_API_TAG:-<собирается при deploy>}"
    export ZAZZZOR_CADDY_TAG="${ZAZZZOR_CADDY_TAG:-<собирается при deploy>}"
    # Без Swarm (например, в CI) у узла нет ID: показываем заглушку.
    export ZAZZZOR_NODE_ID="${ZAZZZOR_NODE_ID:-<ID этого узла Swarm>}"
    docker stack config -c "$stack_file"
    ;;
  ps)
    docker stack ps "$stack" --format 'table {{.Name}}\t{{.DesiredState}}\t{{.CurrentState}}\t{{.Error}}'
    ;;
  logs)
    [[ -n "${2:-}" ]] || die "укажите сервис: caddy, livekit, duckdns, postgres или api"
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
    sed -n '2,17p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
