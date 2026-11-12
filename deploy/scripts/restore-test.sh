#!/usr/bin/env bash
# Проверка резервной копии от начала до конца в отдельном временном проекте
# docker compose (рабочий стек и его тома не затрагиваются):
#   1. собирает образ API и запускает postgres и api проекта;
#   2. через API создаёт пользователя с аватаром, сервер, иконку которого
#      заменяет (старая уходит в корзину), и сообщение
#      (zazzzor restore-check seed в контейнере api);
#   3. снимает копию (backup.sh compose backup);
#   4. добавляет в базу таблицу «более новой схемы» и восстанавливает копию
#      поверх: таблица исчезает, API запускается, данные на месте
#      (zazzzor restore-check verify);
#   5. удаляет тома, восстанавливает копию на новые и проверяет снова.
# В конце удаляет свои контейнеры, тома, образ и временный каталог.
#
# Использование (из корня репозитория; нужны Docker и bash, Go не нужен):
#   bash deploy/scripts/restore-test.sh
# Печатает шаги и в конце «restore test: OK». Тот же сценарий идёт в CI.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd "$script_dir/../.." && pwd)"
env_file="$repo_dir/deploy/.env.ci"
# shellcheck source=deploy/scripts/lib.sh
source "$script_dir/lib.sh"

test_root="$(mktemp -d)"
# Уникальное имя проекта: down --volumes ниже удаляет только его тома.
STACK_NAME="zazzzor-restore-test-$(date +%s)-$$"
export STACK_NAME
export ENV_FILE="$env_file"
export ZAZZZOR_API_IMAGE="zazzzor-api:$STACK_NAME"
export ZAZZZOR_COMPOSE_OVERRIDE="$test_root/override.yml"
export ZAZZZOR_BACKUP_DIR="$test_root/backups"
mkdir -p "$ZAZZZOR_BACKUP_DIR"
# Compose читает пути сам, и в Git Bash на Windows ему нужен путь с диском.
if command -v cygpath >/dev/null; then
  ZAZZZOR_BACKUP_DIR="$(cygpath -m "$ZAZZZOR_BACKUP_DIR")"
fi
# Чтобы Git Bash на Windows не превращал пути внутри контейнеров в пути
# Windows (на Linux ничего не меняет).
export MSYS2_ARG_CONV_EXCL='/zazzzor;/bin/bash;/bin/sh;/backups'
load_env
pg_image="postgres:$POSTGRES_VERSION"
base_url=http://127.0.0.1:8080

# API публикует порт на 127.0.0.1; здесь — случайный, чтобы не столкнуться
# с тем, что уже работает на машине.
cat > "$ZAZZZOR_COMPOSE_OVERRIDE" <<'EOF'
services:
  api:
    ports: !override ["127.0.0.1::8080"]
EOF
compose=(docker compose --env-file "$env_file" -p "$STACK_NAME" -f "$repo_dir/deploy/docker-compose.yml" -f "$ZAZZZOR_COMPOSE_OVERRIDE")

# Убирает всё, что создала проверка; при ошибке сначала печатает логи.
cleanup() {
  local status=$? tidy='find /backups -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +'
  trap - EXIT
  if ((status != 0)); then
    "${compose[@]}" logs --no-color --tail 60 api postgres >&2 || true
  fi
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || echo "не удалось убрать проект $STACK_NAME: docker compose -p $STACK_NAME down --volumes" >&2
  docker image rm "$ZAZZZOR_API_IMAGE" >/dev/null 2>&1 || true
  # Каталог копий (0700) и файлы в нём принадлежат UID задания: на Linux
  # только он может их удалить и открыть каталог, чтобы хост убрал его.
  if [[ "$(uname -s)" == Linux ]]; then
    tidy="$tidy && chmod 0777 /backups"
  fi
  docker run --rm --user "$backup_uid:$backup_uid" -v "$ZAZZZOR_BACKUP_DIR:/backups" --entrypoint /bin/sh "$pg_image" \
    -c "$tidy" || true
  rm -rf -- "$test_root"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# api_check <seed|verify> запускает restore-check в контейнере api.
api_check() {
  "${compose[@]}" exec -T api /zazzzor restore-check "$1" "$base_url"
}

echo '1. Образ API и запуск проекта'
# На Linux каталог копий должен принадлежать UID задания, как на сервере.
if [[ "$(uname -s)" == Linux ]]; then
  docker run --rm -v "$ZAZZZOR_BACKUP_DIR:/backups" --entrypoint /bin/sh "$pg_image" -c "chown $backup_uid:$backup_uid /backups && chmod 0700 /backups"
fi
docker build -f "$repo_dir/server/Dockerfile" --build-arg VERSION=restore-test -t "$ZAZZZOR_API_IMAGE" "$repo_dir/server"
"${compose[@]}" up -d --wait postgres api
# Регистрация нужна только для заполнения тестовой базы.
"${compose[@]}" exec -T postgres psql -U zazzzor -d zazzzor -v ON_ERROR_STOP=1 -q -c 'UPDATE instance SET registration_open = true'

echo '2. Данные через API'
api_check seed > "$test_root/state.json"

echo '3. Копия'
bash "$script_dir/backup.sh" compose backup
# Каталог копий принадлежит UID 65532 (0700), поэтому читается через задание.
folder="$("${compose[@]}" run --rm --no-deps -T --entrypoint /bin/bash backup -c "ls -1 /backups")"
[[ "$(wc -l <<<"$folder")" -eq 1 && -n "$folder" ]] || die "ожидалась одна копия, есть: $folder"
"${compose[@]}" run --rm --no-deps -T --entrypoint /bin/bash backup -c "tar -tzf /backups/$folder/apidata.tar.gz" > "$test_root/archive.txt"
grep -qE '^trash/icons/[0-9]+/[0-9a-f]{16}/[0-9]+\.png$' "$test_root/archive.txt" || die 'в архиве нет заменённой иконки из корзины'
if bash "$script_dir/backup.sh" compose restore "$folder" >/dev/null 2>&1; then
  die 'восстановление без --yes'
fi

echo '4. Восстановление поверх базы с более новой схемой'
"${compose[@]}" exec -T postgres psql -U zazzzor -d zazzzor -v ON_ERROR_STOP=1 -q -c 'CREATE TABLE restore_test_newer (id int)'
bash "$script_dir/backup.sh" compose restore "$ZAZZZOR_BACKUP_DIR/$folder/" --yes
newer="$("${compose[@]}" exec -T postgres psql -U zazzzor -d zazzzor -Atq -c "SELECT to_regclass('public.restore_test_newer')")"
[[ -z "$newer" ]] || die 'таблица более новой схемы пережила восстановление'
"${compose[@]}" up -d --wait --no-deps api
api_check verify < "$test_root/state.json"

echo '5. Восстановление на чистые тома'
"${compose[@]}" down --volumes
"${compose[@]}" up -d --wait postgres
bash "$script_dir/backup.sh" compose restore "$folder" --yes
"${compose[@]}" up -d --wait --no-deps api
api_check verify < "$test_root/state.json"

echo 'restore test: OK (новая схема и чистые тома: вход, сообщение, байты иконки и аватара)'
