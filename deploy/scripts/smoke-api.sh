#!/usr/bin/env bash
# Проверяет работающий API от начала до конца: регистрирует проверочного
# пользователя smoke_<случайное>, создаёт сервер, отправляет и читает
# сообщение, открывает WebSocket (ждёт HELLO и READY) и удаляет сервер.
# Печатает OK или шаг, на котором проверка сломалась (код выхода не 0).
#
# Использование:
#   bash deploy/scripts/smoke-api.sh                    напрямую в API (127.0.0.1)
#   bash deploy/scripts/smoke-api.sh https://<домен>    через Caddy и интернет
# Проверка запускается внутри контейнера api, поэтому стек должен быть запущен.
# Если работает стек Docker Swarm (deploy/scripts/stack.sh), используется его
# контейнер api, иначе — docker compose. Настройки: deploy/.env (другой файл:
# ENV_FILE=...), имя стека: zazzzor (другое: STACK_NAME=...).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
env_file="${ENV_FILE:-$script_dir/../.env}"

# Чтобы Git Bash на Windows не превращал /zazzzor в путь Windows (на Linux
# ничего не меняет).
export MSYS2_ARG_CONV_EXCL=/zazzzor

swarm_api="$(docker ps -q --filter "label=com.docker.swarm.service.name=${STACK_NAME:-zazzzor}_api" | head -n 1)"
if [[ -n "$swarm_api" ]]; then
  # В стеке API в сети хоста и слушает 127.0.0.1:ZAZZZOR_API_PORT.
  api_port="$(grep -E "^[[:space:]]*ZAZZZOR_API_PORT=" "$env_file" 2>/dev/null | tail -n 1 | cut -d= -f2 | tr -d "\r\"'" || true)"
  base_url="${1:-http://127.0.0.1:${api_port:-8080}}"
  exec docker exec "$swarm_api" /zazzzor smoke "$base_url"
fi

compose=(docker compose -f "$script_dir/../docker-compose.yml")
if [[ -n "${ENV_FILE:-}" ]]; then
  compose+=(--env-file "$ENV_FILE")
fi

# В compose API внутри контейнера всегда слушает 8080, независимо от
# ZAZZZOR_API_PORT.
base_url="${1:-http://127.0.0.1:8080}"

exec "${compose[@]}" exec -T api /zazzzor smoke "$base_url"
