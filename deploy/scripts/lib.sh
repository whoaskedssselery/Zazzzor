#!/usr/bin/env bash
# Общие функции скриптов deploy/scripts. Не запускается сам: его подключают
# через source (сейчас stack.sh).
#
# Использование:
#   source "$script_dir/lib.sh"
#   die "сообщение"                  напечатать ошибку и выйти с кодом 2
#   load_env                         прочитать $env_file (переменная задаётся заранее)

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
