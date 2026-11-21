#!/usr/bin/env bash
# Writes deploy/.env for a first start of the Compose stack: asks for the
# few settings only the owner knows (the DuckDNS domain and token, an email
# for Let's Encrypt, the administrator's username), generates every secret,
# and fills the rest from deploy/.env.example, so pinned versions and
# defaults stay as the repository ships them.
#
# Usage, from the repository root:
#   bash deploy/scripts/init-env.sh                 asks for each setting
#   bash deploy/scripts/init-env.sh --yes --domain zazzzor.duckdns.org \
#     --email ivan@example.org --admin ivan         no questions (token from
#                                                   DUCKDNS_TOKEN or a flag)
# Options:
#   --domain NAME          full hostname, <subdomain>.duckdns.org
#   --email ADDRESS        email for Let's Encrypt notices
#   --duckdns-token TOKEN  DuckDNS token; prefer the DUCKDNS_TOKEN variable,
#                          since flags are visible to other users in ps
#   --admin USERNAME       the first administrator's username
#   --lang en|ru           language of the prompts (default: from LANG)
#   --output FILE          where to write (default: deploy/.env)
#   --force                replace an existing file, keeping a copy
#   -y, --yes              ask nothing; fail if a required setting is missing
#
# The file is written with mode 600. Secret values are never printed, only
# their names. Needs bash, coreutils, awk and openssl or /dev/urandom.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
template="$script_dir/../.env.example"

domain=""
email=""
token="${DUCKDNS_TOKEN:-}"
admin=""
lang=""
output="$script_dir/../.env"
force=0
yes=0

case "${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}" in
  ru*) lang=ru ;;
  *) lang=en ;;
esac

# msg KEY prints one user-facing string in the chosen language.
msg() {
  if [[ "$lang" == ru ]]; then
    case "$1" in
      error) printf 'Ошибка' ;;
      usage) printf 'Справка: bash deploy/scripts/init-env.sh --help' ;;
      unknown) printf 'неизвестный параметр' ;;
      needs_value) printf 'нужно значение после' ;;
      bad_lang) printf -- '--lang принимает en или ru' ;;
      no_template) printf 'не найден шаблон' ;;
      exists) printf 'файл уже есть; чтобы заменить его, добавьте --force' ;;
      force_warn) printf 'Внимание: база создаётся с паролем из первого .env. Если стек уже запускался, новый POSTGRES_PASSWORD не подойдёт к базе (см. раздел 13 deploy/README.ru.md).' ;;
      kept_copy) printf 'Прежний файл сохранён как' ;;
      missing) printf 'не задано (нужно с --yes):' ;;
      ask_domain) printf 'Домен DuckDNS (например, zazzzor.duckdns.org): ' ;;
      ask_token) printf 'Токен DuckDNS (со страницы duckdns.org, ввод скрыт): ' ;;
      ask_email) printf 'Почта для Let'\''s Encrypt: ' ;;
      ask_admin) printf 'Логин администратора (латиница, цифры, _ и ., 2–32 символа): ' ;;
      bad_domain) printf 'домен должен иметь вид <поддомен>.duckdns.org' ;;
      bad_token) printf 'токен DuckDNS имеет вид xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx' ;;
      bad_email) printf 'это не похоже на адрес почты' ;;
      bad_admin) printf 'логин: 2–32 символа, латиница, цифры, _ и .' ;;
      no_input) printf 'ввод закончился; для запуска без вопросов используйте --yes и параметры' ;;
      summary) printf 'Будет записан файл' ;;
      generated) printf 'Сгенерированы' ;;
      confirm) printf 'Записать? [Y/n] ' ;;
      aborted) printf 'Отменено, ничего не записано.' ;;
      no_random) printf 'нет ни openssl, ни /dev/urandom для генерации секретов' ;;
      written) printf 'Готово' ;;
      next) printf 'Дальше, из корня репозитория:' ;;
      step_backup) printf 'Каталог резервных копий:' ;;
      step_ports) printf 'Проверить, что порты свободны, и пробросить на роутере 443/tcp, 7881/tcp, 7882/udp:' ;;
      step_up) printf 'Запустить стек (первая сборка идёт несколько минут):' ;;
      step_admin) printf 'Создать администратора (пароль вводится скрытно):' ;;
      step_smoke) printf 'Войти в браузере, открыть регистрацию («Настройки пользователя» → «Администрирование» → «Сервер Zazzzor») и проверить сервер:' ;;
      details) printf 'Подробности: deploy/README.ru.md' ;;
    esac
  else
    case "$1" in
      error) printf 'Error' ;;
      usage) printf 'Help: bash deploy/scripts/init-env.sh --help' ;;
      unknown) printf 'unknown option' ;;
      needs_value) printf 'a value is required after' ;;
      bad_lang) printf -- '--lang takes en or ru' ;;
      no_template) printf 'template not found' ;;
      exists) printf 'the file already exists; add --force to replace it' ;;
      force_warn) printf 'Warning: Postgres keeps the password from the first .env. If the stack has already run, a new POSTGRES_PASSWORD will not match the database (see section 13 of deploy/README.md).' ;;
      kept_copy) printf 'The previous file was kept as' ;;
      missing) printf 'missing (required with --yes):' ;;
      ask_domain) printf 'DuckDNS domain (for example zazzzor.duckdns.org): ' ;;
      ask_token) printf 'DuckDNS token (from duckdns.org, input hidden): ' ;;
      ask_email) printf 'Email for Let'\''s Encrypt: ' ;;
      ask_admin) printf 'Administrator username (letters, digits, _ and ., 2-32 characters): ' ;;
      bad_domain) printf 'the domain must look like <subdomain>.duckdns.org' ;;
      bad_token) printf 'a DuckDNS token looks like xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx' ;;
      bad_email) printf 'that does not look like an email address' ;;
      bad_admin) printf 'username: 2-32 characters, Latin letters, digits, _ and .' ;;
      no_input) printf 'input ended; to run without questions use --yes and the options' ;;
      summary) printf 'About to write' ;;
      generated) printf 'Generated' ;;
      confirm) printf 'Write it? [Y/n] ' ;;
      aborted) printf 'Cancelled, nothing written.' ;;
      no_random) printf 'neither openssl nor /dev/urandom is available to generate secrets' ;;
      written) printf 'Done' ;;
      next) printf 'Next, from the repository root:' ;;
      step_backup) printf 'Create the backup directory:' ;;
      step_ports) printf 'Check that the ports are free, and forward 443/tcp, 7881/tcp, 7882/udp on the router:' ;;
      step_up) printf 'Start the stack (the first build takes a few minutes):' ;;
      step_admin) printf 'Create the administrator (the password prompt is hidden):' ;;
      step_smoke) printf 'Sign in with a browser, open registration (User settings → Administration → Zazzzor server) and test the server:' ;;
      details) printf 'Details: deploy/README.md' ;;
    esac
  fi
}

die() {
  printf '%s: %s\n' "$(msg error)" "$*" >&2
  exit 2
}

usage() {
  sed -n '2,/^set -euo/{/^set -euo/d;s/^# \{0,1\}//;p}' "${BASH_SOURCE[0]}"
}

# The language flag goes first, so errors about the other flags use it.
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[$i]}" in
    --lang) lang="${args[$((i + 1))]:-}" ;;
    --lang=*) lang="${args[$i]#--lang=}" ;;
  esac
done
[[ "$lang" == en || "$lang" == ru ]] || { lang=en; die "$(msg bad_lang)"; }

while (($#)); do
  case "$1" in
    --domain | --email | --duckdns-token | --admin | --lang | --output)
      (($# >= 2)) || die "$(msg needs_value) $1"
      case "$1" in
        --domain) domain="$2" ;;
        --email) email="$2" ;;
        --duckdns-token) token="$2" ;;
        --admin) admin="$2" ;;
        --output) output="$2" ;;
      esac
      shift 2
      ;;
    --domain=*) domain="${1#*=}"; shift ;;
    --email=*) email="${1#*=}"; shift ;;
    --duckdns-token=*) token="${1#*=}"; shift ;;
    --admin=*) admin="${1#*=}"; shift ;;
    --lang=*) shift ;;
    --output=*) output="${1#*=}"; shift ;;
    --force) force=1; shift ;;
    -y | --yes) yes=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die "$(msg unknown) $1. $(msg usage)" ;;
  esac
done

[[ -f "$template" ]] || die "$(msg no_template) $template"
if [[ -e "$output" && "$force" -ne 1 ]]; then
  die "$output: $(msg exists)"
fi

# normalize_domain accepts a pasted URL and returns the bare lower-case host.
normalize_domain() {
  local d="$1"
  d="${d#http://}"
  d="${d#https://}"
  d="${d%%/*}"
  printf '%s' "$d" | tr '[:upper:]' '[:lower:]'
}

valid_domain() { [[ "$1" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?\.duckdns\.org$ ]]; }
valid_token() { [[ "$1" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; }
valid_email() { [[ "$1" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]]; }
# The same rule as usernameRE in server/internal/auth/service.go.
valid_admin() { [[ "$1" =~ ^[A-Za-z0-9_.]{2,32}$ ]]; }

# ask VAR PROMPT_KEY CHECK ERROR_KEY [secret] reads a value until CHECK
# accepts it. A secret is read without echo.
ask() {
  local var="$1" prompt="$2" check="$3" error="$4" secret="${5:-}" value
  while :; do
    # Printed by hand: read -p stays silent when stdin is not a terminal.
    msg "$prompt" >&2
    if [[ -n "$secret" ]]; then
      IFS= read -rs value || die "$(msg no_input)"
      echo >&2
    else
      IFS= read -r value || die "$(msg no_input)"
    fi
    [[ "$var" == domain ]] && value="$(normalize_domain "$value")"
    [[ "$var" == token ]] && value="$(printf '%s' "$value" | tr -d '[:space:]')"
    if "$check" "$value"; then
      printf -v "$var" '%s' "$value"
      return
    fi
    printf '%s\n' "$(msg "$error")" >&2
  done
}

domain="$(normalize_domain "$domain")"
token="$(printf '%s' "$token" | tr -d '[:space:]')"

if ((yes)); then
  missing=()
  [[ -n "$domain" ]] || missing+=(--domain)
  [[ -n "$token" ]] || missing+=("DUCKDNS_TOKEN/--duckdns-token")
  [[ -n "$email" ]] || missing+=(--email)
  [[ -n "$admin" ]] || missing+=(--admin)
  ((${#missing[@]} == 0)) || die "$(msg missing) ${missing[*]}"
fi

# A value given on the command line must be valid as it is; one typed in
# answer to a prompt is asked again.
check_given() {
  local value="$1" check="$2" error="$3"
  [[ -z "$value" ]] || "$check" "$value" || die "$(msg "$error")"
}
check_given "$domain" valid_domain bad_domain
check_given "$token" valid_token bad_token
check_given "$email" valid_email bad_email
check_given "$admin" valid_admin bad_admin

[[ -n "$domain" ]] || ask domain ask_domain valid_domain bad_domain
[[ -n "$token" ]] || ask token ask_token valid_token bad_token secret
[[ -n "$email" ]] || ask email ask_email valid_email bad_email
[[ -n "$admin" ]] || ask admin ask_admin valid_admin bad_admin

# random_hex BYTES prints 2*BYTES hex characters from a secure source.
random_hex() {
  local out
  if command -v openssl >/dev/null 2>&1; then
    out="$(openssl rand -hex "$1")"
  elif [[ -r /dev/urandom ]]; then
    out="$(od -An -tx1 -N "$1" /dev/urandom | tr -d ' \n')"
  else
    die "$(msg no_random)"
  fi
  [[ "${#out}" -eq $(($1 * 2)) ]] || die "$(msg no_random)"
  printf '%s' "$out"
}

secret_names="LIVEKIT_API_KEY, LIVEKIT_API_SECRET, POSTGRES_PASSWORD"
printf '%s %s:\n' "$(msg summary)" "$output" >&2
printf '  ZAZZZOR_DOMAIN=%s\n  DUCKDNS_SUBDOMAIN=%s\n  DUCKDNS_TOKEN=(hidden)\n  ACME_EMAIL=%s\n  ZAZZZOR_ADMIN_USERNAMES=%s\n' \
  "$domain" "${domain%.duckdns.org}" "$email" "$admin" >&2
printf '  %s: %s\n' "$(msg generated)" "$secret_names" >&2
if ((!yes)); then
  msg confirm >&2
  IFS= read -r answer || die "$(msg no_input)"
  case "$answer" in
    "" | [Yy]* | [Дд]*) ;;
    *) printf '%s\n' "$(msg aborted)" >&2; exit 1 ;;
  esac
fi

# Values reach awk through the environment, never its arguments, so no
# secret shows up in the process list.
INIT_ZAZZZOR_DOMAIN="$domain"
INIT_DUCKDNS_SUBDOMAIN="${domain%.duckdns.org}"
INIT_DUCKDNS_TOKEN="$token"
INIT_ACME_EMAIL="$email"
INIT_ZAZZZOR_ADMIN_USERNAMES="$admin"
# LiveKit wants a secret of at least 32 characters; these give 32 and 64.
INIT_LIVEKIT_API_KEY="$(random_hex 16)"
INIT_LIVEKIT_API_SECRET="$(random_hex 32)"
# Hex only: the password goes into the database URL without escaping.
INIT_POSTGRES_PASSWORD="$(random_hex 24)"
export INIT_ZAZZZOR_DOMAIN INIT_DUCKDNS_SUBDOMAIN INIT_DUCKDNS_TOKEN INIT_ACME_EMAIL \
  INIT_ZAZZZOR_ADMIN_USERNAMES INIT_LIVEKIT_API_KEY INIT_LIVEKIT_API_SECRET INIT_POSTGRES_PASSWORD

out_dir="$(dirname "$output")"
umask 077
tmp="$(mktemp "$out_dir/.env.init.XXXXXX")"
trap 'rm -f -- "$tmp"' EXIT

# Fills each "KEY=" line of the template that has a value above; fails if a
# key is missing from the template or appears twice.
awk -v stamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
  BEGIN {
    n = split("ZAZZZOR_DOMAIN DUCKDNS_SUBDOMAIN DUCKDNS_TOKEN ACME_EMAIL ZAZZZOR_ADMIN_USERNAMES LIVEKIT_API_KEY LIVEKIT_API_SECRET POSTGRES_PASSWORD", keys, " ")
    for (i = 1; i <= n; i++) want[keys[i]] = ENVIRON["INIT_" keys[i]]
    print "# Written by deploy/scripts/init-env.sh at " stamp "."
  }
  {
    sub(/\r$/, "")
    if (match($0, /^[A-Z_][A-Z0-9_]*=$/)) {
      key = substr($0, 1, RLENGTH - 1)
      if (key in want) {
        if (seen[key]++) { print "duplicate key " key > "/dev/stderr"; exit 3 }
        print key "=" want[key]
        next
      }
    }
    print
  }
  END {
    for (k in want) if (!seen[k]) { print "template has no line " k "=" > "/dev/stderr"; exit 3 }
  }
' "$template" >"$tmp" || die "$(msg no_template) $template"
chmod 600 "$tmp"

if [[ -e "$output" ]]; then
  backup="$output.bak.$(date -u +%Y%m%d%H%M%S)"
  cp -p -- "$output" "$backup"
  chmod 600 "$backup"
  printf '%s\n%s %s\n' "$(msg force_warn)" "$(msg kept_copy)" "$backup" >&2
fi
mv -f -- "$tmp" "$output"
trap - EXIT

backup_dir="$(grep -E '^ZAZZZOR_BACKUP_DIR=' "$output" | tail -n 1 | cut -d= -f2)"
cat <<EOF

$(msg written): $output (600)

$(msg next)

  # $(msg step_backup)
  sudo install -d -o 65532 -g 65532 -m 0700 ${backup_dir:-/srv/zazzzor-backups}

  # $(msg step_ports)
  sudo bash deploy/scripts/check-ports.sh

  # $(msg step_up)
  docker compose -f deploy/docker-compose.yml up -d --build

  # $(msg step_admin)
  read -rsp 'Password: ' PASS; echo
  printf '%s\n' "\$PASS" | docker compose -f deploy/docker-compose.yml exec -T api /zazzzor create-admin $admin
  unset PASS

  # $(msg step_smoke)
  bash deploy/scripts/smoke-api.sh https://$domain

$(msg details)
EOF
