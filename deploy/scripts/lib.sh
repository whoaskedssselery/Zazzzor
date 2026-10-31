#!/usr/bin/env bash
# Общие функции скриптов deploy/scripts. Не запускается сам: его подключают
# через source stack.sh, backup.sh и backup-job.sh (последний — внутри образа
# Postgres на Alpine, где тоже есть bash).
#
# Использование:
#   source "$script_dir/lib.sh"
#   die "сообщение"                  напечатать ошибку и выйти с кодом 2
#   load_env                         прочитать $env_file (переменная задаётся заранее)
#   require_backup_dir <каталог>     проверить каталог копий на хосте

# Задание backup работает под тем же UID, что и API (nonroot в distroless).
backup_uid=65532

# Внутренняя сеть Postgres в docker-stack.traefik.yml (networks.db).
# Одноразовые задания backup.sh подключаются к ней как <стек>_db.
# shellcheck disable=SC2034 # Используют backup.sh и stack-test.sh.
traefik_db_network=db

die() {
  echo "Ошибка: $*" >&2
  exit 2
}

# Читает deploy/.env так же, как docker compose, но не исполняет его:
# KEY=VALUE, необязательный "export ", пробелы вокруг ключа и значения,
# кавычки, комментарий " #..." после значения без кавычек. Переменные, уже
# заданные в окружении, как и в compose, важнее файла.
load_env() {
  # shellcheck disable=SC2154 # env_file задаёт подключивший скрипт.
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

# Каталог копий на хосте должен существовать и принадлежать UID задания.
# Недостающий каталог Docker создал бы от root, и копии молча перестали бы
# сниматься, поэтому его готовит владелец сервера, а здесь проверка
# называет нужную команду.
require_backup_dir() {
  local dir="$1" owner mode
  [[ "$dir" == /* ]] || die "ZAZZZOR_BACKUP_DIR должен быть абсолютным путём, сейчас: $dir"
  if [[ ! -d "$dir" ]]; then
    die "нет каталога копий $dir. Создайте его: sudo install -d -o $backup_uid -g $backup_uid -m 0700 '$dir'"
  fi
  owner="$(stat -c %u "$dir")"
  mode="$(stat -c %a "$dir")"
  if [[ "$owner" != "$backup_uid" ]]; then
    die "каталог копий $dir принадлежит UID $owner (права $mode), а задание backup работает под UID $backup_uid. Исправьте: sudo chown $backup_uid:$backup_uid '$dir' && sudo chmod 0700 '$dir'"
  fi
}
