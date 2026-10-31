#!/usr/bin/env bash
# Проверяет оба Swarm-варианта с фальшивыми значениями, без развёртывания.
#
# Использование (из корня репозитория):
#   bash deploy/scripts/stack-test.sh
#
# Нужен Docker CLI. Сервисы, сети и тома не создаются: deploy проверяется
# с подменным docker, который падает на любом изменяющем вызове.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy_dir="$(cd "$script_dir/.." && pwd)"
ci_env="$deploy_dir/.env.ci"
export ZAZZZOR_API_TAG=ci ZAZZZOR_CADDY_TAG=ci
# Адрес только для Traefik-рендеров: в .env.ci его нет, чтобы edge-рендер
# проверял LiveKit с STUN (LIVEKIT_USE_EXTERNAL_IP=true).
node_ip=192.0.2.10
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT

# shellcheck source=deploy/scripts/lib.sh
source "$script_dir/lib.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# expect <файл> <шаблон grep> <что не так, если строки нет>
expect() {
  grep -q -- "$2" "$1" || fail "$3"
}

# reject <файл> <шаблон grep> <что не так, если строка есть>
reject() {
  if grep -n -- "$2" "$1" >&2; then
    fail "$3"
  fi
}

# render <файл> [ИМЯ=значение...]: stack.sh config с .env.ci и данным
# окружением; в результате не должно остаться ни одной ${...}.
render() {
  local out="$1"
  shift
  env ENV_FILE="$ci_env" "$@" bash "$script_dir/stack.sh" config >"$out" ||
    fail "stack.sh config не отрисовал стек ($*)"
  reject "$out" '\$''{' "остались переменные в стеке ($*)"
}

# service <файл> <сервис>: блок одного сервиса из отрисованного стека.
service() {
  awk -v name="  $2:" '
    /^[^ ]/ { in_services = ($0 == "services:"); inside = 0 }
    in_services && /^  [^ ]/ { inside = ($0 == name) }
    inside' "$1"
}

# networks <файл>: блок networks верхнего уровня.
networks() {
  awk '/^[^ ]/ { inside = ($0 == "networks:") } inside' "$1"
}

echo '- .env.example: настройки варианта закомментированы'
# Активная строка варианта в примере столкнулась бы с той, которую владелец
# впишет ниже (STYLE §16): раньше она молча побеждала, теперь это повтор.
reject "$deploy_dir/.env.example" '^\(ZAZZZOR_EDGE\|TRAEFIK_[A-Z_]*\|LIVEKIT_TURN_[A-Z_]*\|ZAZZZOR_\(API\|CADDY\)_REPOSITORY\)=' \
  'в .env.example активна строка варианта; закомментируйте её со значением по умолчанию'

echo '- .env: повтор ключа — ошибка, а не тихий выбор одного из значений'
cp "$ci_env" "$tmp/duplicate.env"
echo 'ZAZZZOR_DOMAIN=second.example.invalid' >>"$tmp/duplicate.env"
if ENV_FILE="$tmp/duplicate.env" bash "$script_dir/stack.sh" config >"$tmp/duplicate" 2>&1; then
  fail 'stack.sh принял .env с повторённым ZAZZZOR_DOMAIN'
fi
expect "$tmp/duplicate" 'ZAZZZOR_DOMAIN' 'ошибка о повторе не называет ключ'

echo '- Compose: репозитории Swarm не меняют полный образ Compose'
cp "$ci_env" "$tmp/compose.env"
printf '%s\n' ZAZZZOR_API_REPOSITORY=ghcr.io/example/api ZAZZZOR_CADDY_REPOSITORY=ghcr.io/example/web >>"$tmp/compose.env"
docker compose --env-file "$tmp/compose.env" -f "$deploy_dir/docker-compose.yml" config --images >"$tmp/compose-images" ||
  fail 'docker compose config не прошёл с .env.ci'
grep -Fxq 'zazzzor-api:local' "$tmp/compose-images" || fail 'настройки Swarm изменили полный образ Compose'

echo '- edge Caddy'
render "$tmp/caddy" ZAZZZOR_EDGE=caddy
expect "$tmp/caddy" 'duckdns:' 'edge-вариант без DuckDNS'
expect "$tmp/caddy" 'use_external_ip: true' 'edge без LIVEKIT_NODE_IP не включил STUN'

echo '- Traefik, TURN выключен'
render "$tmp/traefik" ZAZZZOR_EDGE=traefik LIVEKIT_NODE_IP="$node_ip"
expect "$tmp/traefik" 'Caddyfile.web' 'web не запускает Caddyfile.web'
reject "$tmp/traefik" 'duckdns:' 'Traefik-вариант запускает DuckDNS'
expect "$tmp/traefik" 'traefik.http.routers.zazzzor-api.priority: "100"' 'у router API не приоритет 100'
expect "$tmp/traefik" 'traefik.http.routers.zazzzor-web.priority: "1"' 'у router web не приоритет 1'
expect "$tmp/traefik" '172.16.0.0/12' 'LiveKit не ограничен docker_gwbridge'
expect "$tmp/traefik" 'mode: host' 'медиа-порты не в режиме host'
expect "$tmp/traefik" 'PGHOST: postgres' 'backup ищет Postgres не по имени сервиса'
expect "$tmp/traefik" 'enabled: false' 'TURN не выключен по умолчанию'
reject "$tmp/traefik" 'traefik.tcp.routers.zazzzor-turn' 'TCP-router TURN есть при выключенном TURN'
# backup.sh подключает одноразовые задания к этой же сети (lib.sh).
networks "$tmp/traefik" | grep -A4 "^  $traefik_db_network:$" | grep -q 'attachable: true' ||
  fail "сеть $traefik_db_network не attachable: задания backup.sh к ней не подключатся"
for name in api postgres backup; do
  service "$tmp/traefik" "$name" | grep -q "^      $traefik_db_network: null$" ||
    fail "сервис $name не в сети $traefik_db_network"
done

echo '- Traefik, TURN включён, образы GHCR'
render "$tmp/turn" ZAZZZOR_EDGE=traefik LIVEKIT_NODE_IP="$node_ip" LIVEKIT_TURN_ENABLED=true \
  ZAZZZOR_API_REPOSITORY=ghcr.io/example/api ZAZZZOR_CADDY_REPOSITORY=ghcr.io/example/web
expect "$tmp/turn" 'enabled: true' 'TURN не включился'
expect "$tmp/turn" 'external_tls: true' 'TURN без external_tls'
expect "$tmp/turn" 'traefik.tcp.routers.zazzzor-turn.rule: HostSNI' 'нет TCP-router TURN'
expect "$tmp/turn" 'image: ghcr.io/example/api:ci' 'API не из заданного репозитория'
expect "$tmp/turn" 'image: ghcr.io/example/web:ci' 'web не из заданного репозитория'

echo '- неверные настройки отвергаются'
if ENV_FILE="$ci_env" ZAZZZOR_EDGE=bad bash "$script_dir/stack.sh" config >"$tmp/bad" 2>&1; then
  fail 'неизвестный ZAZZZOR_EDGE принят'
fi
if ENV_FILE="$ci_env" ZAZZZOR_EDGE=caddy ZAZZZOR_API_REPOSITORY=ghcr.io/example/api \
  bash "$script_dir/stack.sh" config >"$tmp/edge-repository" 2>&1; then
  fail 'edge принял репозиторий, которого нет в его YAML'
fi

echo '- deploy не выключает молча TURN работающего стека'
# Подменный docker: работающий livekit с TCP-router TURN, как у ручного стека.
mkdir "$tmp/bin"
cat >"$tmp/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  *Swarm.LocalNodeState*) echo active ;;
  *Swarm.ControlAvailable*) echo true ;;
  *Swarm.NodeID*) echo test-node ;;
  'service ls '*) echo zazzzor_livekit ;;
  'service inspect '*) echo 'HostSNI(`turn.example.invalid`)' ;;
  'image inspect '*) ;;
  *) echo "unexpected mutation: $*" >&2; exit 99 ;;
esac
EOF
chmod +x "$tmp/bin/docker"
if PATH="$tmp/bin:$PATH" ENV_FILE="$ci_env" ZAZZZOR_EDGE=traefik LIVEKIT_NODE_IP="$node_ip" \
  bash "$script_dir/stack.sh" deploy >"$tmp/turn-guard" 2>&1; then
  fail 'deploy без LIVEKIT_TURN_ENABLED выключил TURN работающего стека'
fi
expect "$tmp/turn-guard" 'LIVEKIT_TURN_ENABLED=true' 'ошибка не говорит, как сохранить TURN'
reject "$tmp/turn-guard" 'unexpected mutation' 'deploy изменил стек до проверки TURN'
# Явное false — осознанное выключение: проверка TURN его пропускает, и
# deploy доходит до следующей (каталога копий из .env.ci нет).
PATH="$tmp/bin:$PATH" ENV_FILE="$ci_env" ZAZZZOR_EDGE=traefik LIVEKIT_NODE_IP="$node_ip" LIVEKIT_TURN_ENABLED=false \
  bash "$script_dir/stack.sh" deploy >"$tmp/turn-off" 2>&1 || true
reject "$tmp/turn-off" 'LIVEKIT_TURN_ENABLED=true' 'явное LIVEKIT_TURN_ENABLED=false не принято'
reject "$tmp/turn-off" 'unexpected mutation' 'deploy изменил стек до проверки каталога копий'
expect "$tmp/turn-off" 'ZAZZZOR_BACKUP_DIR\|каталог' 'deploy с LIVEKIT_TURN_ENABLED=false не дошёл до проверки каталога копий'

echo 'stack tests: OK (example, duplicates, edge, Traefik, TURN, repositories, TURN guard)'
