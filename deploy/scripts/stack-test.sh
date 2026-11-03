#!/usr/bin/env bash
# Проверяет оба Swarm-варианта и путь владельца по deploy/README.md §17 с
# фальшивыми значениями, без развёртывания.
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

# set_key <файл> <ключ> <значение> делает то, что README просит у человека:
# находит строку ключа (закомментированную или нет), убирает «# » и меняет
# значение. Нет такой строки — README велит раскомментировать то, чего нет.
set_key() {
  local file="$1" key="$2" value="$3"
  grep -Eq "^(# )?$key=" "$file" || fail "в $(basename "$file") нет строки $key, которую README велит раскомментировать"
  awk -v key="$key" -v value="$value" '
    !done && ($0 ~ "^(# )?" key "=") { print key "=" value; done = 1; next }
    { print }' "$file" >"$file.new"
  mv -- "$file.new" "$file"
}

# readme_block <метка>: строки блока кода сразу после <!-- метка --> в README.
readme_block() {
  awk -v marker="<!-- $1 -->" '
    index($0, marker) { found = 1; next }
    found && /```/ { if (inside) exit; inside = 1; next }
    inside { sub(/^[[:space:]]+/, ""); print }' "$deploy_dir/README.md"
}

# apply_readme <файл> <метка>: применяет блок README построчно через set_key.
apply_readme() {
  local file="$1" line n=0
  while IFS= read -r line; do
    [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || fail "README, блок $2: не ИМЯ=значение: $line"
    set_key "$file" "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
    n=$((n + 1))
  done < <(readme_block "$2")
  ((n > 0)) || fail "в README нет блока <!-- $2 -->"
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
# Traefik v3 skips a service whose labels mix both providers ("both Docker and Swarm labels are defined").
reject "$tmp/traefik" 'traefik.docker.' 'labels Docker-провайдера рядом со Swarm: Traefik пропустит сервисы'
expect "$tmp/traefik" 'traefik.swarm.network: traefik' 'сервисы не указывают сеть Traefik для Swarm-провайдера'
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

echo '- приватный голосовой control path с TURN и без него'
for config in "$tmp/traefik" "$tmp/turn"; do
  service "$config" api >"$tmp/api"
  service "$config" livekit >"$tmp/livekit"
  service "$config" web >"$tmp/web"
  expect "$tmp/api" 'ZAZZZOR_LIVEKIT_URL: http://voice-sfu:7880' 'API не подключён к приватному LiveKit'
  expect "$tmp/api" 'ZAZZZOR_LIVEKIT_API_KEY: APItestonly' 'API не получил ключ LiveKit'
  expect "$tmp/api" 'ZAZZZOR_LIVEKIT_API_SECRET: deadbeef' 'API не получил секрет LiveKit'
  expect "$tmp/api" 'voice-api' 'нет приватного alias API'
  expect "$tmp/livekit" 'voice-sfu' 'нет приватного alias LiveKit'
  expect "$tmp/livekit" 'api_key: APItestonly' 'webhook подписан другим ключом'
  expect "$tmp/livekit" 'http://voice-api:8080/api/v1/livekit/webhook' 'webhook не идёт напрямую в API'
  for name in api livekit; do
    service "$config" "$name" | grep -q '^      voice:' || fail "$name не в control-сети voice"
  done
  for name in web postgres backup; do
    service "$config" "$name" >"$tmp/other"
    reject "$tmp/other" '^      voice:' "$name попал в control-сеть voice"
  done
  networks "$config" >"$tmp/networks"
  expect "$tmp/networks" '^  voice:' 'нет отдельной control-сети'
  grep -A3 '^  voice:' "$tmp/networks" | grep -q 'internal: true' || fail 'voice не внутренняя'
  expect "$tmp/web" 'traefik.http.routers.zazzzor-webhook.priority: "200"' 'блокировка webhook не перекрывает API'
  expect "$tmp/web" "Path(\`/api/v1/livekit/webhook\`)" 'нет точного router webhook'
  expect "$tmp/web" 'traefik.http.routers.zazzzor-webhook.service: zazzzor-web' 'webhook попадает не в web404'
  expect "$tmp/web" 'traefik.http.routers.zazzzor-webhook.tls: "true"' 'webhook router без TLS'
  reject "$tmp/api" '^    ports:' 'API опубликовал host-порт'
done

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

echo '- .env по README §17 разворачивает Traefik-стек'
# Как владелец: копия примера, обязательные поля §5, затем блоки README
# «раскомментируйте и измените» (основной и TURN).
readme_env="$tmp/readme.env"
cp "$deploy_dir/.env.example" "$readme_env"
set_key "$readme_env" ZAZZZOR_DOMAIN zazzzor-readme.example.invalid
set_key "$readme_env" LIVEKIT_API_KEY APIreadmetest
set_key "$readme_env" LIVEKIT_API_SECRET deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef
set_key "$readme_env" POSTGRES_PASSWORD cafecafecafecafecafecafecafecafecafecafecafecafe
# Ошибка владельца: блок дописан в конец, а не раскомментирован. Ни одна
# строка примера не должна его перебить: результат тот же Traefik-стек.
cp "$readme_env" "$tmp/appended.env"
readme_block traefik-env >>"$tmp/appended.env"
ENV_FILE="$tmp/appended.env" bash "$script_dir/stack.sh" config >"$tmp/appended" ||
  fail 'stack.sh config не отрисовал .env с дописанным блоком README'
expect "$tmp/appended" 'Caddyfile.web' '.env с дописанным блоком README разворачивает не Traefik-стек'
apply_readme "$readme_env" traefik-env
apply_readme "$readme_env" traefik-turn-env
ENV_FILE="$readme_env" bash "$script_dir/stack.sh" config >"$tmp/readme" ||
  fail 'stack.sh config не отрисовал .env, собранный по README'
reject "$tmp/readme" '\$''{' 'остались переменные в стеке по README'
expect "$tmp/readme" 'Caddyfile.web' '.env по README разворачивает не Traefik-стек'
reject "$tmp/readme" 'duckdns:' '.env по README разворачивает edge-стек с DuckDNS'
expect "$tmp/readme" 'image: ghcr.io/whoaskedssselery/zazzzor-api:ci' '.env по README берёт API не из GHCR'
expect "$tmp/readme" 'image: ghcr.io/whoaskedssselery/zazzzor-caddy:ci' '.env по README берёт web не из GHCR'
expect "$tmp/readme" 'traefik.tcp.routers.zazzzor-turn.rule: HostSNI' '.env по README выключил TURN'

echo 'stack tests: OK (example, duplicates, edge, Traefik, TURN, repositories, TURN guard, README)'
