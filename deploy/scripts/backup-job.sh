#!/usr/bin/env bash
# Резервные копии Zazzzor: задание сервиса backup. Работает внутри образа
# Postgres на Alpine (bash и busybox: flock, tar, sha256sum, date) под UID
# 65532, как API. Пароль базы (PGPASSWORD) не печатает.
#
# Копия — каталог <ГГГГММДДTччммссZ>-<суффикс> в BACKUP_ROOT: сначала
# pg_dump -Fc, потом архив изображений вместе с корзиной, затем manifest.json
# и SHA256SUMS. Пишется в .partial-*, публикуется переименованием.
#
# Использование:
#   backup-job.sh schedule        каждый день в ZAZZZOR_BACKUP_TIME (по TZ); если
#                                 сегодняшняя копия пропущена или последней больше
#                                 суток, снимает сразу; неудачную повторяет через 30 мин
#   backup-job.sh once            одна копия сейчас (не дольше часа)
#   backup-job.sh health          healthcheck: 0, если последняя копия моложе 26 ч
#   backup-job.sh prune           удалить копии сверх хранимых
#   backup-job.sh verify <имя>    проверить суммы и архив копии
#   backup-job.sh check <имя>     verify и сравнить схему копии со схемой образа API
#   backup-job.sh restore <имя>   check, затем база и изображения из копии (API в это
#                                 время остановлен); код 3 — ничего не изменено
#
# Переменные:
#   BACKUP_ROOT (/backups), DATA_ROOT (/data)    каталоги внутри контейнера
#   BACKUP_HOST_DIR               тот же каталог копий на хосте, для сообщений
#   ZAZZZOR_BACKUP_TIME (04:30), TZ               время ежедневной копии
#   ZAZZZOR_BACKUP_KEEP_DAILY (7)                 сколько последних дней хранить
#   ZAZZZOR_BACKUP_KEEP_WEEKLY (4)                сколько последних недель хранить
#   ZAZZZOR_APP_IMAGE             образ API, записывается в manifest.json
#   ZAZZZOR_SCHEMA_VERSION        для check и restore: zazzzor schema-version образа
#   PGHOST, PGPORT, PGUSER, PGPASSWORD, PGDATABASE   подключение к Postgres
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/scripts/lib.sh
source "$script_dir/lib.sh"

root="${BACKUP_ROOT:-/backups}"
data="${DATA_ROOT:-/data}"
host_root="${BACKUP_HOST_DIR:-$root}"
keep_daily="${ZAZZZOR_BACKUP_KEEP_DAILY:-7}"
keep_weekly="${ZAZZZOR_BACKUP_KEEP_WEEKLY:-4}"
lock="$root/.lock"
# Когда расписание запустилось впервые: healthcheck судит по нему, пока копий
# нет. Лежит в каталоге копий, а не в /tmp, чтобы перезапуск задачи (Swarm
# перезапускает нездоровую) или хоста не сбрасывал этот отсчёт.
started_file="$root/.schedule-started"
# Копия раз в сутки плюс запас на её длительность.
max_age_hours=26
# Пауза перед повтором неудачной плановой копии.
retry_seconds=1800
# Зависшая копия не должна держать блокировку вечно.
once_timeout_seconds=3600
name_re='^[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9]+$'

tmp=''
published=''
phase=''
schedule_child=''

log() {
  echo "backup: $*"
}

valid_name() {
  [[ "$1" =~ $name_re ]]
}

validate_retention() {
  if [[ ! "$keep_daily" =~ ^[1-9][0-9]?$ || ! "$keep_weekly" =~ ^[1-9][0-9]?$ ]]; then
    die "ZAZZZOR_BACKUP_KEEP_DAILY и ZAZZZOR_BACKUP_KEEP_WEEKLY — числа от 1 до 99, сейчас: $keep_daily и $keep_weekly"
  fi
}

# Каталог копий должен существовать и быть доступен на запись UID задания.
# Ошибка называет владельца, права и команду, которая это исправит.
require_writable_root() {
  local owner mode
  if [[ ! -d "$root" ]]; then
    die "нет каталога копий $host_root. Создайте его на хосте: sudo install -d -o $backup_uid -g $backup_uid -m 0700 '$host_root'"
  fi
  if [[ ! -w "$root" || ! -x "$root" ]]; then
    owner="$(stat -c %u:%g "$root")"
    mode="$(stat -c %a "$root")"
    die "нет права записи в каталог копий $host_root: владелец $owner, права $mode, а задание работает под UID $(id -u). Выполните на хосте: sudo chown $backup_uid:$backup_uid '$host_root' && sudo chmod 0700 '$host_root'"
  fi
}

# Одна копия или восстановление за раз. Блокировку flock держит ядро: если
# процесс убит, она освобождается сама, и следующий запуск не застревает.
# take_lock [код] выходит с этим кодом (по умолчанию 2), если блокировка занята.
take_lock() {
  exec 9>>"$lock"
  if ! flock -n 9; then
    echo "Ошибка: уже идёт другая копия или восстановление (занят $host_root/.lock). Дождитесь конца и повторите" >&2
    exit "${1:-2}"
  fi
}

# Последняя готовая копия старше суток или копий нет: тогда расписание
# снимает копию сразу, не дожидаясь времени. Иначе после простоя сервер до
# этого времени оставался бы без свежей копии и нездоровым.
backup_overdue() {
  local last ts
  last="$(newest_backup)"
  [[ -n "$last" ]] || return 0
  ts="$(date -u -d "${last:0:4}-${last:4:2}-${last:6:2} ${last:9:2}:${last:11:2}:${last:13:2}" +%s)"
  (($(date +%s) - ts > 24 * 3600))
}

# Печатает имя самой новой готовой копии (или ничего).
newest_backup() {
  local name last=''
  while IFS= read -r name; do
    if valid_name "$name" && [[ -f "$root/$name/manifest.json" && ! -L "$root/$name" ]]; then
      last="$name"
    fi
  done < <(find "$root" -mindepth 1 -maxdepth 1 -type d -exec basename '{}' \; | sort)
  printf '%s' "$last"
}

# Перед копией нужно место хотя бы на ещё одну такую же, как последняя
# (и не меньше 64 МиБ), иначе pg_dump упадёт посреди записи.
check_space() {
  local last need=65536 size free
  last="$(newest_backup)"
  if [[ -n "$last" ]]; then
    size="$(du -sk "$root/$last" | cut -f1)"
    need=$((size > need ? size : need))
  fi
  free="$(df -Pk "$root" | awk 'NR == 2 { print $4 }')"
  if ((free < need)); then
    die "мало места в $host_root: свободно $((free / 1024)) МиБ, нужно не меньше $((need / 1024)) МиБ. Освободите место или храните меньше копий (ZAZZZOR_BACKUP_KEEP_DAILY, ZAZZZOR_BACKUP_KEEP_WEEKLY)"
  fi
}

# Версия схемы базы: последняя применённая миграция goose, в пустой базе 0.
schema_version() {
  local table version
  table="$(psql -X -Atq -v ON_ERROR_STOP=1 -c "SELECT to_regclass('public.goose_db_version')")"
  if [[ -z "$table" ]]; then
    echo 0
    return
  fi
  version="$(psql -X -Atq -v ON_ERROR_STOP=1 -c 'SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied')"
  [[ "$version" =~ ^[0-9]+$ ]] || die "непонятная версия схемы базы: $version"
  echo "$version"
}

# Архивирует изображения: живые каталоги раньше корзины. Если во время
# чтения файл уходит в корзину, он окажется в ней, а корзина читается позже.
# Busybox tar падает на файле, исчезнувшем посреди обхода (удаление или
# очистка корзины в API); тогда архив снимается заново, до трёх раз.
archive_files() {
  local out="$1" attempt
  for attempt in 1 2 3; do
    if tar -czf "$out" -C "$data" avatars icons trash; then
      return 0
    fi
    log "архив изображений не удался (попытка $attempt из 3)"
  done
  return 1
}

# Итог once для лога: какой этап не удался и что осталось на диске.
on_once_exit() {
  local status=$?
  trap - EXIT
  if [[ -n "$tmp" ]]; then
    rm -rf -- "$tmp"
  fi
  if ((status != 0)); then
    case "$phase" in
      prune) echo "backup: копия $published сохранена, но удалить старые копии не удалось (ошибка выше); проверьте место на диске" >&2 ;;
      *) echo "backup: копия НЕ снята (этап: ${phase:-подготовка}); неполные файлы удалены" >&2 ;;
    esac
  fi
  exit "$status"
}

# Одна копия. Порядок важен: сначала pg_dump, потом картинки, так что всё,
# на что ссылается дамп, уже записано или лежит в корзине, которая тоже
# попадает в архив. Пишет в .partial-*, публикует одним переименованием:
# неполная копия никогда не выглядит готовой. Затем удаляет лишние старые.
once() {
  local started name tag pg schema dump_size data_size
  validate_retention
  require_writable_root
  take_lock
  # Блокировка у нас, значит .partial-* остались от прерванных запусков.
  find "$root" -mindepth 1 -maxdepth 1 -name '.partial-*' -exec rm -rf -- '{}' +
  check_space
  tag="${ZAZZZOR_APP_IMAGE:-unknown}"
  [[ "$tag" =~ ^[A-Za-z0-9_./:@+-]+$ ]] || die "непонятный образ API в ZAZZZOR_APP_IMAGE: $tag"
  trap on_once_exit EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  tmp="$(mktemp -d "$root/.partial-XXXXXXXX")"
  pg="$(psql -X -Atq -v ON_ERROR_STOP=1 -c 'SHOW server_version')"
  [[ "$pg" =~ ^[A-Za-z0-9_.+()[:space:]-]+$ ]] || die "непонятная версия Postgres: $pg"
  schema="$(schema_version)"
  # Время копии — начало pg_dump: оно определяет, что в неё попало (RPO).
  started="$(date -u +%Y%m%dT%H%M%SZ)"
  name="$started-${tmp##*.partial-}"
  phase=pg_dump
  pg_dump -Fc > "$tmp/database.dump"
  phase=архив
  archive_files "$tmp/apidata.tar.gz"
  phase=manifest
  dump_size="$(wc -c < "$tmp/database.dump")"
  data_size="$(wc -c < "$tmp/apidata.tar.gz")"
  printf '{"format":1,"time":"%s-%s-%sT%s:%s:%sZ","app_image":"%s","postgres_version":"%s","schema_version":%s,"database_bytes":%s,"data_bytes":%s}\n' \
    "${started:0:4}" "${started:4:2}" "${started:6:2}" "${started:9:2}" "${started:11:2}" "${started:13:2}" \
    "$tag" "$pg" "$schema" "$dump_size" "$data_size" > "$tmp/manifest.json"
  (cd "$tmp" && sha256sum database.dump apidata.tar.gz manifest.json > SHA256SUMS)
  mv -- "$tmp" "$root/$name"
  tmp=''
  published="$name"
  phase=prune
  prune
  phase=''
  log "готово: $name"
}

# Хранит объединение: последнюю копию каждого из keep_daily последних дней
# и каждой из keep_weekly последних ISO-недель. Остальные готовые копии
# удаляет; .partial-* и чужие каталоги не трогает.
prune() {
  local name date_key week_key days_kept=0 weeks_kept=0
  local -A days=() weeks=() keep=()
  validate_retention
  while IFS= read -r name; do
    valid_name "$name" || continue
    [[ -f "$root/$name/manifest.json" && ! -L "$root/$name" ]] || continue
    date_key="${name:0:4}-${name:4:2}-${name:6:2}"
    week_key="$(date -u -d "$date_key 00:00:00" +%G-%V)"
    if [[ -z "${days[$date_key]:-}" ]]; then
      days[$date_key]=1
      if ((days_kept < keep_daily)); then
        keep[$name]=1
        days_kept=$((days_kept + 1))
      fi
    fi
    if [[ -z "${weeks[$week_key]:-}" ]]; then
      weeks[$week_key]=1
      if ((weeks_kept < keep_weekly)); then
        keep[$name]=1
        weeks_kept=$((weeks_kept + 1))
      fi
    fi
  done < <(find "$root" -mindepth 1 -maxdepth 1 -type d -exec basename '{}' \; | sort -r)
  while IFS= read -r name; do
    valid_name "$name" || continue
    [[ -f "$root/$name/manifest.json" && ! -L "$root/$name" ]] || continue
    if [[ -z "${keep[$name]:-}" ]]; then
      log "удаляю старую копию $name"
      rm -rf -- "${root:?}/$name"
    fi
  done < <(find "$root" -mindepth 1 -maxdepth 1 -type d -exec basename '{}' \;)
}

# Распаковка идёт в том API, поэтому в архиве допустимы только avatars/,
# icons/ и trash/, без «..» и без ссылок. Busybox tar показывает жёсткую
# ссылку как «-… имя -> цель», GNU tar как «h… link to цель».
archive_safe() {
  local archive="$1" entries listing entry
  entries="$(tar -tzf "$archive")" || return 1
  while IFS= read -r entry; do
    case "$entry" in
      avatars/* | icons/* | trash/*) ;;
      *) return 1 ;;
    esac
    case "/$entry/" in
      *'/../'* | *'/./'*) return 1 ;;
    esac
  done <<<"$entries"
  listing="$(tar -tvzf "$archive")" || return 1
  ! grep -qE '^[lh]| -> | link to ' <<<"$listing"
}

# Проверяет копию до того, как что-то остановлено или изменено: что её
# можно прочесть, что все три файла на месте и их суммы сходятся, и что
# архив безопасно распаковывать. Любая ошибка называет, что делать.
verify() {
  local name="$1" folder file expected actual
  valid_name "$name" || die "неверное имя копии «$name»: ожидается каталог вида 20261027T043000Z-AbCd1234"
  folder="$root/$name"
  [[ -d "$folder" && ! -L "$folder" ]] || die "нет копии $host_root/$name"
  # Копия, перенесённая с другой машины, обычно принадлежит пользователю
  # хоста, и задание под UID 65532 её не прочтёт.
  for file in "$folder" "$folder"/*; do
    if [[ ! -r "$file" ]] || [[ -d "$file" && ! -x "$file" ]]; then
      die "задание (UID $(id -u)) не может прочитать $host_root/$name. Выполните на хосте: sudo chown -R $backup_uid:$backup_uid '$host_root/$name'"
    fi
  done
  [[ -f "$folder/SHA256SUMS" && ! -L "$folder/SHA256SUMS" ]] || die "копия $name неполная: нет SHA256SUMS. Возьмите другую копию"
  [[ "$(wc -l < "$folder/SHA256SUMS")" -eq 3 ]] || die "в копии $name испорчен SHA256SUMS. Возьмите другую копию"
  for file in database.dump apidata.tar.gz manifest.json; do
    [[ -f "$folder/$file" && ! -L "$folder/$file" ]] || die "в копии $name нет $file. Возьмите другую копию"
    expected="$(awk -v name="$file" '$2 == name { print $1 }' "$folder/SHA256SUMS")"
    [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || die "в SHA256SUMS копии $name нет суммы $file"
    actual="$(sha256sum "$folder/$file")"
    actual="${actual%% *}"
    [[ "$expected" == "$actual" ]] || die "копия $name испорчена: не совпадает сумма $file. Возьмите другую копию"
  done
  archive_safe "$folder/apidata.tar.gz" || die "архив изображений копии $name содержит пути вне avatars/, icons/, trash/ или ссылки; восстанавливать его нельзя"
  log "копия $name цела"
}

# Печатает поле $2 из manifest.json копии $1 (manifest пишет once, плоский JSON).
manifest_field() {
  sed -nE "s/.*\"$2\":\"?([^\",}]*)\"?.*/\1/p" "$root/$1/manifest.json"
}

# Копию с более новой схемой, чем знает образ API, восстанавливать нельзя:
# старый API запустился бы на незнакомой схеме. Более старую можно: API
# при запуске доведёт её миграциями.
check() {
  local name="$1" image_schema="${ZAZZZOR_SCHEMA_VERSION:-}" backup_schema app_image
  verify "$name"
  [[ "$image_schema" =~ ^[0-9]+$ ]] || die "не задана схема образа API (ZAZZZOR_SCHEMA_VERSION); запускайте восстановление через deploy/scripts/backup.sh"
  backup_schema="$(manifest_field "$name" schema_version)"
  app_image="$(manifest_field "$name" app_image)"
  [[ "$backup_schema" =~ ^[0-9]+$ ]] || die "в manifest.json копии $name нет schema_version"
  log "копия $name: схема $backup_schema, снята с образом $app_image; схема текущего образа API: $image_schema"
  if ((backup_schema > image_schema)); then
    die "копия $name новее образа API: в ней схема $backup_schema, а образ знает только до $image_schema. Восстанавливайте с образом, с которым она снята: $app_image"
  fi
}

# База из копии одной транзакцией в одной сессии psql: схема public
# создаётся заново (pg_restore --clean удалил бы только то, что есть в
# копии, и таблицы более новой схемы остались бы), затем SQL из pg_restore.
# COMMIT дописывается, только если pg_restore дошёл до конца; иначе psql
# закрывается посреди транзакции, и Postgres откатывает её целиком.
restore_database() {
  local dump="$root/$1/database.dump"
  {
    echo 'BEGIN;'
    echo 'DROP SCHEMA public CASCADE;'
    echo 'CREATE SCHEMA public;'
    pg_restore --no-owner -f - "$dump" && echo 'COMMIT;'
  } | PGOPTIONS='-c client_min_messages=warning' psql -X -q -o /dev/null -v ON_ERROR_STOP=1
}

# Восстановление; API в это время остановлен. Пока ничего не изменено,
# любая ошибка даёт код 3, и backup.sh снова запускает API: копия не прошла
# проверку, блокировка занята (идёт плановая копия), архив не распаковался,
# база не восстановилась (транзакция откатана). Картинки распаковываются
# рядом и подменяются переименованием уже после базы. Трогаются только
# avatars, icons и trash: другое в томе (например, lost+found) остаётся.
restore() {
  local name="$1" fresh old kind
  (check "$name") || exit 3
  (require_writable_root) || exit 3
  take_lock 3
  if [[ "$data" == / || ! -d "$data" || ! -w "$data" ]]; then
    echo "Ошибка: в каталог изображений $data нельзя писать" >&2
    exit 3
  fi
  # Блокировка у нас, значит .restore-* остались от прерванного запуска.
  find "$data" -mindepth 1 -maxdepth 1 -name '.restore-*' -exec rm -rf -- '{}' +
  fresh="$(mktemp -d "$data/.restore-new-XXXXXXXX")"
  if ! tar -xzf "$root/$name/apidata.tar.gz" -C "$fresh"; then
    rm -rf -- "$fresh"
    echo "Ошибка: архив изображений копии $name не распаковался; ничего не изменено" >&2
    exit 3
  fi
  log "пересоздаю схему public и восстанавливаю базу из $name одной транзакцией"
  if ! restore_database "$name"; then
    rm -rf -- "$fresh"
    echo "Ошибка: база не восстановлена из $name, транзакция откатана; ничего не изменено" >&2
    exit 3
  fi
  log 'подменяю изображения'
  old="$(mktemp -d "$data/.restore-old-XXXXXXXX")"
  for kind in avatars icons trash; do
    if [[ -e "$data/$kind" ]]; then
      mv -- "$data/$kind" "$old/"
    fi
    if [[ -e "$fresh/$kind" ]]; then
      mv -- "$fresh/$kind" "$data/"
    else
      mkdir -- "$data/$kind"
    fi
  done
  rm -rf -- "$old" "$fresh"
  log "база и изображения восстановлены из $name"
}

# Останавливает расписание вместе с идущей копией: у неё своя группа
# процессов (setsid), и TERM получают и pg_dump, и tar, а ловушка EXIT
# копии успевает убрать .partial-*.
stop_schedule() {
  trap - INT TERM
  if [[ -n "$schedule_child" ]]; then
    kill -TERM -- "-$schedule_child" 2>/dev/null || kill -TERM "$schedule_child" 2>/dev/null || true
    wait "$schedule_child" || true
  fi
  exit 0
}

# Расписание. Снимает копию, когда настало время и сегодня её ещё не было,
# и сразу, если последней копии больше суток (после простоя). Неудачную
# повторяет через retry_seconds, а не ждёт следующего дня. Отсчёт для
# healthcheck пишет один раз, при самом первом запуске.
schedule() {
  local at="${ZAZZZOR_BACKUP_TIME:-04:30}" today now last next_try=0 due
  [[ "$at" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || die "ZAZZZOR_BACKUP_TIME должно быть ЧЧ:ММ, сейчас: $at"
  validate_retention
  if [[ -w "$root" && ! -e "$started_file" ]]; then
    date +%s > "$started_file"
  fi
  log "расписание: каждый день в $at (TZ ${TZ:-UTC}), хранить $keep_daily дн. и $keep_weekly нед."
  trap stop_schedule INT TERM
  while :; do
    today="$(date +%F)"
    now="$(date +%H:%M)"
    last=''
    if [[ -r "$root/.last-scheduled" ]]; then
      last="$(cat "$root/.last-scheduled")"
    fi
    due=0
    if [[ ! "$now" < "$at" && "$last" != "$today" ]] || backup_overdue; then
      due=1
    fi
    if ((due == 1 && $(date +%s) >= next_try)); then
      setsid bash "$0" once &
      schedule_child=$!
      if wait "$schedule_child"; then
        printf '%s\n' "$today" > "$root/.last-scheduled"
      else
        echo "backup: плановая копия не снята, повтор через $((retry_seconds / 60)) мин" >&2
        next_try=$(($(date +%s) + retry_seconds))
      fi
      schedule_child=''
    fi
    sleep 20 &
    schedule_child=$!
    wait "$schedule_child"
    schedule_child=''
  done
}

# healthcheck сервиса backup: нездоров, если в каталог нельзя писать или
# последняя готовая копия старше max_age_hours (а копий нет дольше этого
# с запуска расписания). Пока копия снимается, здоров.
health() {
  local last now since ts age
  now="$(date +%s)"
  if [[ -f "$lock" ]]; then
    exec 8<"$lock"
    if ! flock -n 8; then
      echo 'копия снимается сейчас'
      return 0
    fi
  fi
  if [[ ! -w "$root" ]]; then
    echo "нет права записи в каталог копий $host_root (смотрите логи сервиса backup)"
    return 1
  fi
  last="$(newest_backup)"
  if [[ -z "$last" ]]; then
    since="$now"
    if [[ -r "$started_file" ]]; then
      since="$(cat "$started_file")"
    fi
    age=$(((now - since) / 3600))
    if ((now - since > max_age_hours * 3600)); then
      echo "ни одной копии за $age ч работы расписания (смотрите логи сервиса backup)"
      return 1
    fi
    echo "копий пока нет; расписание работает $age ч"
    return 0
  fi
  ts="$(date -u -d "${last:0:4}-${last:4:2}-${last:6:2} ${last:9:2}:${last:11:2}:${last:13:2}" +%s)"
  age=$(((now - ts) / 3600))
  if ((now - ts > max_age_hours * 3600)); then
    echo "последняя копия $last снята $age ч назад, это больше $max_age_hours ч (смотрите логи сервиса backup)"
    return 1
  fi
  echo "последняя копия $last, $age ч назад"
}

case "${1:-}" in
  once)
    # Повторный запуск под timeout: зависшая копия не держит блокировку вечно.
    if [[ "${ZAZZZOR_BACKUP_BOUNDED:-0}" != 1 ]]; then
      exec timeout -k 30 "$once_timeout_seconds" env ZAZZZOR_BACKUP_BOUNDED=1 bash "$0" once
    fi
    once
    ;;
  schedule)
    schedule
    ;;
  health)
    health
    ;;
  prune)
    prune
    ;;
  verify | check | restore)
    [[ -n "${2:-}" ]] || die "укажите имя копии: backup-job.sh $1 <имя>"
    "$1" "$2"
    ;;
  *)
    sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
