#!/usr/bin/env bash
# Проверяет, что порты Zazzzor свободны на этой машине.
# Использование: bash deploy/scripts/check-ports.sh [--stack] [путь к .env]
# По умолчанию читает deploy/.env. Запускайте ДО первого запуска (после него
# эти порты, разумеется, заняты самим Zazzzor). С --stack (или если Docker в
# режиме Swarm) проверяет и порты, нужные только docker stack: метрики API и
# Postgres на 127.0.0.1.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
stack_mode=0
if [[ "${1:-}" == --stack ]]; then
  stack_mode=1
  shift
elif [[ "$(docker info --format "{{.Swarm.LocalNodeState}}" 2>/dev/null || true)" == active ]]; then
  stack_mode=1
fi
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
# Необязательна (как и в compose): в .env этапа 1.1 её нет — тогда 8080.
api_port=8080
if grep -qE '^[[:space:]]*ZAZZZOR_API_PORT=' "$env_file"; then
  api_port="$(env_value ZAZZZOR_API_PORT)"
fi

# Только для docker stack (deploy/scripts/stack.sh): там API отдаёт метрики, а
# Postgres слушает на 127.0.0.1 хоста. Необязательны, по умолчанию 9464 и 55432.
metrics_port=9464
if grep -qE "^[[:space:]]*ZAZZZOR_METRICS_PORT=" "$env_file"; then
  metrics_port="$(env_value ZAZZZOR_METRICS_PORT)"
fi
postgres_port=55432
if grep -qE "^[[:space:]]*POSTGRES_PORT=" "$env_file"; then
  postgres_port="$(env_value POSTGRES_PORT)"
fi

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
# API Zazzzor (compose публикует его только на 127.0.0.1, но занятый порт на
# любом адресе тоже помешает).
check "$api_port" tcp
if [[ "$stack_mode" -eq 1 ]]; then
  check "$metrics_port" tcp
  check "$postgres_port" tcp
fi

if [[ "$busy" -ne 0 ]]; then
  echo "Освободите порты или поменяйте их в $env_file (7880 менять нельзя; для API — ZAZZZOR_API_PORT, в docker stack ещё ZAZZZOR_METRICS_PORT и POSTGRES_PORT)."
  exit 1
fi

echo "Все порты свободны"
