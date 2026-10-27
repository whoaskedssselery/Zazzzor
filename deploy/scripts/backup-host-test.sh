#!/usr/bin/env bash
# Проверяет порядок действий deploy/scripts/backup.sh в обоих режимах
# (compose и swarm) с подменённой командой docker: настоящий Docker не
# нужен и не затрагивается. Подмена записывает каждый шаг в $STATE.
#
# Каталог копий здесь, как на сервере, недоступен вызывающему для чтения
# (chmod 0000): backup.sh не должен заглядывать внутрь. На Linux проверка
# идёт так же, как в CI, — под UID 65532, владельцем каталога:
#   docker run --rm --user 65532:65532 -v "$PWD:/repo:ro" -w /repo \
#     postgres:<POSTGRES_VERSION из deploy/.env.ci> bash deploy/scripts/backup-host-test.sh
# Или прямо (в Git Bash права каталога не действуют):
#   bash deploy/scripts/backup-host-test.sh
# Печатает имя каждой проверки и в конце «backup host tests: OK».
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_root="$(mktemp -d)"
trap 'chmod -R u+rwx "$test_root" 2>/dev/null; rm -rf -- "$test_root"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

mkdir -p "$test_root/bin"
cat > "$test_root/bin/docker" <<'EOF'
#!/usr/bin/env bash
# Подменный docker: печатает то, что спрашивает backup.sh, и пишет шаги.
set -euo pipefail
args="$*"
step() {
  echo "$1" >> "$STATE"
}
case "$args" in
  info*) echo true ;;
  "volume inspect"*) ;;
  ps*_postgres*) echo test-postgres ;;
  ps*) ;;
  *" ps "*) ;;
  *"service inspect"*Image*) echo zazzzor-api:test ;;
  *"service inspect"*Replicas*) echo 1 ;;
  *"service inspect"*Env*) printf 'ZAZZZOR_DATABASE_URL=postgres://test\nZAZZZOR_ALLOWED_ORIGINS=https://test.invalid\n' ;;
  *"service scale"*=0) step stop ;;
  *"service scale"*) step start ;;
  *schema-version*) echo 9 ;;
  *" stop api"*) step stop ;;
  *" up -d "*api*) step start ;;
  *" check "*)
    step check
    [[ "${FAIL_CHECK:-0}" != 1 ]] || exit 2
    ;;
  *" restore "*)
    step restore
    [[ "${FAIL_RESTORE:-0}" != 1 ]] || exit "${FAIL_RESTORE_CODE:-2}"
    ;;
  *"files reconcile"*)
    step reconcile
    [[ "${FAIL_RECONCILE:-0}" != 1 ]] || exit 1
    ;;
  *" once"*) step once ;;
  *) echo "unexpected docker $args" >&2; exit 3 ;;
esac
EOF
chmod +x "$test_root/bin/docker"
export PATH="$test_root/bin:$PATH" STATE="$test_root/state"
export ENV_FILE="$script_dir/../.env.ci" STACK_NAME=zazzzor-host-test
export ZAZZZOR_BACKUP_DIR="$test_root/backups"
name=20261027T043000Z-test
mkdir -p "$ZAZZZOR_BACKUP_DIR/$name" "$test_root/elsewhere/$name"
# Как каталог 0700 UID 65532 для пользователя хоста: внутрь не заглянуть.
chmod 0000 "$ZAZZZOR_BACKUP_DIR"

# expect сравнивает записанные шаги с ожидаемыми.
expect() {
  local want
  want="$(printf '%s\n' "$@")"
  [[ "$(cat "$STATE")" == "$want" ]] || fail "шаги: $(tr '\n' ' ' < "$STATE"), ожидались: $*"
}

for mode in compose swarm; do
  echo "- $mode: без --yes ничего не делается"
  : > "$STATE"
  if bash "$script_dir/backup.sh" "$mode" restore "$name" 2>/dev/null; then fail 'восстановление без --yes'; fi
  [[ ! -s "$STATE" ]] || fail "без --yes выполнены шаги: $(cat "$STATE")"

  echo "- $mode: проверка, остановка API, база и файлы, сверка, запуск API"
  : > "$STATE"
  bash "$script_dir/backup.sh" "$mode" restore "$name" --yes >/dev/null
  expect check stop restore reconcile start

  echo "- $mode: копия новее образа отвергается до остановки API"
  : > "$STATE"
  if FAIL_CHECK=1 bash "$script_dir/backup.sh" "$mode" restore "$name" --yes >/dev/null 2>&1; then fail 'принята непрошедшая проверку копия'; fi
  expect check

  echo "- $mode: неудача восстановления базы оставляет API остановленным"
  : > "$STATE"
  if out="$(FAIL_RESTORE=1 bash "$script_dir/backup.sh" "$mode" restore "$name" --yes 2>&1)"; then fail 'неудача восстановления скрыта'; fi
  expect check stop restore
  grep -q 'повторите' <<<"$out" || fail "нет следующего шага в сообщении: $out"

  echo "- $mode: данные не изменены (код 3): API запущен снова, код не 0"
  : > "$STATE"
  if out="$(FAIL_RESTORE=1 FAIL_RESTORE_CODE=3 bash "$script_dir/backup.sh" "$mode" restore "$name" --yes 2>&1)"; then fail 'неудача восстановления скрыта'; fi
  expect check stop restore start
  grep -q 'Данные не изменены' <<<"$out" || fail "нет объяснения, что данные не изменены: $out"

  echo "- $mode: неудача сверки файлов: API запущен, предупреждение, код не 0"
  : > "$STATE"
  if out="$(FAIL_RECONCILE=1 bash "$script_dir/backup.sh" "$mode" restore "$name" --yes 2>&1)"; then fail 'неудача сверки скрыта'; fi
  expect check stop restore reconcile start
  grep -q 'files reconcile' <<<"$out" || fail "предупреждение не говорит, что повторить: $out"

  echo "- $mode: копия сейчас"
  : > "$STATE"
  bash "$script_dir/backup.sh" "$mode" backup >/dev/null
  expect once
done

echo '- путь к копии: со слешем, относительный и абсолютный'
for arg in "$name/" "$ZAZZZOR_BACKUP_DIR/$name" "$ZAZZZOR_BACKUP_DIR/$name/"; do
  : > "$STATE"
  bash "$script_dir/backup.sh" compose restore "$arg" --yes >/dev/null || fail "не принят путь $arg"
done
for arg in "backups/$name" "./backups/$name/" "elsewhere/../backups/$name"; do
  : > "$STATE"
  (cd "$test_root" && bash "$script_dir/backup.sh" compose restore "$arg" --yes >/dev/null) || fail "не принят относительный путь $arg"
done

if [[ "$(uname -s)" == Linux ]]; then
  echo '- нет каталога копий: команда install, задание не запускается'
  : > "$STATE"
  if out="$(ZAZZZOR_BACKUP_DIR="$test_root/missing" bash "$script_dir/backup.sh" compose backup 2>&1)"; then fail 'копия в несуществующий каталог'; fi
  grep -q "sudo install -d -o 65532" <<<"$out" || fail "нет команды install: $out"
  [[ ! -s "$STATE" ]] || fail "без каталога копий запущено задание: $(cat "$STATE")"
fi

echo '- копия вне ZAZZZOR_BACKUP_DIR: команда, которая её туда перенесёт'
: > "$STATE"
if out="$(bash "$script_dir/backup.sh" compose restore "$test_root/elsewhere/$name" --yes 2>&1)"; then fail 'принята копия вне каталога'; fi
grep -q "sudo chown -R 65532:65532" <<<"$out" || fail "нет команды переноса: $out"
[[ ! -s "$STATE" ]] || fail "для копии вне каталога выполнены шаги: $(cat "$STATE")"

echo 'backup host tests: OK'
