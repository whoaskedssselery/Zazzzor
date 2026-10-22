#!/usr/bin/env bash
# Проверяет работающий API от начала до конца: регистрирует проверочного
# пользователя smoke_<случайное>, создаёт сервер, отправляет и читает
# сообщение, открывает WebSocket (ждёт HELLO и READY) и удаляет сервер.
# Печатает OK или шаг, на котором проверка сломалась (код выхода не 0).
#
# Использование:
#   bash deploy/scripts/smoke-api.sh                    напрямую в API (127.0.0.1)
#   bash deploy/scripts/smoke-api.sh https://<домен>    через Caddy и интернет
# Проверка запускается внутри контейнера api (docker compose exec), поэтому
# стек должен быть запущен. Настройки: deploy/.env (другой файл: ENV_FILE=...).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
compose=(docker compose -f "$script_dir/../docker-compose.yml")
if [[ -n "${ENV_FILE:-}" ]]; then
  compose+=(--env-file "$ENV_FILE")
fi

# Чтобы Git Bash на Windows не превращал /zazzzor в путь Windows (на Linux
# ничего не меняет).
export MSYS2_ARG_CONV_EXCL=/zazzzor

# Внутри контейнера API всегда слушает 8080, независимо от ZAZZZOR_API_PORT.
base_url="${1:-http://127.0.0.1:8080}"

exec "${compose[@]}" exec -T api /zazzzor smoke "$base_url"
