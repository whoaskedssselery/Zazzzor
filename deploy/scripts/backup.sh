#!/usr/bin/env bash
# Резервная копия Zazzzor сейчас и восстановление из копии, с хоста. Сама
# работа идёт в задании backup (deploy/scripts/backup-job.sh) в образе
# Postgres; здесь — запуск задания, остановка и запуск API.
#
# Использование (из корня репозитория):
#   bash deploy/scripts/backup.sh compose backup                   копия сейчас (docker compose)
#   bash deploy/scripts/backup.sh compose restore <копия> --yes     заменить данные копией
#   bash deploy/scripts/backup.sh swarm backup                     то же для стека
#   bash deploy/scripts/backup.sh swarm restore <копия> --yes       (обычно через stack.sh)
#   bash deploy/scripts/backup.sh compose reconcile                сверить изображения с базой
#   bash deploy/scripts/backup.sh swarm reconcile                  (при работающем API)
# <копия> — имя каталога копии или путь к нему, относительный или
# абсолютный, со слешем в конце или без. Копия должна лежать прямо в
# ZAZZZOR_BACKUP_DIR: только этот каталог виден заданию.
#
# Восстановление: проверка сумм и схемы (API ещё работает), остановка API,
# база и изображения, files reconcile, запуск API. Без --yes ничего не
# делает. Если данные не изменились (база откатилась, блокировка занята),
# API запускается снова; если изменились частично, остаётся остановленным:
# повторите ту же команду. Если не удалась только сверка, API запускается.
# В обоих случаях код выхода не 0.
#
# Настройки: deploy/.env (другой файл: ENV_FILE=...). Имя проекта compose
# или стека: zazzzor (другое: STACK_NAME=...). ZAZZZOR_COMPOSE_OVERRIDE=<файл>
# добавляет ещё один compose-файл (так делает restore-test.sh).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy_dir="$(cd "$script_dir/.." && pwd)"
env_file="${ENV_FILE:-$deploy_dir/.env}"
# shellcheck source=deploy/scripts/lib.sh
source "$script_dir/lib.sh"

usage() {
  sed -n '2,26p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 2
}

mode="${1:-}"
action="${2:-}"
[[ "$mode" == compose || "$mode" == swarm ]] || usage
case "$action" in
  backup | reconcile)
    [[ $# -eq 2 ]] || usage
    ;;
  restore)
    [[ $# -eq 4 && "$4" == --yes ]] || die "restore заменяет все данные сервера копией; подтвердите: backup.sh $mode restore <копия> --yes"
    ;;
  *)
    usage
    ;;
esac

load_env
stack="${STACK_NAME:-${COMPOSE_PROJECT_NAME:-zazzzor}}"
[[ "$stack" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || die "неверное имя проекта: $stack"
[[ -n "${ZAZZZOR_BACKUP_DIR:-}" ]] || die "заполните ZAZZZOR_BACKUP_DIR в $env_file (каталог копий на хосте)"
# До любого запуска задания: Docker создал бы недостающий каталог от root.
# Проверяется только владелец (stat), внутрь каталога хост не заглядывает.
# Права UID есть только на Linux; Docker Desktop их не применяет.
if [[ "$(uname -s)" == Linux ]]; then
  require_backup_dir "$ZAZZZOR_BACKUP_DIR"
fi
volume="${stack}_apidata"
pg_image="postgres:${POSTGRES_VERSION:?заполните POSTGRES_VERSION в $env_file}"
# Чтобы Git Bash на Windows не превращал пути внутри контейнера (/opt/backup,
# /data) в пути Windows (на Linux ничего не меняет).
export MSYS2_ARG_CONV_EXCL='/zazzzor;/bin/bash;/opt/backup;/data;/backups'
compose=(docker compose --env-file "$env_file" -p "$stack" -f "$deploy_dir/docker-compose.yml")
if [[ -n "${ZAZZZOR_COMPOSE_OVERRIDE:-}" ]]; then
  compose+=(-f "$ZAZZZOR_COMPOSE_OVERRIDE")
fi

if [[ "$mode" == swarm ]]; then
  [[ "$(docker info --format '{{.Swarm.ControlAvailable}}')" == true ]] || die "запускайте на узле-менеджере Swarm, где работает стек $stack"
  # Тома и сервисы стека привязаны к этому узлу (stack.sh), задание тоже.
  if [[ -z "$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_postgres")" ]]; then
    die "Postgres стека $stack не работает на этом узле: bash deploy/scripts/stack.sh ps"
  fi
  ZAZZZOR_APP_IMAGE="$(docker service inspect --format '{{.Spec.TaskTemplate.ContainerSpec.Image}}' "${stack}_api")"
  export ZAZZZOR_APP_IMAGE
fi

# run_job <команда> [имя] запускает backup-job.sh. Только restore получает
# том изображений на запись.
run_job() {
  local access=ro
  if [[ "$1" == restore ]]; then
    access=rw
  fi
  if [[ "$mode" == compose ]]; then
    local files=()
    if [[ "$access" == rw ]]; then
      files=(-f "$deploy_dir/docker-restore.yml")
    fi
    "${compose[@]}" "${files[@]}" run --rm --no-deps -T -e "ZAZZZOR_SCHEMA_VERSION=${schema:-}" backup "$@"
  else
    docker run --rm --user "$backup_uid:$backup_uid" --network host \
      -e PGHOST=127.0.0.1 -e "PGPORT=${POSTGRES_PORT:-55432}" -e PGUSER=zazzzor -e PGDATABASE=zazzzor \
      -e "PGPASSWORD=${POSTGRES_PASSWORD:?заполните POSTGRES_PASSWORD в $env_file}" \
      -e ZAZZZOR_APP_IMAGE -e "ZAZZZOR_SCHEMA_VERSION=${schema:-}" -e "BACKUP_HOST_DIR=$ZAZZZOR_BACKUP_DIR" \
      -e TZ -e ZAZZZOR_BACKUP_KEEP_DAILY -e ZAZZZOR_BACKUP_KEEP_WEEKLY \
      -v "$volume:/data:$access" -v "$ZAZZZOR_BACKUP_DIR:/backups" -v "$script_dir:/opt/backup:ro" \
      --entrypoint /bin/bash "$pg_image" /opt/backup/backup-job.sh "$@"
  fi
}

if [[ "$action" == backup ]]; then
  run_job once
  exit 0
fi

# files reconcile безопасна при работающем API, поэтому запускается в его
# контейнере, как smoke-api.sh.
if [[ "$action" == reconcile ]]; then
  if [[ "$mode" == compose ]]; then
    exec "${compose[@]}" exec -T api /zazzzor files reconcile
  fi
  api_container="$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_api" | head -n 1)"
  [[ -n "$api_container" ]] || die "API стека $stack не работает на этом узле: bash deploy/scripts/stack.sh ps"
  exec docker exec "$api_container" /zazzzor files reconcile
fi

# Нормализует путь, не обращаясь к диску: делает его абсолютным от текущего
# каталога и убирает «.» и «..». Заглядывать в каталог копий хост не может:
# он 0700 и принадлежит UID 65532, поэтому cd и realpath там не годятся.
abs_path() {
  local path="$1" part parts=() out=()
  if [[ "$path" != /* && ! "$path" =~ ^[A-Za-z]:/ ]]; then
    path="$PWD/$path"
  fi
  IFS=/ read -ra parts <<<"$path"
  for part in "${parts[@]}"; do
    case "$part" in
      '' | .) ;;
      ..)
        if ((${#out[@]} > 0)); then
          unset 'out[-1]'
        fi
        ;;
      *) out+=("$part") ;;
    esac
  done
  (IFS=/ && printf '/%s' "${out[*]}")
}

# Принимает имя копии или путь к ней в любом виде и печатает имя. Копия
# должна лежать прямо в ZAZZZOR_BACKUP_DIR: только он смонтирован в задание.
# Путь сверяется как строка; есть ли копия и можно ли её прочесть, проверяет
# задание (verify) под UID 65532 и называет, что делать.
resolve_backup() {
  local arg="${1%/}" name parent
  name="${arg##*/}"
  if [[ "$arg" == */* ]]; then
    parent="${arg%/*}"
    parent="$(abs_path "${parent:-/}")"
    if [[ "$parent" != "$(abs_path "$ZAZZZOR_BACKUP_DIR")" ]]; then
      die "копия $arg лежит вне ZAZZZOR_BACKUP_DIR ($ZAZZZOR_BACKUP_DIR), а задание видит только его. Перенесите её: sudo cp -a '$arg' '$ZAZZZOR_BACKUP_DIR/' && sudo chown -R $backup_uid:$backup_uid '$ZAZZZOR_BACKUP_DIR/$name'"
    fi
  fi
  [[ -n "$name" ]] || die "укажите копию: имя каталога в $ZAZZZOR_BACKUP_DIR (sudo ls '$ZAZZZOR_BACKUP_DIR')"
  printf '%s' "$name"
}

name="$(resolve_backup "$3")"

# Схема, до которой доводит базу образ API: копия новее него отвергается
# ещё до остановки API. На чистой машине этот запуск заодно создаёт том
# изображений с владельцем из образа API.
if [[ "$mode" == compose ]]; then
  schema="$("${compose[@]}" run --rm --no-deps -T api schema-version)"
else
  docker volume inspect "$volume" >/dev/null || die "нет тома изображений $volume: сначала разверните стек (bash deploy/scripts/stack.sh deploy) или проверьте STACK_NAME (сейчас $stack)"
  schema="$(docker run --rm "$ZAZZZOR_APP_IMAGE" schema-version)"
fi
schema="${schema//[[:space:]]/}"
[[ "$schema" =~ ^[0-9]+$ ]] || die "образ API не назвал версию схемы (zazzzor schema-version): $schema"
run_job check "$name"

api_stopped=0
restored=0
api_env=''

# Итог при любом выходе. До восстановления базы и изображений API
# остаётся остановленным: запускать его на частично восстановленной базе
# нельзя, а повтор той же команды безопасен.
on_exit() {
  local status=$?
  trap - EXIT
  if [[ -n "$api_env" ]]; then
    rm -f -- "$api_env"
  fi
  if ((status != 0 && api_stopped == 1 && restored == 0)); then
    echo "Ошибка: восстановление из $name НЕ завершено (ошибка выше). API оставлен остановленным, чтобы никто не работал на частично восстановленной базе. Устраните причину и повторите: bash deploy/scripts/backup.sh $mode restore $name --yes" >&2
  fi
  exit "$status"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Копия $name проверена. Останавливаю API проекта $stack."
replicas=1
if [[ "$mode" == compose ]]; then
  "${compose[@]}" stop api
  api_stopped=1
  [[ -z "$("${compose[@]}" ps -q --status running api)" ]] || die "API проекта $stack всё ещё работает"
else
  replicas="$(docker service inspect --format '{{.Spec.Mode.Replicated.Replicas}}' "${stack}_api")"
  [[ "$replicas" =~ ^[0-9]+$ ]] || die "сервис ${stack}_api не replicated: восстанавливать некуда"
  # Окружение API нужно, чтобы запустить files reconcile тем же образом.
  api_env="$(mktemp)"
  chmod 600 "$api_env"
  docker service inspect --format '{{range .Spec.TaskTemplate.ContainerSpec.Env}}{{println .}}{{end}}' "${stack}_api" > "$api_env"
  docker service scale "${stack}_api=0" >/dev/null
  api_stopped=1
  for _ in $(seq 1 60); do
    [[ -n "$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_api")" ]] || break
    sleep 1
  done
  [[ -z "$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_api")" ]] || die "API стека $stack не остановился за 60 секунд"
fi

# Запускает API так, как он работал до восстановления.
start_api() {
  if [[ "$mode" == compose ]]; then
    "${compose[@]}" up -d --no-deps api
  else
    docker service scale "${stack}_api=$replicas" >/dev/null
  fi
}

# Код 3 от задания значит, что данные не изменены (база откатилась,
# блокировка занята плановой копией): API можно и нужно запустить снова.
status=0
run_job restore "$name" || status=$?
if ((status == 3)); then
  restored=1
  echo "Данные не изменены (ошибка выше). Запускаю API снова." >&2
  start_api
  die "восстановление из $name не выполнено. Устраните причину и повторите: bash deploy/scripts/backup.sh $mode restore $name --yes"
fi
((status == 0)) || exit "$status"
restored=1

echo 'Сверяю изображения с базой (files reconcile).'
reconciled=1
if [[ "$mode" == compose ]]; then
  "${compose[@]}" run --rm --no-deps -T api files reconcile || reconciled=0
else
  docker run --rm --network host --env-file "$api_env" -v "$volume:/data" "$ZAZZZOR_APP_IMAGE" files reconcile || reconciled=0
fi
start_api

if ((reconciled == 0)); then
  echo "Внимание: база и изображения восстановлены из $name, API запущен, но files reconcile сообщил об ошибках (выше): часть картинок может не открываться. Устраните причину и повторите только сверку: bash deploy/scripts/backup.sh $mode reconcile" >&2
  exit 1
fi
echo "Восстановление из $name завершено, API запущен. Проверьте вход, сообщения и картинки."
