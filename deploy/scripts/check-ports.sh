#!/usr/bin/env bash
# Проверяет, что порты Zazzzor свободны на этой машине.
# Использование: bash deploy/scripts/check-ports.sh [путь к .env]
# По умолчанию читает deploy/.env. Запускайте ДО первого `docker compose up`
# (после запуска эти порты, разумеется, заняты самим Zazzzor).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
env_file="${1:-$script_dir/../.env}"

if [[ ! -f "$env_file" ]]; then
  echo "Не найден файл настроек: $env_file" >&2
  echo "Скопируйте deploy/.env.example в deploy/.env и заполните его." >&2
  exit 2
fi

if ! command -v ss >/dev/null 2>&1; then
  echo "Не найдена утилита ss (пакет iproute2)." >&2
  exit 2
fi

# Значение переменной из .env без выполнения файла: последняя строка NAME=...,
# без кавычек и символа \r (на случай файла из Windows).
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
  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    echo "Переменная $name должна быть номером порта, сейчас: '$value'" >&2
    exit 2
  fi
  printf '%s' "$value"
}

https_port="$(env_value ZAZZZOR_HTTPS_PORT)"
tcp_port="$(env_value LIVEKIT_TCP_PORT)"
udp_port="$(env_value LIVEKIT_UDP_PORT)"
turn_port="$(env_value LIVEKIT_TURN_UDP_PORT)"

busy=0

# check <порт> <tcp|udp>
check() {
  local port="$1" proto="$2" flag out users
  if [[ "$proto" == tcp ]]; then flag=-Hltnp; else flag=-Hlunp; fi
  # -p показывает процесс только при запуске от root; без него просто пусто.
  out="$(ss "$flag" "sport = :$port" 2>/dev/null || true)"
  if [[ -n "$out" ]]; then
    users="$(grep -o 'users:(.*)' <<<"$out" | head -n 1 || true)"
    if [[ -z "$users" ]]; then
      users="неизвестен, запустите через sudo, чтобы увидеть"
    fi
    echo "ЗАНЯТ: $port/$proto (процесс $users)"
    busy=1
  fi
}

check "$https_port" tcp
check 7880 tcp
check "$tcp_port" tcp
check "$udp_port" udp
check "$turn_port" udp

if [[ "$busy" -ne 0 ]]; then
  echo "Освободите порты или поменяйте их в $env_file (7880 менять нельзя)."
  exit 1
fi

echo "Все порты свободны"
