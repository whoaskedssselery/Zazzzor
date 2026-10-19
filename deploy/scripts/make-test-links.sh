#!/usr/bin/env bash
# Выпускает персональные ссылки на страницу проверки связи (токен на 24 часа).
# Использование: bash deploy/scripts/make-test-links.sh Вася Петя ...
# Настройки берутся из deploy/.env (другой файл: ENV_FILE=/путь/к/.env).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
env_file="${ENV_FILE:-$script_dir/../.env}"

if [[ $# -eq 0 ]]; then
  echo "Использование: bash deploy/scripts/make-test-links.sh <имя> [имя...]" >&2
  exit 2
fi

if [[ ! -f "$env_file" ]]; then
  echo "Не найден файл настроек: $env_file" >&2
  exit 2
fi

# Значение переменной из .env без выполнения файла (см. check-ports.sh).
env_value() {
  local name="$1" line value
  line="$(grep -E "^[[:space:]]*${name}=" "$env_file" | tail -n 1 || true)"
  if [[ -z "$line" ]]; then
    echo "В $env_file не задана переменная $name" >&2
    exit 2
  fi
  value="${line#*=}"
  value="${value%$'\r'}"
  value="${value%\"}"; value="${value#\"}"
  value="${value%\'}"; value="${value#\'}"
  printf '%s' "$value"
}

domain="$(env_value ZAZZZOR_DOMAIN)"
https_port="$(env_value ZAZZZOR_HTTPS_PORT)"
api_key="$(env_value LIVEKIT_API_KEY)"
api_secret="$(env_value LIVEKIT_API_SECRET)"
cli_version="$(env_value LIVEKIT_CLI_VERSION)"

if [[ "$api_key" == CHANGE_ME* || "$api_secret" == CHANGE_ME* ]]; then
  echo "В $env_file остались ключи-заглушки LiveKit. Сгенерируйте свои (см. комментарии в .env.example)." >&2
  exit 2
fi

# Внешний порт, который видят друзья. Обычно совпадает с ZAZZZOR_HTTPS_PORT,
# но при пробросе «внешний 443 -> 8443» задайте ZAZZZOR_PUBLIC_PORT=443.
public_port="$https_port"
if grep -qE '^[[:space:]]*ZAZZZOR_PUBLIC_PORT=' "$env_file"; then
  public_port="$(env_value ZAZZZOR_PUBLIC_PORT)"
fi

base="https://$domain"
if [[ "$public_port" != 443 ]]; then
  base="$base:$public_port"
fi

# Ключи передаются в контейнер через окружение (это --api-key/--api-secret
# в lk), чтобы секрет не светился в списке процессов.
export LIVEKIT_API_KEY="$api_key" LIVEKIT_API_SECRET="$api_secret"

for name in "$@"; do
  token="$(docker run --rm -e LIVEKIT_API_KEY -e LIVEKIT_API_SECRET \
    "livekit/livekit-cli:$cli_version" token create --quiet \
    --join --room connectivity --identity "$name" \
    --valid-for 24h --token-only)"
  echo "$name: $base/connectivity-test/#token=$token"
done
