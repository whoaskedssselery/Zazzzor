#!/usr/bin/env bash
# Проверяет HTTP-конфигурацию веб-клиента (Caddyfile.web) в собранном образе
# Caddy: SPA, CSP, кэш, 404 для отсутствующих assets и страницу проверки связи.
#
# Использование (из корня репозитория):
#   bash deploy/scripts/web-test.sh <образ>
#
# Создаёт только свой контейнер и удаляет его при любом выходе.
set -euo pipefail

image="${1:?укажите собранный образ Caddy}"
name="zazzzor-web-test-$$"
tmp="$(mktemp -d)"
export MSYS2_ARG_CONV_EXCL='/etc/caddy;/config;/data'

# Удаляет только контейнер этой проверки и её временные ответы.
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf -- "$tmp"
}
trap cleanup EXIT

# Печатает логи контейнера, чтобы в CI была видна причина, и завершает тест.
fail() {
  echo "FAIL: $*" >&2
  docker logs "$name" >&2 || true
  exit 1
}

# expect_status <путь> <код> [метод]: ответ и заголовки сохраняются в $tmp.
expect_status() {
  local status
  status="$(curl -sS --max-time 5 -X "${3:-GET}" -o "$tmp/body" -D "$tmp/headers" -w '%{http_code}' "$url$1")" ||
    fail "$1: нет ответа от $url"
  [[ "$status" == "$2" ]] || fail "$1: статус $status, ожидался $2"
}

# expect_header <путь> <регулярное выражение строки заголовка>
expect_header() {
  grep -qi -- "$2" "$tmp/headers" || fail "$1: нет заголовка $2"
}

docker run -d --name "$name" --read-only --cap-drop ALL --cap-add NET_BIND_SERVICE \
  --tmpfs /config --tmpfs /data -p 127.0.0.1::8080 \
  "$image" caddy run --config /etc/caddy/Caddyfile.web >/dev/null
port="$(docker port "$name" 8080/tcp | head -1)" || fail "у контейнера $name нет порта 8080"
url="http://${port%$'\r'}"

# Ждёт первого ответа; если Caddy упал при старте, сразу показывает почему.
ready=0
for _ in $(seq 1 30); do
  [[ "$(docker inspect --format '{{.State.Running}}' "$name")" == true ]] || fail 'Caddy остановился при старте'
  if curl -fs --max-time 3 -o "$tmp/index" "$url/"; then
    ready=1
    break
  fi
  sleep 1
done
((ready)) || fail "Caddy не ответил на $url/ за 30 секунд"

for path in / /channels/@me; do
  expect_status "$path" 200
  cmp -s "$tmp/index" "$tmp/body" || fail "$path: ответ не index.html"
  expect_header "$path" '^Content-Security-Policy:.*frame-ancestors'
  # The noise suppression models are WebAssembly; nothing more than compiling them is allowed.
  expect_header "$path" "^Content-Security-Policy:.*script-src 'self' 'wasm-unsafe-eval';"
  expect_header "$path" '^Cache-Control: no-cache'
  # A year, without preload or includeSubDomains: DuckDNS names are shared.
  expect_header "$path" '^Strict-Transport-Security: max-age=31536000[[:space:]]*$'
done

asset="$(sed -n 's/.*src="\(\/assets\/[^\"]*\.js\)".*/\1/p' "$tmp/index" | head -1)"
[[ -n "$asset" ]] || fail 'в index.html нет скрипта /assets/*.js'
expect_status "$asset" 200
expect_header "$asset" '^Cache-Control: public, max-age=31536000, immutable'

expect_status /assets/missing-web-test.js 404
if grep -qi 'immutable' "$tmp/headers"; then
  fail '/assets/missing-web-test.js: отсутствующий asset закэширован'
fi

for path in /api/v1/livekit/webhook '/api/v1/livekit/webhook?test=1' /api/v1/livekit/%77ebhook; do
  expect_status "$path" 404
  expect_status "$path" 404 POST
done

expect_status /connectivity-test/ 200
expect_header /connectivity-test/ '^Content-Security-Policy:.*wss:'
if cmp -s "$tmp/index" "$tmp/body"; then
  fail '/connectivity-test/ вернул SPA вместо страницы проверки связи'
fi

# The TLS Caddyfile cannot be served here (it needs DuckDNS), so its adapted
# JSON is checked instead: HSTS on both the web client and /api/*.
adapted="$(docker run --rm --env-file deploy/.env.ci "$image" caddy adapt --config /etc/caddy/Caddyfile 2>/dev/null)" ||
  fail 'caddy adapt не разобрал Caddyfile'
hsts="$(grep -o '"Strict-Transport-Security":\["max-age=31536000"\]' <<<"$adapted" | wc -l)"
((hsts >= 2)) || fail "в Caddyfile HSTS в $hsts блоках, ожидались веб-клиент и /api/*"

echo 'web test: OK (SPA, CSP, HSTS, cache, asset 404, webhook 404, connectivity test)'
