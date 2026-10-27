#!/usr/bin/env bash
# Проверяет deploy/scripts/backup-job.sh без Docker и без настоящей базы:
# pg_dump, psql и pg_restore подменены записью вызовов. Запускается в том же
# образе Postgres на Alpine (busybox), что и само задание, и под тем же UID
# 65532: только так права каталога проверяются честно (root пишет везде).
#
# Использование (из корня репозитория):
#   docker run --rm --user 65532:65532 -v "$PWD:/repo:ro" -w /repo \
#     postgres:<POSTGRES_VERSION из deploy/.env.ci> bash deploy/scripts/backup-job-test.sh
# Печатает имя каждой проверки и в конце «backup job tests: OK».
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
job="$script_dir/backup-job.sh"
test_root="$(mktemp -d)"
holder=''
scheduler=''

# Убирает фоновые процессы проверок и временный каталог.
cleanup() {
  if [[ -n "$holder" ]]; then
    kill "$holder" 2>/dev/null || true
  fi
  if [[ -n "$scheduler" ]]; then
    kill -KILL -- "-$scheduler" 2>/dev/null || true
  fi
  chmod -R u+w "$test_root" 2>/dev/null || true
  rm -rf -- "$test_root"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# Подменные pg_dump, psql и pg_restore пишут свои аргументы в $CALLS.
mkdir -p "$test_root/bin"
cat > "$test_root/bin/pg_dump" <<'EOF'
#!/bin/sh
if [ "${FAIL_DUMP:-0}" = 1 ]; then exit 1; fi
if [ "${HOLD_DUMP:-0}" = 1 ]; then touch "$BACKUP_ROOT/.dump-started"; exec sleep 300; fi
printf 'test dump'
EOF
cat > "$test_root/bin/psql" <<'EOF'
#!/bin/sh
case "$*" in
  *server_version*) echo '18.6' ;;
  *goose_db_version*) echo "${FAKE_SCHEMA:-9}" ;;
  *)
    echo "psql $*" >> "$CALLS"
    cat >> "$CALLS"
    ;;
esac
EOF
# pg_restore -f - пишет SQL в stdout, то есть в psql.
cat > "$test_root/bin/pg_restore" <<'EOF'
#!/bin/sh
echo "-- pg_restore $*"
if [ "${FAIL_PG_RESTORE:-0}" = 1 ]; then exit 1; fi
EOF
chmod +x "$test_root/bin/pg_dump" "$test_root/bin/psql" "$test_root/bin/pg_restore"
export PATH="$test_root/bin:$PATH"
export CALLS="$test_root/calls"
export TMPDIR="$test_root/tmp"
mkdir -p "$TMPDIR"

# fresh_dirs даёт каждой проверке пустые каталоги копий и данных.
fresh_dirs() {
  export BACKUP_ROOT="$test_root/backups-$1" DATA_ROOT="$test_root/data-$1"
  mkdir -p "$BACKUP_ROOT" "$DATA_ROOT/avatars/1" "$DATA_ROOT/icons" "$DATA_ROOT/trash"
  printf 'image' > "$DATA_ROOT/avatars/1/0123456789abcdef.png"
  : > "$CALLS"
}

# newest печатает имя самой новой готовой копии.
newest() {
  find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d ! -name '.*' -exec basename '{}' \; | sort | tail -n 1
}

# fake_backup создаёт пустую «готовую» копию с данным именем.
fake_backup() {
  mkdir -p "$BACKUP_ROOT/$1"
  touch "$BACKUP_ROOT/$1/manifest.json"
}

echo '- once публикует полную копию, время manifest — начало pg_dump'
fresh_dirs once
bash "$job" once >/dev/null
name="$(newest)"
for f in database.dump apidata.tar.gz manifest.json SHA256SUMS; do
  [[ -f "$BACKUP_ROOT/$name/$f" ]] || fail "в копии нет $f"
done
bash "$job" verify "$name" >/dev/null
tar -tzf "$BACKUP_ROOT/$name/apidata.tar.gz" | grep -q 'avatars/1/0123456789abcdef.png' || fail 'в архиве нет аватара'
stamp="$(sed -E 's/.*"time":"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})Z".*/\1\2\3T\4\5\6Z/' "$BACKUP_ROOT/$name/manifest.json")"
[[ "$name" == "$stamp"-* ]] || fail "время manifest $stamp не совпадает с именем $name"
grep -q '"schema_version":9' "$BACKUP_ROOT/$name/manifest.json" || fail 'в manifest нет schema_version'

echo '- испорченная копия не проходит проверку'
printf 'corruption' >> "$BACKUP_ROOT/$name/database.dump"
if bash "$job" verify "$name" 2>/dev/null; then fail 'испорченная копия принята'; fi

echo '- неудачный pg_dump не оставляет ни копии, ни .partial'
fresh_dirs faileddump
if FAIL_DUMP=1 bash "$job" once 2>/dev/null; then fail 'неудачный pg_dump принят'; fi
[[ -z "$(newest)" ]] || fail 'после неудачи опубликована копия'
[[ -z "$(find "$BACKUP_ROOT" -maxdepth 1 -name '.partial-*')" ]] || fail 'остался .partial'

echo '- блокировка убитого задания и его .partial не мешают следующему запуску'
fresh_dirs stale
bash -c 'exec 9>>"$1/.lock"; flock 9; kill -KILL $$' _ "$BACKUP_ROOT" || true
mkdir "$BACKUP_ROOT/.partial-killed"
bash "$job" once >/dev/null || fail 'запуск после убитого задания не удался'
[[ ! -e "$BACKUP_ROOT/.partial-killed" ]] || fail 'старый .partial не убран'

echo '- занятая блокировка называет причину'
fresh_dirs busy
flock "$BACKUP_ROOT/.lock" sleep 30 &
holder=$!
for _ in $(seq 1 50); do
  if ! flock -n "$BACKUP_ROOT/.lock" true; then break; fi
  sleep 0.1
done
if out="$(bash "$job" once 2>&1)"; then fail 'копия снята при занятой блокировке'; fi
grep -q 'уже идёт' <<<"$out" || fail "непонятная ошибка при занятой блокировке: $out"
kill "$holder"
wait "$holder" 2>/dev/null || true
holder=''

echo '- каталог без права записи: ошибка называет владельца, права и команду'
fresh_dirs readonly
chmod 0555 "$BACKUP_ROOT"
if out="$(BACKUP_HOST_DIR=/srv/zazzzor-backups bash "$job" once 2>&1)"; then fail 'копия в каталог без записи'; fi
chmod 0755 "$BACKUP_ROOT"
for want in "права 555" "UID $(id -u)" "sudo chown 65532:65532 '/srv/zazzzor-backups'"; do
  grep -qF "$want" <<<"$out" || fail "в ошибке нет «$want»: $out"
done

echo '- архив повторяется, если файл исчез во время чтения'
fresh_dirs vanish
cat > "$test_root/bin/tar" <<'EOF'
#!/bin/sh
if [ "$1" = -czf ] && [ ! -f "$TMPDIR/tar-failed" ]; then
  touch "$TMPDIR/tar-failed"
  echo 'tar: avatars/1/x.png: No such file or directory' >&2
  exit 1
fi
exec /bin/tar "$@"
EOF
chmod +x "$test_root/bin/tar"
bash "$job" once >/dev/null 2>&1 || fail 'одна неудача tar сорвала копию'
rm -f "$test_root/bin/tar"
[[ -f "$TMPDIR/tar-failed" ]] || fail 'подменный tar не вызывался'
[[ -n "$(newest)" ]] || fail 'после повтора tar копии нет'

echo '- healthcheck: свежая копия — здоров, старая — нет, идущая копия — здоров'
fresh_dirs health
fake_backup "$(date -u +%Y%m%dT%H%M%SZ)-fresh"
bash "$job" health >/dev/null || fail 'свежая копия считается несвежей'
fresh_dirs stalehealth
fake_backup 20200101T043000Z-old
if out="$(bash "$job" health 2>&1)"; then fail 'старая копия считается свежей'; fi
grep -q '20200101T043000Z-old' <<<"$out" || fail "healthcheck не называет последнюю копию: $out"
flock "$BACKUP_ROOT/.lock" sleep 30 &
holder=$!
sleep 0.5
bash "$job" health >/dev/null || fail 'во время копии healthcheck не здоров'
kill "$holder"
wait "$holder" 2>/dev/null || true
holder=''

echo '- healthcheck: копий нет, расписание запущено давно — не здоров'
fresh_dirs nobackup
echo $(($(date +%s) - 27 * 3600)) > "$BACKUP_ROOT/.schedule-started"
if bash "$job" health >/dev/null 2>&1; then fail 'нет копий за 27 ч, но healthcheck здоров'; fi
date +%s > "$BACKUP_ROOT/.schedule-started"
bash "$job" health >/dev/null || fail 'только что запущенное расписание считается больным'

echo '- перезапуск расписания не сбрасывает отсчёт healthcheck'
fresh_dirs restart
echo $(($(date +%s) - 27 * 3600)) > "$BACKUP_ROOT/.schedule-started"
FAIL_DUMP=1 setsid bash "$job" schedule >/dev/null 2>&1 &
scheduler=$!
sleep 1
kill -TERM "$scheduler"
wait "$scheduler" || true
scheduler=''
if bash "$job" health >/dev/null 2>&1; then fail 'перезапуск расписания сбросил отсчёт: healthcheck здоров'; fi

echo '- копия старше суток снимается сразу, не дожидаясь времени'
fresh_dirs overdue
fake_backup 20200101T043000Z-old
HOLD_DUMP=1 ZAZZZOR_BACKUP_TIME=23:59 setsid bash "$job" schedule >/dev/null 2>&1 &
scheduler=$!
for _ in $(seq 1 100); do
  [[ ! -f "$BACKUP_ROOT/.dump-started" ]] || break
  sleep 0.05
done
kill -TERM "$scheduler"
wait "$scheduler" || true
scheduler=''
[[ -f "$BACKUP_ROOT/.dump-started" ]] || fail 'при копии старше суток расписание ждёт времени'

echo '- хранятся последние копии по дням и ISO-неделям'
fresh_dirs prune
for day in $(seq -w 1 20); do
  fake_backup "202610${day}T043000Z-test"
done
ZAZZZOR_BACKUP_KEEP_DAILY=2 ZAZZZOR_BACKUP_KEEP_WEEKLY=2 bash "$job" prune
[[ -d "$BACKUP_ROOT/20261020T043000Z-test" && -d "$BACKUP_ROOT/20261019T043000Z-test" ]] || fail 'удалены последние дни'
[[ -d "$BACKUP_ROOT/20261018T043000Z-test" ]] || fail 'удалена последняя копия прошлой недели'
[[ ! -d "$BACKUP_ROOT/20261001T043000Z-test" ]] || fail 'старая копия не удалена'

echo '- неудачная очистка после публикации не выдаётся за неудачную копию'
fresh_dirs prunefail
fake_backup 20200101T043000Z-old
touch "$BACKUP_ROOT/20200101T043000Z-old/locked"
chmod 0555 "$BACKUP_ROOT/20200101T043000Z-old"
if out="$(ZAZZZOR_BACKUP_KEEP_DAILY=1 ZAZZZOR_BACKUP_KEEP_WEEKLY=1 bash "$job" once 2>&1)"; then
  fail 'неудачная очистка не дала ошибку'
fi
chmod 0755 "$BACKUP_ROOT/20200101T043000Z-old"
grep -q 'сохранена' <<<"$out" || fail "сообщение не говорит, что копия сохранена: $out"
[[ "$(newest)" != 20200101T043000Z-old ]] || fail 'новая копия не опубликована'

echo '- в архиве не должно быть путей наружу и ссылок'
fresh_dirs unsafe
for kind in symlink hardlink outside; do
  bash "$job" once >/dev/null
  name="$(newest)"
  rm -rf "$test_root/evil"
  mkdir -p "$test_root/evil/avatars/1"
  printf 'x' > "$test_root/evil/avatars/1/a.png"
  case "$kind" in
    symlink) ln -s /etc/passwd "$test_root/evil/avatars/1/link.png" ;;
    hardlink) ln "$test_root/evil/avatars/1/a.png" "$test_root/evil/avatars/1/b.png" ;;
    outside) printf 'x' > "$test_root/evil/outside" ;;
  esac
  (cd "$test_root/evil" && tar -czf "$BACKUP_ROOT/$name/apidata.tar.gz" -- *)
  (cd "$BACKUP_ROOT/$name" && sha256sum database.dump apidata.tar.gz manifest.json > SHA256SUMS)
  if bash "$job" verify "$name" >/dev/null 2>&1; then fail "архив с $kind принят"; fi
  rm -rf -- "${BACKUP_ROOT:?}/$name"
done
if bash "$job" verify ../outside >/dev/null 2>&1; then fail 'имя с ../ принято'; fi

echo '- копия с более новой схемой, чем у образа, отвергается до восстановления'
fresh_dirs schema
FAKE_SCHEMA=9 ZAZZZOR_APP_IMAGE=zazzzor-api:new bash "$job" once >/dev/null
name="$(newest)"
if out="$(ZAZZZOR_SCHEMA_VERSION=5 bash "$job" check "$name" 2>&1)"; then fail 'копия новее образа принята'; fi
grep -q 'zazzzor-api:new' <<<"$out" || fail "ошибка не называет образ из manifest: $out"
ZAZZZOR_SCHEMA_VERSION=9 bash "$job" check "$name" >/dev/null || fail 'копия той же схемы отвергнута'
ZAZZZOR_SCHEMA_VERSION=12 bash "$job" check "$name" >/dev/null || fail 'копия старше образа отвергнута'

echo '- restore: одна транзакция (схема заново, pg_restore --no-owner), затем картинки'
fresh_dirs restore
bash "$job" once >/dev/null
name="$(newest)"
rm -f "$DATA_ROOT/avatars/1/0123456789abcdef.png"
printf 'stray' > "$DATA_ROOT/icons/stray"
mkdir "$DATA_ROOT/lost+found"
ZAZZZOR_SCHEMA_VERSION=9 bash "$job" restore "$name" >/dev/null
want="$(printf '%s\n' 'BEGIN;' 'DROP SCHEMA public CASCADE;' 'CREATE SCHEMA public;' \
  "-- pg_restore --no-owner -f - $BACKUP_ROOT/$name/database.dump" 'COMMIT;')"
[[ "$(sed 1d "$CALLS")" == "$want" ]] || fail "psql получил не то: $(cat "$CALLS")"
[[ "$(head -n 1 "$CALLS")" == psql*ON_ERROR_STOP=1* ]] || fail "psql без ON_ERROR_STOP: $(head -n 1 "$CALLS")"
[[ -f "$DATA_ROOT/avatars/1/0123456789abcdef.png" ]] || fail 'файлы не восстановлены'
[[ ! -e "$DATA_ROOT/icons/stray" ]] || fail 'лишний файл остался после замены'
[[ -d "$DATA_ROOT/lost+found" ]] || fail 'restore удалил постороннее (lost+found)'
[[ -z "$(find "$DATA_ROOT" -maxdepth 1 -name '.restore-*')" ]] || fail 'остались .restore-*'

echo '- restore: неудачный pg_restore — без COMMIT, код 3, картинки не тронуты'
fresh_dirs restorefail
bash "$job" once >/dev/null
name="$(newest)"
printf 'stray' > "$DATA_ROOT/icons/stray"
: > "$CALLS"
status=0
FAIL_PG_RESTORE=1 ZAZZZOR_SCHEMA_VERSION=9 bash "$job" restore "$name" >/dev/null 2>&1 || status=$?
((status == 3)) || fail "код неудачного восстановления базы $status, ожидался 3"
if grep -q 'COMMIT' "$CALLS"; then fail 'неудачный pg_restore закоммичен'; fi
[[ -f "$DATA_ROOT/icons/stray" ]] || fail 'картинки заменены, хотя база не восстановилась'

echo '- restore: занятая блокировка — код 3, ничего не изменено'
flock "$BACKUP_ROOT/.lock" sleep 30 &
holder=$!
sleep 0.5
: > "$CALLS"
status=0
ZAZZZOR_SCHEMA_VERSION=9 bash "$job" restore "$name" >/dev/null 2>&1 || status=$?
kill "$holder"
wait "$holder" 2>/dev/null || true
holder=''
((status == 3)) || fail "код восстановления при занятой блокировке $status, ожидался 3"
[[ ! -s "$CALLS" ]] || fail "при занятой блокировке тронута база: $(cat "$CALLS")"

echo '- непрочитываемая копия: ошибка называет chown -R для неё'
fresh_dirs unreadable
bash "$job" once >/dev/null
name="$(newest)"
chmod 0000 "$BACKUP_ROOT/$name"
status=0
out="$(BACKUP_HOST_DIR=/srv/zazzzor-backups bash "$job" verify "$name" 2>&1)" || status=$?
chmod 0700 "$BACKUP_ROOT/$name"
((status != 0)) || fail 'непрочитываемая копия принята'
grep -qF "sudo chown -R 65532:65532 '/srv/zazzzor-backups/$name'" <<<"$out" || fail "нет команды chown -R: $out"

echo '- неверное время расписания отвергается'
if ZAZZZOR_BACKUP_TIME=99:99 bash "$job" schedule 2>/dev/null; then fail 'время 99:99 принято'; fi

echo '- TERM останавливает расписание вместе с идущим pg_dump'
fresh_dirs term
HOLD_DUMP=1 ZAZZZOR_BACKUP_TIME=00:00 setsid bash "$job" schedule >/dev/null 2>&1 &
scheduler=$!
for _ in $(seq 1 100); do
  [[ ! -f "$BACKUP_ROOT/.dump-started" ]] || break
  sleep 0.05
done
[[ -f "$BACKUP_ROOT/.dump-started" ]] || fail 'плановый pg_dump не начался'
kill -TERM "$scheduler"
for _ in $(seq 1 60); do
  kill -0 "$scheduler" 2>/dev/null || break
  sleep 0.05
done
if kill -0 "$scheduler" 2>/dev/null; then fail 'расписание не остановило pg_dump по TERM'; fi
wait "$scheduler" || true
scheduler=''
[[ -z "$(find "$BACKUP_ROOT" -maxdepth 1 -name '.partial-*')" ]] || fail 'после TERM остался .partial'
flock -n "$BACKUP_ROOT/.lock" true || fail 'после TERM блокировка занята'

echo 'backup job tests: OK'
