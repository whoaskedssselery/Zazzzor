# Zazzzor: запуск на сервере

Инструкция от чистого Linux-сервера до работающего стека. Всё работает в
Docker. Внутри стека:

| Сервис | Что делает | Наружу |
|---|---|---|
| `caddy` | HTTPS (сертификат Let's Encrypt через DuckDNS), отдаёт веб-клиент, API и страницу проверки связи | `ZAZZZOR_HTTPS_PORT` (443) |
| `api` | сервер Zazzzor: REST `/api/v1` и WebSocket `/api/v1/gateway` | только `127.0.0.1:${ZAZZZOR_API_PORT}` |
| `postgres` | база данных | никуда, только внутренняя сеть с `api` |
| `livekit` | голос и демки | 7881/tcp, 7882/udp; сигнализация 7880 только на `127.0.0.1` |
| `duckdns` | обновляет IP в DuckDNS каждые 5 минут | нет |

## Что сейчас работает

- **Работает:** API (регистрация, серверы, каналы, роли, инвайты, сообщения,
  голосовые токены и модерация), realtime-шлюз WebSocket и веб-клиент на
  `https://<домен>/`: вход и регистрация, серверы, каналы и категории, чат с
  ответами и правками, роли и права, приглашения, настройки профиля и панель
  администратора. Клиент работает в актуальных Chrome, Edge и Firefox. API
  проверяется скриптом `smoke-api.sh` (раздел 9), связь API с LiveKit —
  раздел 14.
- **Голос и демка** (этап 1.3): голосовые каналы, кто говорит, микрофон и звук,
  push-to-talk (пока окно активно), выбор устройств, голосовая модерация,
  демонстрация экрана 1080p60 и просмотр чужих демок по кнопке «Смотреть».
  Звук демки: вкладка Chrome и Edge — да; весь экран Chrome и Edge на Windows —
  звук системы без голосового чата; окно и Firefox — без звука (подсказка в
  интерфейсе). Шумоподавление «Улучшенное» (по умолчанию) — нейросеть
  DeepFilterNet3 прямо в браузере: модель около 23 МБ раздаёт этот же сервер
  при первом звонке, дальше она в кэше браузера; если компьютер не успевает,
  клиент сам переходит на запасное (RNNoise) и пишет об этом в «Голос и
  видео». Связь можно отдельно проверить на
  [странице проверки связи](#проверка-связи-этап-11).
- **Пока нет:** приложения для Windows (этап 1.4: глобальные клавиши, звук
  окна).

Друзья регистрируются сами на `https://<домен>/register`, пока регистрация
открыта (раздел 10), а потом заходят на ваш сервер по ссылке-приглашению
(меню сервера → «Пригласить людей»).

Если на сервере используется Docker Swarm (`docker stack`), смотрите
[раздел 15](#15-вариант-docker-swarm-docker-stack): разделы 1–7 общие, запуск и
обслуживание — там.

Все команды ниже выполняются **из корня репозитория** (каталог `zazzzor`).
Если `docker` у вас требует `sudo`, добавляйте `sudo` перед командами
`docker ...` и `bash deploy/scripts/...`.

## 1. Требования

- Linux (проверялось на Ubuntu/Debian), x86_64.
- Docker Engine **25 или новее** и плагин Docker Compose **v2.24 или новее**
  (стек проверен на Engine 29.6 и Compose 5.3). Проверка:
  `docker version` и `docker compose version`.
- Свободные порты: 443/tcp (или другой HTTPS-порт, см. раздел 14),
  7881/tcp, 7882/udp, а на `127.0.0.1` — 7880/tcp и 8080/tcp (порт API).
- Память: лимиты контейнеров в сумме около 3,5 ГБ (LiveKit 1 ГБ, Postgres 1 ГБ,
  API 1 ГБ, Caddy 256 МБ, DuckDNS 64 МБ). 12 ГБ хватает с запасом.
- Доступ к настройкам роутера (проброс портов), белый (публичный) IP.
- `git`, `openssl` (генерация ключей) и `ss` (пакет `iproute2`).

## 2. Установка Docker (Ubuntu/Debian)

Если Docker уже стоит (на сервере работают другие контейнеры), проверьте
только версии из раздела 1 и переходите к разделу 3.

Официальная инструкция: <https://docs.docker.com/engine/install/ubuntu/>
(для Debian — <https://docs.docker.com/engine/install/debian/>). Кратко,
через официальный скрипт установки:

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
# Чтобы запускать docker без sudo (после этого перелогиньтесь):
sudo usermod -aG docker "$USER"
docker compose version
```

Если образы с Docker Hub не скачиваются (из России такое бывает), пропишите
зеркало реестра в `/etc/docker/daemon.json`
(`"registry-mirrors": ["https://<зеркало>"]`) и перезапустите Docker:
`sudo systemctl restart docker`.

## 3. Код

```bash
git clone https://github.com/whoaskedssselery/Zazzzor.git zazzzor
cd zazzzor
```

## 4. Поддомен и токен DuckDNS

1. Зайдите на <https://www.duckdns.org> (вход через GitHub, Google и т.п.).
2. Создайте поддомен, например `zazzzor` — получится `zazzzor.duckdns.org`.
3. Скопируйте `token` в верхней части страницы.

IP вписывать вручную не нужно: контейнер `duckdns` сам обновляет его каждые
5 минут.

## 5. Файл настроек `deploy/.env`

```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
nano deploy/.env
```

Заполните:

| Переменная | Что писать |
|---|---|
| `ZAZZZOR_DOMAIN` | `zazzzor.duckdns.org` (полное имя) |
| `DUCKDNS_SUBDOMAIN` | `zazzzor` (без `.duckdns.org`) |
| `DUCKDNS_TOKEN` | токен с duckdns.org |
| `ACME_EMAIL` | ваша почта для Let's Encrypt |
| `LIVEKIT_API_KEY` | результат `openssl rand -hex 16` |
| `LIVEKIT_API_SECRET` | результат `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | результат `openssl rand -hex 24` (только hex, без других символов) |
| `ZAZZZOR_ADMIN_USERNAMES` | ваш будущий логин, например `ivan` (раздел 10) |
| `ZAZZZOR_BACKUP_DIR` | каталог резервных копий на хосте; `/srv/zazzzor-backups` можно оставить, но каталог создайте (раздел 12, шаг 1) |

Каждую команду `openssl` запускайте отдельно и вставляйте её вывод в
соответствующую строку. Порты, `ZAZZZOR_API_PORT` и версии образов можно
оставить как есть.

`POSTGRES_PASSWORD` задаётся один раз: при первом запуске база создаётся с
этим паролем. Если потом поменять его в `.env`, пароль в самой базе
не изменится и API перестанет подключаться (как это исправить — раздел 13).

Пока обязательные поля пустые, `docker compose` не запустится и напишет,
какую переменную заполнить, например:

```
required variable POSTGRES_PASSWORD is missing a value: заполните POSTGRES_PASSWORD в deploy/.env - результат openssl rand -hex 24
```

Файл `deploy/.env` в git не попадает — не публикуйте его, в нём секреты.

Рядом лежит `deploy/.env.ci` с заведомо фальшивыми значениями (домен
`.invalid`, нулевой токен). Он нужен только для проверки конфигурации без
секретов, на сервере его не используйте:

```bash
docker compose --env-file deploy/.env.ci -f deploy/docker-compose.yml config --quiet
```

## 6. Проверка, что порты свободны

На сервере уже работают другие сервисы, поэтому сначала убедитесь, что нужные
порты не заняты:

```bash
sudo bash deploy/scripts/check-ports.sh
```

- `Все порты свободны` — можно продолжать.
- `ЗАНЯТ: 443/tcp (процесс ...)` — порт занят. Для 443 см.
  [«Если 443 уже занят»](#если-443-уже-занят). Остальные порты
  (`LIVEKIT_TCP_PORT`, `LIVEKIT_UDP_PORT`, `ZAZZZOR_API_PORT`) можно поменять в
  `deploy/.env` на любые свободные. Порт 7880 (внутренний, только localhost)
  менять нельзя.

`sudo` нужен только чтобы увидеть, какой процесс занял порт. Проверку
запускайте до первого запуска: когда Zazzzor работает, его порты, естественно,
заняты им самим.

## 7. Проброс портов на роутере

Пробросьте на внутренний IP сервера (значения по умолчанию):

| Внешний порт | Протокол | Порт на сервере | Зачем |
|---|---|---|---|
| 443 | TCP | `ZAZZZOR_HTTPS_PORT` (443) | HTTPS: API, страница, сигнализация LiveKit |
| 7881 | TCP | `LIVEKIT_TCP_PORT` (7881) | медиа через TCP, если UDP заблокирован |
| 7882 | UDP | `LIVEKIT_UDP_PORT` (7882) | основное медиа (голос, видео, демки) |

Порт 80 не нужен: сертификат выпускается через DNS DuckDNS. Порты API (8080),
LiveKit 7880 и Postgres наружу **не** пробрасываются.

Если на сервере включён файрвол (`ufw`, `firewalld`), откройте там те же порты,
например:

```bash
sudo ufw allow 443/tcp
sudo ufw allow 7881/tcp
sudo ufw allow 7882/udp
```

Рекомендуется увеличить буфер приёма UDP (LiveKit иначе предупреждает в логах):

```bash
echo 'net.core.rmem_max=5000000' | sudo tee /etc/sysctl.d/90-zazzzor.conf
sudo sysctl --system
```

## 8. Запуск

Сначала создайте каталог резервных копий (раздел 12, шаг 1), затем:

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

Первая сборка образов Caddy и API занимает несколько минут. При старте API сам
применяет миграции базы.

## 9. Проверка

```bash
docker compose -f deploy/docker-compose.yml ps
```

У `postgres`, `api` и `backup` в колонке `STATUS` должно быть `(healthy)` (API
становится healthy в течение минуты после старта), у остальных — `Up`.
У `api` в колонке `PORTS` — только `127.0.0.1:8080->8080/tcp`.

Сертификат: выпуск через DNS занимает 1–3 минуты, в логах Caddy должна
появиться строка `certificate obtained successfully`:

```bash
docker compose -f deploy/docker-compose.yml logs -f caddy
```

Сквозная проверка API:

```bash
bash deploy/scripts/smoke-api.sh
```

Скрипт внутри контейнера `api` регистрирует проверочного пользователя
`smoke_<случайные символы>`, создаёт сервер, отправляет и читает сообщение,
открывает WebSocket `/api/v1/gateway` и ждёт события HELLO и READY (до 10 с),
затем удаляет созданный сервер. При успехе последняя строка — `OK`. При
ошибке он пишет, на каком шаге и почему всё сломалось, например:

```
ОШИБКА: шаг «регистрация»: ответ 403 registration_closed вместо 201 (регистрация закрыта в настройках сервера; откройте её на время проверки)
```

Особенности:

- Регистраций не больше 3 в час с одного адреса, поэтому больше трёх
  запусков подряд упрутся в ответ 429 — подождите час.
- Пока регистрация закрыта (раздел 10), проверка не пройдёт на первом шаге.
- Проверочные пользователи остаются в базе (удаления аккаунтов нет), они
  ни на что не влияют.

Проверка через Caddy и интернет (HTTPS, сертификат, маршрут `/api/*`):

```bash
bash deploy/scripts/smoke-api.sh https://zazzzor.duckdns.org
```

Если она падает с ошибкой подключения, а проверка без адреса проходит, дело
может быть в роутере: многие не умеют открывать собственный внешний адрес
изнутри сети. Тогда откройте `https://<домен>/api/v1/healthz` с телефона через
мобильный интернет — должен прийти ответ `{"status":"ok"}`.

Убедитесь, что сервисы слушают нужные порты:

```bash
sudo ss -lntup | grep -E ':(443|7880|7881|7882|8080)\b'
```

Ожидается: 7880/tcp и 8080/tcp только на `127.0.0.1`, а 443/tcp, 7881/tcp и
7882/udp — на всех адресах (`*`, `0.0.0.0` или `[::]`). Если HTTPS-порт или
порт API у вас другие, подставьте свои. Если 7880 или 8080 видны на `*` или
`0.0.0.0`, что-то не так с конфигурацией — не продолжайте.

Открываться должны `https://<домен>/` (веб-клиент, экран входа),
`https://<домен>/api/v1/healthz` и
`https://<домен>/connectivity-test/`. Любой другой путь, кроме `/api/…`,
`/rtc…` и `/connectivity-test…`, тоже отдаёт веб-клиент с кодом 200:
адреса вида `/channels/…` разбирает сам клиент.

## 10. Первый администратор и регистрация

1. Укажите логин в `ZAZZZOR_ADMIN_USERNAMES` в `deploy/.env`. После изменения
   обновите API тем же способом, которым запускали стек.
2. Создайте аккаунт из терминала. Введите пароль скрыто и передайте его
   через stdin:

   ```bash
   read -rsp 'Пароль: ' PASS; echo
   printf '%s\n' "$PASS" |
     docker compose --env-file deploy/.env -f deploy/docker-compose.yml exec -T api /zazzzor create-admin ivan
   unset PASS
   ```

   В Swarm используйте `docker exec -i <контейнер-api> /zazzzor create-admin ivan`
   в той же команде с передачей пароля через stdin. Если аккаунт с этим
   логином уже есть, войдите с его паролем.
3. Войдите через браузер. На пустой базе регистрация закрыта. Если нужны
   новые аккаунты, откройте её в «Настройки пользователя» →
   «Администрирование» → «Сервер Zazzzor». Настройка существующей базы
   сохраняется при обновлении.

Во вкладке «Пользователи» администратор может найти аккаунт по логину,
сбросить пароль, заблокировать или разблокировать аккаунт.

## 11. Обновление

```bash
git pull
# Сравните настройки: новые переменные из .env.example нужно перенести в .env.
diff deploy/.env.example deploy/.env
docker compose -f deploy/docker-compose.yml up -d --build
```

При обновлении на версию с голосовыми каналами `up -d` пересоздаёт сеть
`zazzzor_api` с постоянной подсетью `172.31.250.0/24` (раздел 14, «Голосовые
каналы»): API и LiveKit перезапускаются, это несколько секунд. Новых
переменных нет: API берёт уже заполненные `LIVEKIT_API_KEY` и
`LIVEKIT_API_SECRET`.

При обновлении на версию с резервными копиями добавьте в `deploy/.env`
`ZAZZZOR_BACKUP_DIR` и до `up` создайте этот каталог (раздел 12, шаг 1).
Без переменной не работает ни одна команда `docker compose`.

Миграции базы применяются автоматически при старте API. Чтобы
`zazzzor --version` показывал версию, а не `dev`, собирайте так:

```bash
ZAZZZOR_VERSION=$(git describe --tags --always) docker compose -f deploy/docker-compose.yml up -d --build
```

Старые слои образов после обновлений можно удалить командой
`docker image prune` (удаляет только образы без тегов, у всех проектов на
сервере).

## 12. Данные и резервные копии

Данные лежат в томах Docker:

| Том | Что в нём |
|---|---|
| `zazzzor_pgdata` | база Postgres: пользователи, серверы, сообщения |
| `zazzzor_apidata` | загруженные картинки (аватары, иконки серверов) |
| `zazzzor_caddy_data` | сертификаты HTTPS |

Они переживают `down`, перезапуски и обновления.
**`docker compose ... down -v` удаляет тома, то есть все данные безвозвратно.**
Не используйте `-v` без резервной копии.

### Автоматические резервные копии

Сервис `backup` каждый день снимает копию базы и картинок. Он работает и в
compose, и в стеке Swarm.

1. До первого запуска создайте каталог копий на хосте. Путь задаёт
   `ZAZZZOR_BACKUP_DIR` в `deploy/.env` (в `.env.example` это
   `/srv/zazzzor-backups`). Сервис работает под UID 65532, как API, поэтому
   каталог принадлежит ему:

   ```bash
   sudo install -d -o 65532 -g 65532 -m 0700 /srv/zazzzor-backups
   ```

   В копиях вся переписка, сессии и хеши паролей. Не открывайте каталог
   другим пользователям.
2. Копия снимается в 04:30 по `TZ=Europe/Warsaw`. Хранится последняя копия
   за каждый из 7 последних дней и за каждую из 4 последних недель. Это
   меняют `TZ`, `ZAZZZOR_BACKUP_TIME`, `ZAZZZOR_BACKUP_KEEP_DAILY` и
   `ZAZZZOR_BACKUP_KEEP_WEEKLY` в `deploy/.env` (см. `.env.example`); потом
   `docker compose -f deploy/docker-compose.yml up -d` или `stack.sh deploy`.
   Если сервер был выключен в 04:30, копия снимется сразу после запуска.
   Неудачная копия повторяется через 30 минут.
3. Каждая копия — каталог вида `20261027T023000Z-AbCd1234` (время начала, UTC)
   с файлами `database.dump`, `apidata.tar.gz` (картинки вместе с корзиной),
   `manifest.json` и `SHA256SUMS`. Недописанная копия лежит в `.partial-*`
   и в восстановлении не участвует.
4. Следите за здоровьем сервиса. Он становится `unhealthy`, если последней
   копии больше 26 часов или в каталог нельзя писать:

   ```bash
   docker compose -f deploy/docker-compose.yml ps backup
   docker compose -f deploy/docker-compose.yml logs --tail 100 backup
   # Swarm: неисправная задача перезапускается и видна в списке задач.
   bash deploy/scripts/stack.sh ps
   docker service ps zazzzor_backup
   bash deploy/scripts/stack.sh logs backup
   ```

   Ошибка в логе называет причину и команду, которая её исправит.
5. Снять копию сейчас:

   ```bash
   bash deploy/scripts/backup.sh compose backup
   # Swarm:
   bash deploy/scripts/stack.sh backup
   ```

Удалённые и заменённые картинки 8 суток лежат в корзине (`trash` в томе
картинок) и наружу не отдаются. Поэтому копия, снятая до удаления, остаётся
полной. Срок задаёт `ZAZZZOR_FILE_RETENTION`, он должен быть больше суток.
Корзину раз в час чистит сам API. Не удаляйте её руками.

На пользователя выделено 2 ГиБ для вложений, включая ещё не отправленные
файлы. При удалении сообщения его вложения сразу перестают считаться в
квоте. Загрузка доступна, пока на томе файлов свободно не меньше 2 ГиБ.
Чтобы изменить значения, раскомментируйте `ZAZZZOR_ATTACHMENT_QUOTA_BYTES`
и `ZAZZZOR_MIN_FREE_BYTES` в `deploy/.env`. Обе настройки задаются в байтах.
Ноль отключает соответствующее ограничение.

Вложения хранятся в исходном виде в том же томе. До 25 МиБ на файл,
до 10 файлов на сообщение. Лимит загрузок на пользователя составляет
30 в минуту (пакет до 10, одновременно до двух). Неотправленные файлы
недоступны через час и удаляются очередным проходом фоновой задачи.
Удалённые вложения лежат в общей корзине столько же, сколько картинки.
Учитывайте исходный размер вложений, корзину и копии при расчёте места
на диске.

### Восстановление

Цели: терять не больше суток (RPO ≤ 24 ч) и подниматься примерно за час
(RTO около 1 ч). На этом железе они ещё не проверены.

**Восстановление заменяет все данные сервера копией.** Всё, что появилось
после неё, пропадёт.

1. Если сервер жив, сначала снимите свежую копию (шаг 5 выше).
2. Выберите копию: `sudo ls /srv/zazzzor-backups`.
3. Восстановите её:

   ```bash
   bash deploy/scripts/backup.sh compose restore 20261027T023000Z-AbCd1234 --yes
   # Swarm:
   bash deploy/scripts/stack.sh restore 20261027T023000Z-AbCd1234 --yes
   ```

   Копию можно назвать именем или путём. Скрипт проверяет суммы и схему
   копии, останавливает API, восстанавливает базу и картинки, сверяет
   картинки с базой (`files reconcile --strict`: нехватка картинки из базы —
   ошибка, а не предупреждение). После успешной сверки скрипт
   удаляет прежние файлы и запускает API.
4. Проверьте вход, сообщения, аватары, иконки, вложения и эмодзи.

Если скрипт остановился с ошибкой:

- до остановки API данные не тронуты. Например, «копия новее образа API»:
  восстанавливайте с образом, который назван в сообщении, или обновите код;
- «Данные не изменены»: восстановление остановилось до изменений.
  API запускается снова. Устраните причину и повторите команду;
- если занята блокировка, дождитесь окончания копии или другого
  восстановления. Скрипт оставляет API остановленным;
- при сбое базы, подмены или сверки API остаётся выключенным. Сохраните
  каталог `.restore-old-*` в томе данных. Устраните причину и продолжите
  с той же копией:

  ```bash
  bash deploy/scripts/backup.sh compose restore 20261027T023000Z-AbCd1234 --yes --resume
  # Swarm:
  bash deploy/scripts/stack.sh restore 20261027T023000Z-AbCd1234 --yes --resume
  ```

  Если прерванный запуск был на старой версии и в каталоге нет журнала,
  сохраните прежние файлы отдельно перед ручным восстановлением.

Копия со старой схемой базы восстанавливается и новым образом: API при
запуске сам применит новые миграции.

Сертификаты Caddy и состояние LiveKit в копию не входят. Сертификаты
выпустятся заново, идущие звонки просто прервутся. `deploy/.env` с секретами
храните отдельно. Шифрования копий пока нет.

### Восстановление на чистой машине

1. Установите Docker и скачайте код (разделы 2 и 3). Верните `deploy/.env`
   из своей отдельной копии.
2. Создайте каталог копий (шаг 1 раздела «Автоматические резервные копии»).
3. Перенесите копию в этот каталог и отдайте её UID 65532, иначе сервис её
   не прочтёт:

   ```bash
   rsync -a backup-user@backup-host:/srv/backups/zazzzor/20261027T023000Z-AbCd1234 /tmp/
   sudo cp -a /tmp/20261027T023000Z-AbCd1234 /srv/zazzzor-backups/
   sudo chown -R 65532:65532 /srv/zazzzor-backups/20261027T023000Z-AbCd1234
   ```

4. Соберите образы и запустите только базу:

   ```bash
   docker compose -f deploy/docker-compose.yml build
   docker compose -f deploy/docker-compose.yml up -d --wait postgres
   ```

5. Восстановите копию и запустите остальное:

   ```bash
   bash deploy/scripts/backup.sh compose restore 20261027T023000Z-AbCd1234 --yes
   docker compose -f deploy/docker-compose.yml up -d
   ```

   В Swarm вместо шагов 4 и 5: `stack.sh deploy` (раздел 15), затем
   `stack.sh restore <копия> --yes`.

### Копия вне сервера

Копия на том же диске — не копия: сломается диск, и пропадут и данные, и
копии. Переносите готовые копии на другую машину или на отключаемый
USB-диск. Скрипты этого не делают. Например:

```bash
# На другой сервер по SSH:
sudo rsync -a --exclude '.partial-*' --exclude '.lock' --exclude '.last-scheduled' \
  /srv/zazzzor-backups/ backup-user@backup-host:/srv/backups/zazzzor/
# В облако через rclone (remote настраивается командой rclone config):
sudo rclone copy /srv/zazzzor-backups remote:zazzzor-backups \
  --exclude '.partial-*/**' --exclude '.lock' --exclude '.last-scheduled'
```

На новом месте проверьте суммы: `cd <копия> && sha256sum -c SHA256SUMS`.

### Проверка восстановления

Раз в несколько месяцев и после крупных обновлений проверьте, что копия
действительно восстанавливается. Нужны только Docker и bash:

```bash
bash deploy/scripts/restore-test.sh
```

Скрипт поднимает отдельный временный проект compose. Через API он создаёт
пользователя с аватаром, сервер с иконкой и эмодзи и сообщение с вложением.
Потом снимает копию и восстанавливает её дважды, поверх базы с лишней таблицей
и на пустые тома. Проверяет вход, сообщение и SHA-256 всех четырёх файлов.
В конце скрипт удаляет временный проект.
Рабочий стек не затрагивается. В конце печатается `restore test: OK`. Тот же
сценарий идёт в CI.

### Ручная копия, если скрипты не работают

Команды запускаются из корня репозитория. **API на время копии
останавливается**, иначе база и картинки могут разойтись:

```bash
PG_IMAGE=postgres:$(grep -E '^POSTGRES_VERSION=' deploy/.env | cut -d= -f2)
mkdir -p ~/zazzzor-manual
docker compose -f deploy/docker-compose.yml stop api
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_dump -U zazzzor -d zazzzor -Fc > ~/zazzzor-manual/database.dump
docker run --rm -v zazzzor_apidata:/data:ro "$PG_IMAGE" \
  tar czf - -C /data avatars icons attachments trash > ~/zazzzor-manual/apidata.tar.gz
docker compose -f deploy/docker-compose.yml start api
(cd ~/zazzzor-manual && sha256sum database.dump apidata.tar.gz > SHA256SUMS)
```

Восстановление из неё. **Оно заменяет все данные сервера:**

```bash
PG_IMAGE=postgres:$(grep -E '^POSTGRES_VERSION=' deploy/.env | cut -d= -f2)
(cd ~/zazzzor-manual && sha256sum -c SHA256SUMS)
docker compose -f deploy/docker-compose.yml stop api
docker compose -f deploy/docker-compose.yml exec -T postgres psql -U zazzzor -d zazzzor \
  -v ON_ERROR_STOP=1 -c 'DROP SCHEMA public CASCADE' -c 'CREATE SCHEMA public'
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_restore -U zazzzor -d zazzzor --exit-on-error --no-owner < ~/zazzzor-manual/database.dump
docker run --rm -i --user 65532:65532 -v zazzzor_apidata:/data "$PG_IMAGE" \
  sh -c 'rm -rf /data/avatars /data/icons /data/attachments /data/trash && tar xzf - -C /data' \
  < ~/zazzzor-manual/apidata.tar.gz
docker compose -f deploy/docker-compose.yml run --rm --no-deps api files reconcile
docker compose -f deploy/docker-compose.yml start api
```

## 13. Логи, перезапуск, если что-то не работает

```bash
# Логи одного сервиса (api, postgres, caddy, livekit, duckdns, backup)
docker compose -f deploy/docker-compose.yml logs -f api
# Перезапустить сервис
docker compose -f deploy/docker-compose.yml restart api
# Применить изменения .env или кода
docker compose -f deploy/docker-compose.yml up -d --build
# Остановить всё (данные остаются)
docker compose -f deploy/docker-compose.yml down
```

Логи всех контейнеров ротируются: не больше 3 файлов по 10 МБ на сервис.
API пишет одну JSON-строку на запрос; пароли, токены и cookie в логи не
попадают.

Частые проблемы:

- **`docker compose` ругается `required variable ... is missing a value`.**
  Не заполнена переменная в `deploy/.env` — её имя в сообщении. После
  обновления это обычно новая переменная из `.env.example`.
- **Порт занят** (`address already in use` / `port is already allocated`).
  Запустите `sudo bash deploy/scripts/check-ports.sh` (покажет процесс) и
  поменяйте порт в `.env`, для 443 — раздел 14.
- **Сертификат не выпускается.** Проверьте `DUCKDNS_TOKEN` и `ZAZZZOR_DOMAIN`
  в `.env` и `logs caddy`. TXT-запись DNS-01 Caddy проверяет через
  публичные DNS: строка `resolvers 1.1.1.1 8.8.8.8` в
  `deploy/caddy/Caddyfile` включена по умолчанию, потому что кэш DNS
  провайдера или роутера часто отдаёт старый ответ и проверка зависает. Если
  исходящие DNS-запросы к этим адресам у вас закрыты, удалите эту строку и
  выполните `docker compose -f deploy/docker-compose.yml up -d --build`.
- **`api` в статусе `unhealthy` или `Restarting`.** Смотрите
  `docker compose -f deploy/docker-compose.yml logs --tail 50 api`. Первая
  строка с `"level":"ERROR"` называет причину: `load config` — ошибка в
  настройках (в сообщении имя переменной), `open database` — база недоступна
  или не тот пароль (следующий пункт).
- **Поменяли `POSTGRES_PASSWORD` после первого запуска.** В логах API:
  `password authentication failed for user "zazzzor"`. Либо верните в `.env`
  старый пароль, либо выставьте в базе новый (контейнер `postgres` уже получил
  его из `.env` при `up -d`) и перезапустите API:

  ```bash
  docker compose -f deploy/docker-compose.yml up -d
  docker compose -f deploy/docker-compose.yml exec postgres \
    sh -c 'psql -U zazzzor -d zazzzor -c "ALTER USER zazzzor PASSWORD '\''$POSTGRES_PASSWORD'\''"'
  docker compose -f deploy/docker-compose.yml restart api
  ```

- **`https://<домен>/api/...` отвечает 502.** Caddy работает, а API нет:
  проверьте `ps` и `logs api`. Если меняли `ZAZZZOR_API_PORT`, выполните
  `up -d`, чтобы Caddy тоже получил новый порт.
- **Страница не открывается вообще.** Проверьте, что `<домен>` указывает на
  ваш внешний IP (`nslookup <домен>`), что 443 проброшен и Caddy получил
  сертификат (`logs caddy`). Проверяйте с телефона через мобильный интернет.

## 14. Соседство с другими сервисами на сервере

Zazzzor занимает на хосте только порты из раздела 1: наружу — HTTPS-порт,
7881/tcp и 7882/udp, на `127.0.0.1` — 7880 и `ZAZZZOR_API_PORT`. Postgres не
публикует порт вовсе (с вашим другим Postgres на 5432 он не конфликтует).
Контейнеры, тома и сети называются `zazzzor-*` / `zazzzor_*`. Команды из этой
инструкции трогают только проект `zazzzor`, но `docker system prune` и
`docker volume prune` затрагивают **все** проекты на сервере — будьте с ними
осторожны.

### Если 443 уже занят

Если 443 на сервере занят другим сервисом (например, nginx), поставьте в
`deploy/.env`:

```
ZAZZZOR_HTTPS_PORT=8443
```

и на роутере пробросьте **внешний 443 → внутренний 8443** (TCP). Чтобы адреса
(ссылки проверки связи, разрешённый `Origin` API) были без порта,
раскомментируйте там же:

```
ZAZZZOR_PUBLIC_PORT=443
```

Перезапустите: `docker compose -f deploy/docker-compose.yml up -d`.

Если внешний 443 на роутере уже отдан другому сервису (тот же nginx доступен
снаружи), пробросьте внешний 8443 → 8443 и не задавайте
`ZAZZZOR_PUBLIC_PORT`: адреса будут с `:8443`. Это работает, но в некоторых
сетях нестандартные порты режутся.

API принимает изменяющие запросы только с `Origin` вашего домена: compose
разрешает `https://<домен>` и `https://<домен>:<внешний порт>`
(`ZAZZZOR_PUBLIC_PORT`, а если он не задан — `ZAZZZOR_HTTPS_PORT`).

### Сеть и адреса клиентов

Caddy работает в сети хоста и ходит в API на `127.0.0.1:${ZAZZZOR_API_PORT}`.
Docker доставляет такие соединения в контейнер от адреса шлюза bridge-сети
`zazzzor_api`, `172.31.250.1` (подсеть сети задана в
`docker-compose.yml`, см. «Голосовые каналы» ниже). API верит заголовку
`X-Forwarded-For` (настоящий адрес клиента, его ставит Caddy) только от
адресов из `ZAZZZOR_TRUSTED_PROXIES`, по умолчанию
`172.16.0.0/12,127.0.0.1/32`, куда этот шлюз входит. От адреса клиента зависят
лимиты (например, 3 регистрации в час с адреса), поэтому, если адрес не
распознан, все друзья будут для API «одним клиентом».

Это безопасно: к порту API, кроме Caddy, могут подключиться только процессы
на самом сервере, снаружи он недоступен.

### Голосовые каналы

API выдаёт токены LiveKit и следит, кто в каком голосовом канале. Для этого
API и LiveKit связаны в обе стороны:

1. **API → LiveKit** (список комнат, отключение участника). LiveKit работает в
   сети хоста и слушает `7880` на `127.0.0.1` (для Caddy) и на
   `172.31.250.1` — шлюзе сети `zazzzor_api`. Этот адрес есть только на
   самом сервере, снаружи он не виден. API ходит на
   `http://172.31.250.1:7880`.
2. **LiveKit → API** (вебхуки: кто подключился, кто вышел, кто показывает
   экран). LiveKit шлёт их на `http://127.0.0.1:${ZAZZZOR_API_PORT}/api/v1/livekit/webhook`,
   подписывая ключом `LIVEKIT_API_KEY`. Неподписанные запросы API отклоняет.

Публичный `/api/v1/livekit/webhook` Caddy закрывает ответом `404`, в том
числе для подписанного запроса. Это нормально: LiveKit использует адрес
на `127.0.0.1`, а не публичный HTTPS-домен.

Ключ и секрет — те же `LIVEKIT_API_KEY` и `LIVEKIT_API_SECRET` из
`deploy/.env`. Если их нет, API всё равно запускается, но голосовые запросы
отвечают `503 voice_unavailable`, а в логе API при старте одна строка
`voice is off`. Если задан только один из двух, API не запускается и
называет недостающую переменную.

Проверка после запуска:

```bash
# LiveKit слушает оба адреса:
docker compose -f deploy/docker-compose.yml logs livekit | grep bindAddresses
# API достучался до LiveKit (сверка комнат при старте и каждые 5 минут):
docker compose -f deploy/docker-compose.yml logs livekit | grep ListRooms
# Вебхуки доходят (появляются, когда кто-то заходит в голосовой канал):
docker compose -f deploy/docker-compose.yml logs api | grep livekit/webhook
```

В строках вебхуков должен быть `"status":204`. `401` значит, что ключ в
настройках LiveKit и у API разный: оба берутся из одного `deploy/.env`,
поэтому выполните `up -d`, чтобы оба сервиса его перечитали.

В приватных метриках API есть
`zazzzor_voice_reconcile_last_success_timestamp_seconds`: время последней
полностью успешной сверки, в Unix-секундах. `0` означает, что сверка ещё
не завершалась успешно либо голос отключён. Ошибка одной комнаты не мешает
проверить остальные, но не обновляет эту метку. Если она отстаёт более чем
на два `ReconcileInterval` (интервал определён в серверном коде), проверьте
логи API и доступность LiveKit. `/api/v1/readyz` от LiveKit не зависит:
текстовый чат продолжает работать при сбое голоса.

В варианте Swarm с сетью хоста (раздел 15) метрики доступны на самом сервере:
если меняли `ZAZZZOR_METRICS_PORT`, подставьте выбранный порт вместо `9464`.

```bash
curl --fail http://127.0.0.1:9464/metrics | grep zazzzor_voice_reconcile
```

В Compose метрики остаются внутри Docker-сети; ради этой проверки
не публикуйте их порт наружу.

Если после перезагрузки LiveKit пишет об ошибке привязки `172.31.250.1`,
повторите создание сервисов Compose и проверьте журнал:

```bash
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml logs --tail 50 livekit
```

Если подсеть `172.31.250.0/24` на сервере уже занята другой сетью
(`docker compose up` сообщит `Pool overlaps with other one on this address
space`), замените её в `deploy/docker-compose.yml` в трёх местах: в
`networks.api` (подсеть и шлюз), в `bind_addresses` у `livekit` и в
`ZAZZZOR_LIVEKIT_URL` у `api`. Шлюз должен остаться внутри `172.16.0.0/12`
или его нужно добавить в `ZAZZZOR_TRUSTED_PROXIES`.

После перезапуска API голосовые состояния восстанавливаются из LiveKit за
секунды; отметки «микрофон выключен» и «звук выключен» клиенты присылают
заново сами.

Серверные mute/deaf хранятся в памяти API. Перезапуск, в том числе при
rolling update, снимает их и может снова разрешить микрофон участнику
активного звонка. После обновления проверьте модерацию и при необходимости
примените ограничения снова. Выход из голосового канала сохраняет эти
флаги; выход, исключение или бан участника из сервера сбрасывает их.

## 15. Вариант: Docker Swarm (`docker stack`)

Если на сервере сервисы уже живут в Docker Swarm, Zazzzor разворачивается
стеком: файл `deploy/docker-stack.yml` и скрипт `deploy/scripts/stack.sh`.
Разделы 1–7 (Docker, код, DuckDNS, `.env`, порты, роутер) те же. Вместо
`docker compose` из разделов 8–13 используются команды ниже. Compose и стек
одновременно не запускайте: они занимают одни и те же порты.

### Чем стек отличается от compose

Swarm не умеет публиковать порт только на `127.0.0.1`, а открывать API и базу
наружу нельзя. Поэтому в стеке `postgres` и `api` работают в сети хоста и сами
слушают только `127.0.0.1`:

| Сервис | Где слушает |
|---|---|
| `caddy` | `ZAZZZOR_HTTPS_PORT` (443) на всех адресах |
| `livekit` | 7881/tcp, 7882/udp на всех адресах; 7880 на `127.0.0.1` (Caddy и API) |
| `api` | `127.0.0.1:${ZAZZZOR_API_PORT}` (8080), метрики `127.0.0.1:${ZAZZZOR_METRICS_PORT}` (9464) |
| `postgres` | `127.0.0.1:${POSTGRES_PORT}` (55432, чтобы не встретиться с другим Postgres на 5432) |
| `duckdns` | никуда, только исходящие запросы |

Ещё отличия:
- Swarm не собирает образы. Их собирает `stack.sh` на этом сервере, а все
  сервисы привязаны к этому узлу. Тег образа — его содержимое, поэтому при
  обновлении перезапускаются только сервисы, чей образ действительно
  изменился (Caddy, например, не трогается, если менялся только код API).
- `depends_on` в Swarm нет: API при старте сам ждёт базу до 60 секунд (в логах
  `database not ready, retrying`), а если не дождался, Swarm перезапускает его.
  Неверный пароль базы ждать бессмысленно — тогда API падает сразу.
- API и LiveKit оба в сети хоста, поэтому друг к другу ходят через
  `127.0.0.1`: API на `127.0.0.1:7880`, вебхуки LiveKit на
  `127.0.0.1:${ZAZZZOR_API_PORT}`. Подсеть `172.31.250.0/24` стеку не нужна.
- `no-new-privileges` Swarm не поддерживает. Остальная защита API та же:
  файловая система только для чтения, `cap_drop: ALL`, пользователь не root.
- Метрики API (`127.0.0.1:9464/metrics`) и Postgres (`127.0.0.1:55432`)
  доступны процессам на самом сервере (в compose они были только внутри сети
  Docker). Снаружи и из обычных контейнеров их не видно, Postgres защищён
  паролем (scram-sha-256).
- Тома называются так же, как в compose (`zazzzor_pgdata` и т. д.). Чтобы
  перейти с compose на стек, остановите compose **без** `-v`
  (`docker compose -f deploy/docker-compose.yml down`) и разверните стек:
  данные останутся. Имя стека не меняйте (`STACK_NAME`): у другого имени
  другие тома, то есть пустая база.

### Запуск

Один раз включите Swarm, если он ещё не включён (`docker info` покажет
`Swarm: active`):

```bash
docker swarm init
```

Если у сервера несколько сетевых адресов, Docker попросит указать один:
`docker swarm init --advertise-addr <IP сервера в локальной сети>`.

Проверка портов (раздел 6) в режиме Swarm проверяет ещё `POSTGRES_PORT` и
`ZAZZZOR_METRICS_PORT`. До `docker swarm init` запускайте её с флагом
`--stack`. Затем сборка и развёртывание (первая сборка — несколько минут):

```bash
bash deploy/scripts/stack.sh deploy
```

Скрипт читает `deploy/.env`, собирает образы `zazzzor-api` и `zazzzor-caddy`,
выполняет `docker stack deploy` и ждёт, пока все сервисы запустятся и пройдут
healthcheck. В конце печатает таблицу задач. У всех пяти должно быть
`Running`.

### Проверка

```bash
bash deploy/scripts/stack.sh ps
```

```bash
docker ps --filter label=com.docker.stack.namespace=zazzzor --format 'table {{.Names}}\t{{.Status}}'
```

У `postgres` и `api` должно быть `(healthy)`. Остальные проверки — как в
разделе 9: `bash deploy/scripts/smoke-api.sh` сам находит контейнер API стека,
`sudo ss -lntup` должен показать 8080, 9464, 55432 и 7880 только на
`127.0.0.1`.

### Логи, обновление, остановка

```bash
bash deploy/scripts/stack.sh logs api
```

Вместо `api` — `caddy`, `livekit`, `postgres` или `duckdns`. Следить за логами
в реальном времени: `bash deploy/scripts/stack.sh logs caddy --follow`.

Обновление:

```bash
git pull && { diff deploy/.env.example deploy/.env; bash deploy/scripts/stack.sh deploy; }
```

Swarm по очереди заменяет изменившиеся сервисы (старая задача
останавливается, затем запускается новая) и откатывает обновление, если новая
версия не стартует. Миграции базы при этом не откатываются, поэтому они
всегда совместимы с предыдущей версией. Изменения `.env` применяются тем же
`deploy`.

Старые образы остаются на диске. Предыдущий нужен для отката, остальные можно
удалить по тегу (`docker image ls zazzzor-api`, `docker rmi zazzzor-api:<тег>`).
`docker image prune -a` удалит и образ для отката, а заодно неиспользуемые
образы других проектов.

Остановить стек (тома с данными остаются; команда ждёт, пока всё
остановится):

```bash
bash deploy/scripts/stack.sh rm
```

### Резервная копия и пароль базы в стеке

Копии в стеке снимает тот же сервис `backup`, команды — те же, что в
разделе 12, через `stack.sh`:

```bash
bash deploy/scripts/stack.sh backup
bash deploy/scripts/stack.sh restore 20261027T023000Z-AbCd1234 --yes
bash deploy/scripts/stack.sh reconcile
```

Перед `deploy` каталог `ZAZZZOR_BACKUP_DIR` должен существовать и
принадлежать UID 65532: иначе `deploy` остановится и напечатает команду,
которая это исправит. Ручные команды раздела 12 в стеке отличаются так:
API останавливается командой `docker service scale zazzzor_api=0` и
запускается `docker service scale zazzzor_api=1`, а вместо
`docker compose ... exec -T postgres` пишите `docker exec -i "$PG"`, где
`PG=$(docker ps -q --filter label=com.docker.swarm.service.name=zazzzor_postgres)`.

Сменить `POSTGRES_PASSWORD`: сначала поменяйте пароль в самой базе, пока
работает старая версия, и только потом разверните стек с новым `.env`. Если
сначала сделать `deploy`, новый API не подключится, Swarm откатит его на
старый пароль, а после смены пароля в базе API останется лежать. Порядок:
впишите новый пароль в `deploy/.env`, затем

```bash
PG=$(docker ps -q --filter label=com.docker.swarm.service.name=zazzzor_postgres)
NEWPW=$(grep -E '^POSTGRES_PASSWORD=' deploy/.env | cut -d= -f2) \
  docker exec -e NEWPW "$PG" sh -c 'psql -U zazzzor -d zazzzor -c "ALTER USER zazzzor PASSWORD '\''$NEWPW'\''"'
bash deploy/scripts/stack.sh deploy
```

`docker exec -e NEWPW` передаёт пароль через окружение, а не командной
строкой, поэтому его не видно в списке процессов.

## 16. Готовые образы (GitHub Container Registry)

Workflow `.github/workflows/images.yml` собирает образы и публикует их в
GHCR, так что на сервере их можно не собирать:

| Образ | Что внутри |
|---|---|
| `ghcr.io/whoaskedssselery/zazzzor-api` | API (`server/Dockerfile`) |
| `ghcr.io/whoaskedssselery/zazzzor-caddy` | Caddy с DuckDNS и статическими страницами (`deploy/caddy/Dockerfile`) |

Если в репозитории заданы переменная `DOCKERHUB_USERNAME` и секрет
`DOCKERHUB_TOKEN` (токен Docker Hub с правом записи), те же образы с теми
же тегами публикуются и в Docker Hub: `<DOCKERHUB_USERNAME>/zazzzor-api` и
`<DOCKERHUB_USERNAME>/zazzzor-caddy`. Публичные образы оттуда сервер
скачивает без входа.

Теги:

| Событие | Теги |
|---|---|
| Успешный CI на `main` | `sha-<12 знаков коммита>`, `main`, `latest` |
| git-тег `v1.2.3` на коммите с успешным CI в `main` | `sha-<...>`, `1.2.3`, `1.2` |
| Аварийный запуск (Actions → Images → Run workflow, укажите причину) | `sha-<...>` |

Разворачивайте по `sha-<...>` или по версии: эти теги не меняются, поэтому
Swarm видит каждое обновление, а откат идёт на конкретный образ. `latest` и
`main` переезжают на новый коммит, и Swarm с `--resolve-image never` такую
смену не заметит.

Тег для текущего коммита на сервере:

```bash
echo "sha-$(git rev-parse --short=12 HEAD)"
```

Новые пакеты GHCR по умолчанию приватные. Либо сделайте их публичными
(GitHub → Packages → пакет → Package settings → Change visibility), либо
войдите на сервере токеном с правом `read:packages`:

```bash
docker login ghcr.io -u whoaskedssselery
```

В файле стека тогда указывается образ из реестра, например
`image: ghcr.io/whoaskedssselery/zazzzor-api:${API_TAG}` с
`API_TAG=sha-<...>`. Флаг `--resolve-image never` для таких образов уберите,
чтобы Swarm скачал образ и закрепил его по digest.

---

## 17. Развёртывание за Traefik (Docker Swarm)

Этот вариант — для сервера, где в Docker Swarm уже работает Traefik. Стек
описан в `deploy/docker-stack.traefik.yml`, команды — те же `stack.sh`.
Edge Caddy и DuckDNS не запускаются: TLS и порт 443 остаются у Traefik.
Веб-контейнер отдаёт клиент по обычному HTTP на 8080 внутри overlay-сети,
наружу этот порт не открыт. Тома — те же `zazzzor_pgdata` и `zazzzor_apidata`.

> **Внимание.** Если на сервере уже работает ручной стек Zazzzor
> (`docker stack ls` показывает `zazzzor`), сначала прочитайте
> [«Переход с ручного стека»](#переход-с-ручного-стека) в конце раздела.
> **Пароль базы переносится оттуда как есть. Не генерируйте новый
> `POSTGRES_PASSWORD` из §5**: с другим паролем API не подключится к
> существующей базе.

1. На менеджере должна существовать внешняя overlay-сеть `traefik`.
   Traefik должен быть подключён к ней и читать service labels Swarm.
   Нужны HTTPS entrypoint `websecure` и certresolver `letsencrypt`.
   Если в вашем Traefik они называются иначе, раскомментируйте и измените
   в `deploy/.env` `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT` и
   `TRAEFIK_CERTRESOLVER`. Стек рассчитан на Swarm provider Traefik v3
   (`--providers.swarm`): сеть указана label `traefik.swarm.network`.
   Labels Docker provider (`traefik.docker.*`) рядом не ставьте: увидев оба
   набора, Traefik v3 пропускает сервис («both Docker and Swarm labels are
   defined»), и сайт отвечает его 404. У Traefik v2
   (`--providers.docker.swarmMode`) этот label не читается: задайте сеть
   его флагом `--providers.docker.network`.

2. Новая установка: создайте `deploy/.env` по §5 и заполните в нём
   `ZAZZZOR_DOMAIN` (домен приложения), `LIVEKIT_API_KEY`,
   `LIVEKIT_API_SECRET`, `POSTGRES_PASSWORD` и `ZAZZZOR_ADMIN_USERNAMES`.
   `DUCKDNS_*` и `ACME_EMAIL` здесь не нужны, оставьте их пустыми. Создайте
   каталог копий (§12, шаг 1). Затем найдите в `deploy/.env` строки ниже,
   **раскомментируйте** их (уберите `# ` в начале) **и измените**:

   <!-- traefik-env -->
   ```bash
   ZAZZZOR_EDGE=traefik
   LIVEKIT_NODE_IP=<публичный IPv4 сервера>
   TRAEFIK_SUBNET=<CIDR сети traefik>
   ZAZZZOR_API_REPOSITORY=ghcr.io/whoaskedssselery/zazzzor-api
   ZAZZZOR_CADDY_REPOSITORY=ghcr.io/whoaskedssselery/zazzzor-caddy
   ```

   Не дописывайте эти строки в конец файла: если ключ встретится дважды,
   `stack.sh` остановится и назовёт обе строки.

   CIDR сети Traefik:

   ```bash
   docker network inspect traefik --format '{{range .IPAM.Config}}{{.Subnet}} {{end}}'
   ```

   Не указывайте весь `10.0.0.0/8`: API верит адресам клиентов из
   `X-Forwarded-For` только от адресов этой сети. В Traefik не включайте
   `forwardedHeaders.insecure`. Ещё две оговорки:
   - Любой контейнер в сети `traefik` тоже входит в этот CIDR: он может
     обратиться к `api:8080` напрямую и подделать `X-Forwarded-For`.
     Подключайте к сети `traefik` только сервисы, которым доверяете.
   - Traefik должен публиковать 443 в режиме `host`. В режиме `ingress`
     Swarm подменяет адрес клиента, и для API все друзья — один адрес с
     общими лимитами. Проверка (у 443 должно быть `"PublishMode":"host"`):

     ```bash
     docker service inspect <сервис traefik> --format '{{json .Endpoint.Ports}}'
     ```

   PostgreSQL, метрики и административный API Caddy наружу не публикуются.
   `LIVEKIT_NODE_IP` обязателен: здесь медиа идут через docker_gwbridge
   (`rtc.ips.includes: 172.16.0.0/12`), STUN не переопределяет адрес.
   При смене публичного IP обновите переменную и повторите deploy.

3. Сохраните предыдущие теги для отката. Разверните опубликованную пару
   образов одного коммита (§16):

   ```bash
   export ZAZZZOR_API_TAG=sha-<12 знаков коммита>
   export ZAZZZOR_CADDY_TAG=sha-<те же 12 знаков>
   bash deploy/scripts/stack.sh deploy
   bash deploy/scripts/stack.sh ps
   bash deploy/scripts/stack.sh logs web --follow
   ```

   Скрипт скачивает отсутствующие GHCR-образы; для приватных нужен
   `docker login ghcr.io`. Все сервисы и локальные тома закреплены за этим
   менеджером, поэтому образы не требуется раздавать другим узлам.
   Используйте неизменяемые `sha-…` теги; `main` и `latest` не подходят.
   Для локальной сборки оставьте строки `*_REPOSITORY` закомментированными
   и не задавайте теги: используется прежний расчёт тега по содержимому.
   `*_REPOSITORY` действуют только при `ZAZZZOR_EDGE=traefik`; edge Caddy
   откажется запускаться с другими репозиториями. Переменная Compose
   `ZAZZZOR_API_IMAGE` по-прежнему означает полный образ с тегом.
   `config` печатает секреты — не публикуйте его вывод.

4. Для обновления подтяните код (в том числе backup-скрипты), задайте новую
   пару тегов и повторите deploy. Ошибка обновления откатывает сервис.
   Для ручного отката задайте предыдущие два тега и повторите deploy.
   Миграции идут только вперёд: предыдущая версия должна понимать текущую
   схему. Восстановление данных — отдельная операция по §12.
   `ps`, `logs <api|web|postgres|livekit|backup>` и `rm` работают как в §15.
   `rm` останавливает только этот стек, внешняя сеть Traefik остаётся.

5. Проброс MikroTik и firewall — как в §7: 443/TCP до Traefik,
   7881/TCP и 7882/UDP непосредственно до узла с LiveKit (host mode).
   Порты 5432, 8080, 7880 и 9464 наружу не открывайте.
   `/rtc` направляется в LiveKit, `/api/` — в API (включая gateway).
   Остальные пути получает web с меньшим приоритетом маршрута.

6. TURN/TLS нужен друзьям, у которых UDP заблокирован (VPN, строгие сети).
   Создайте отдельную DNS-запись на тот же IP, затем в `deploy/.env`
   раскомментируйте и измените:

   <!-- traefik-turn-env -->
   ```bash
   LIVEKIT_TURN_ENABLED=true
   LIVEKIT_TURN_DOMAIN=<отдельный домен TURN>
   ```

   Повторите deploy. Скрипт добавит `docker-stack.traefik-turn.yml`:
   TCP-router по SNI на `websecure`, TLS у Traefik, `external_tls: true` у
   LiveKit. Внутренний TURN listener на 443 не публикуется на хост; наружный
   443 занят Traefik. Домен TURN обязан отличаться от домена приложения,
   иначе TCP-router перехватит HTTPS приложения. Проверьте, что TURN
   запустился:

   ```bash
   bash deploy/scripts/stack.sh logs livekit | grep 'Starting TURN server'
   ```

   В строке должно быть `"turn.portTLS": 443`. Если вместо неё в логах
   `bind: permission denied`, на узле изменён sysctl
   `net.ipv4.ip_unprivileged_port_start` (Docker по умолчанию ставит 0), и
   LiveKit без capabilities не может слушать 443: напишите разработчику.

   Чтобы выключить TURN, задайте `LIVEKIT_TURN_ENABLED=false` и повторите
   deploy: TCP-router будет удалён из labels сервиса. Если строка осталась
   закомментированной, а в работающем стеке TURN есть, deploy остановится
   и попросит выбрать явно: обновление не выключает TURN молча.

7. Проверьте `https://<домен>/api/v1/healthz`, главную страницу, вход и
   gateway в DevTools → Network → WS (`/api/v1/gateway`, статус 101).
   Оставьте вкладку открытой несколько минут: heartbeat должен продолжаться.
   Затем откройте `/connectivity-test/` и выполните проверку связи ниже,
   в том числе из сети с заблокированным UDP при включённом TURN.

   Traefik поддерживает WebSocket upgrade автоматически. Его стандартные
   `respondingTimeouts.readTimeout=60s` относятся к чтению HTTP-запроса,
   `writeTimeout=0s` не ограничивает запись ответа. Не добавляйте middleware
   с общим deadline для gateway. Если в существующем Traefik переопределены
   таймауты, проверьте entrypoint `websecure` и его `serversTransport`:
   оставьте `writeTimeout=0s`, `responseHeaderTimeout=0s`, а при наличии
   transport read/write timeouts — `0s`. `idleConnTimeout` относится к
   свободным keep-alive соединениям, а не к активному WebSocket.
   Настройки меняет владелец существующего Traefik, этот стек их не заменяет.
   Источники: [WebSocket](https://doc.traefik.io/traefik/user-guides/websocket/),
   [entrypoints](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/),
   [serversTransport](https://doc.traefik.io/traefik/reference/routing-configuration/http/load-balancing/serverstransport/).

8. Бэкапы и restore из §12 работают через те же команды `stack.sh backup`,
   `stack.sh restore <копия> --yes`, `stack.sh reconcile`. При
   `ZAZZZOR_EDGE=traefik` задания подключаются к внутренней attachable-сети
   `<STACK_NAME>_db`, Postgres доступен как `postgres:5432`.
   Сеть не имеет внешнего доступа; в ней только API, Postgres и backup.

### Переход с ручного стека

Ручной стек (`docker-stack.traefik.yml`, переменные в командной строке
`docker stack deploy`) обновляется на этот обычным `stack.sh deploy`: без
`stack rm` и без простоя дольше перезапуска сервисов. Тома остаются.
Команды ниже — для стека `zazzzor`; если `docker stack ls` показывает другое
имя, подставьте его и перед каждой командой `stack.sh` выполните
`export STACK_NAME=<имя>`.

1. На всякий случай снимите дамп базы (API останавливать не нужно):

   ```bash
   PG=$(docker ps -q --filter label=com.docker.swarm.service.name=zazzzor_postgres)
   docker exec "$PG" pg_dump -U zazzzor -d zazzzor -Fc > ~/zazzzor-before-traefik.dump
   ls -l ~/zazzzor-before-traefik.dump
   ```

2. Посмотрите старые значения в работающих сервисах. **В выводе секреты —
   не публикуйте его:**

   ```bash
   for s in api postgres livekit; do
     docker service inspect "zazzzor_$s" --format '{{range .Spec.TaskTemplate.ContainerSpec.Env}}{{println .}}{{end}}'
   done
   docker service inspect zazzzor_api zazzzor_web --format '{{.Spec.TaskTemplate.ContainerSpec.Image}}'
   ```

3. Создайте `deploy/.env` (`cp deploy/.env.example deploy/.env` и
   `chmod 600 deploy/.env`) и перенесите значения по таблице.
   **Команды `openssl rand` из §5 здесь не запускайте: все секреты уже есть.**

   | Было в ручном стеке | Стало в `deploy/.env` | Что писать |
   |---|---|---|
   | `DB_PASSWORD` (`POSTGRES_PASSWORD` у `zazzzor_postgres`) | `POSTGRES_PASSWORD` | **то же значение; новый пароль не генерировать** |
   | `ZAZZZOR_DOMAIN` | `ZAZZZOR_DOMAIN` | то же значение |
   | `PUBLIC_IP` (`node_ip` в `LIVEKIT_CONFIG`) | `LIVEKIT_NODE_IP` | то же значение, раскомментировать |
   | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` (ключ и секрет в `keys:`) | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | те же значения |
   | `TURN_DOMAIN` (TURN был включён всегда) | `LIVEKIT_TURN_ENABLED`, `LIVEKIT_TURN_DOMAIN` | **`true`** и тот же домен (п. 6), иначе друзья за VPN потеряют голос |
   | `ADMIN_USERNAMES` | `ZAZZZOR_ADMIN_USERNAMES` | то же значение |
   | `TRAEFIK_SUBNET` (по умолчанию было `10.0.0.0/8`) | `TRAEFIK_SUBNET` | CIDR сети `traefik` (п. 2), не `10.0.0.0/8` |
   | `API_TAG` (образ `zazzzor-api:<тег>`) | `ZAZZZOR_API_TAG` | новый `sha-…` из GHCR (п. 3); старый тег запишите для отката |
   | `WEB_TAG` (образ `zazzzor-caddy:<тег>`) | `ZAZZZOR_CADDY_TAG` | тот же новый `sha-…`: в старом образе нет веб-клиента |
   | сеть `traefik`, `websecure`, `letsencrypt` | `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, `TRAEFIK_CERTRESOLVER` | ничего: это значения по умолчанию |
   | `postgres:18.6-alpine`, `livekit/livekit-server:v1.13.10` | `POSTGRES_VERSION`, `LIVEKIT_VERSION` | ничего: те же версии уже в примере |
   | тома `zazzzor_pgdata`, `zazzzor_apidata` | те же тома | ничего: имя стека то же |
   | сеть `zazzzor_zazzzor` | сеть `zazzzor_db` | ничего: создаётся сама, старую удалите в п. 7 |
   | — | `ZAZZZOR_EDGE`, `*_REPOSITORY` | блок из п. 2 |
   | — | `ZAZZZOR_BACKUP_DIR` | каталог копий (§12, шаг 1) |

4. Проверьте, что пароль совпал (сам пароль не печатается):

   ```bash
   old=$(docker service inspect zazzzor_postgres --format '{{range .Spec.TaskTemplate.ContainerSpec.Env}}{{println .}}{{end}}' | sed -n 's/^POSTGRES_PASSWORD=//p')
   new=$(sed -n 's/^POSTGRES_PASSWORD=//p' deploy/.env)
   if [ -n "$old" ] && [ "$old" = "$new" ]; then echo 'пароль совпадает'; else echo 'НЕ совпадает: исправьте POSTGRES_PASSWORD до deploy'; fi
   ```

5. Разверните по п. 3. Swarm обновит сервисы на месте: создаст сеть
   `zazzzor_db` и переведёт в неё API, Postgres и backup, каждый сервис
   перезапустится один раз. API сам ждёт базу до 60 секунд.

6. Проверьте п. 7, TURN по п. 6 и копию: `bash deploy/scripts/stack.sh backup`.

7. Удалите старую сеть, она больше не нужна:

   ```bash
   docker network rm zazzzor_zazzzor
   ```

   Если Docker отвечает `has active endpoints`, в ней ещё есть контейнер:
   `docker network inspect zazzzor_zazzzor --format '{{range .Containers}}{{.Name}} {{end}}'`.

Если deploy не удался, Swarm откатывает упавший сервис. Откаченный API
окажется в старой сети, где базы уже нет, и сам не поднимется: найдите
причину (`bash deploy/scripts/stack.sh logs api`, чаще всего — пароль),
исправьте `deploy/.env` и повторите deploy. Вернуть ручной стек целиком:
разверните старый YAML прежней командой, тома те же. Если новый API уже
обновил схему базы, старый может с ней не запуститься; тогда восстановите
дамп из п. 1 по §12 «Ручная копия, если скрипты не работают» с поправками
для стека из §15.

### Голосовой API за Traefik: обновление и диагностика

1. Используйте API и web из одной версии этого репозитория. Сохраните
   текущий `deploy/.env`: `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, пароль
   Postgres, `LIVEKIT_NODE_IP` и TURN не меняются. Тома `pgdata` и
   `apidata` сохраняются. Настройки Traefik из раздела 17 остаются прежними.
2. Сохраните SHA и теги образов предыдущей рабочей версии. Проверьте
   `stack.sh config` локально (вывод содержит секреты, его не публикуйте),
   затем обновите через `stack.sh deploy`, как выше. Удалять стек не нужно.
   Swarm создаст отдельную внутреннюю сеть `<имя стека>_voice`; к ней
   подключатся только API и LiveKit. Новые внешние сети и порты не нужны.
3. API обращается к `http://voice-sfu:7880`, LiveKit отправляет подписанные
   события на `http://voice-api:8080/api/v1/livekit/webhook`. Эти имена
   существуют только в control-сети. Публичный путь webhook направляется
   router с приоритетом 200 в web, где Caddy отвечает 404. API-router
   сохраняет приоритет 100. Медиа-порты 7881/TCP, 7882/UDP, фильтр интерфейса
   LiveKit и TURN через Traefik не меняются.
4. Публичная проверка не требует токена:

   ```bash
   curl -i -X POST https://<домен>/api/v1/livekit/webhook
   ```

   Ожидается 404. Если API выдаёт токен, но вход в голос не завершается,
   смотрите `stack.sh logs api` и `stack.sh logs livekit`: ошибки SDK
   указывают на направление API → LiveKit, ошибки доставки webhook — на
   LiveKit → API. Проверьте наличие обоих сервисов в сети `_voice` и
   совпадение используемой пары ключ/секрет, не публикуя их значения.
   Успешный `/readyz` подтверждает только БД, а не связь с LiveKit.
   Метрика `zazzzor_voice_reconcile_last_success_timestamp_seconds`
   доступна только внутри API на `127.0.0.1:9464`; в Traefik-варианте
   этот loopback принадлежит контейнеру, а не хосту.
5. Обновление использует `stop-first`: активный звонок может прерваться,
   клиенту потребуется переподключиться. Для отката верните весь предыдущий
   SHA и соответствующие ему теги API/web, затем выполните `stack.sh deploy`
   с сохранённым `.env`. Не откатывайте отдельный сервис отдельно от
   конфигурации. Не удаляйте тома; оставшаяся неиспользуемая сеть `_voice`
   не требует ручной очистки. Этот инкремент не добавляет миграций БД.

## Проверка связи (этап 1.1)

Страница `https://<домен>/connectivity-test/` проверяет, как у друзей
работает голос и демка через LiveKit, до того как это появится в приложении.
Друзья заходят на неё по персональным ссылкам, созваниваются, запускают
демку и присылают отчёт. Стек тот же: после разделов 1–9 всё уже запущено.

### Проверка страницы

Откройте `https://<домен>/connectivity-test/` (если HTTPS-порт не 443 и вы
не пробрасывали внешний 443 — `https://<домен>:<порт>/connectivity-test/`).
Должна открыться страница проверки связи; без токена в ссылке она сообщит, что
токена нет — это нормально.

Проверяйте с телефона через мобильный интернет, а не из домашней сети:
многие роутеры не умеют открывать собственный внешний адрес изнутри.

### Ссылки для друзей

```bash
bash deploy/scripts/make-test-links.sh Вася Петя Маша
```

Скрипт печатает по строке на человека:

```
Вася: https://zazzzor.duckdns.org/connectivity-test/#token=eyJ...
```

Ссылки действуют 24 часа. Каждому отправляйте только его ссылку (в личку):
имя в ссылке — это то, под которым человек будет виден в комнате. Одна ссылка —
одна вкладка: если открыть её второй раз, первая вкладка отключится с
сообщением «Эта ссылка открыта в другой вкладке или на другом устройстве». Если
`docker` требует `sudo`, запускайте скрипт через `sudo`.

**Перед тем как рассылать ссылки**, выпустите ссылку себе
(`bash deploy/scripts/make-test-links.sh Я-телефон`), откройте её на телефоне
через мобильный интернет (Wi-Fi выключен), нажмите «Подключиться» и убедитесь,
что в таблице показателей «Протокол udp». Если там `tcp` или подключение не
устанавливается, сначала разберитесь с пробросом 7882/UDP (см. ниже «Если
связь не работает»), иначе проверка с друзьями ничего не покажет.

TURN на этом этапе выключен, отдельный порт для него не нужен: проверка
измеряет прямое соединение по UDP и запасное по TCP. TURN/TLS на 443 — запасной
вариант, к нему вернёмся, только если отчёты покажут, что у кого-то UDP
заблокирован.

### Проверка с друзьями

1. Выпустите ссылки на всех участников, включая себя.
2. **Владелец сервера участвует не из домашней сети**, а через раздачу с
   телефона или мобильный интернет. Из домашней сети трафик идёт к серверу
   напрямую по LAN или через hairpin NAT (роутер разворачивает обращение к
   своему же внешнему IP обратно внутрь), минуя интернет, и такой отчёт ничего
   не говорит о реальной связи.
3. Созвон: все заходят из Chrome или Edge, без VPN, говорят 2–3 минуты.
4. Одна демка: один человек стримит видео 1080p60 (ролик с движением)
   5 минут, остальные смотрят. Стримящий выбирает в окне выбора **вкладку
   Chrome** с роликом и включает «Поделиться звуком вкладки», а не весь экран:
   звук всего экрана захватит и голоса остальных из колонок, и все услышат эхо.
5. Две демки одновременно 5 минут (так же, вкладками).

Инструкция для друзей (отправьте вместе со ссылкой):

> Открой ссылку в Chrome или Edge, нажми «Подключиться» и разреши микрофон.
> В конце каждого этапа нажми «Скопировать отчёт» и пришли мне текст одним
> сообщением, без правок, а потом нажми «Новый замер» (связь при этом не
> рвётся). Этапы: созвон → одна демка → две демки, то есть три отчёта.

«Новый замер» обнуляет статистику и время начала, поэтому каждый отчёт
описывает ровно один этап.

Как читать отчёты:

| Что | Норма |
|---|---|
| Подключились все | да, без VPN |
| Транспорт (`protocolsSeen`) | только `udp` у всех; `tcp` — повод пересмотреть проброс 7882/UDP |
| Одна демка, `minInboundFps` у зрителей | ≥ 55 |
| Одна демка, `maxInboundHeight` / `minInboundHeight` | 1080 / не ниже 720 |
| Одна демка, `minInboundKbps` | без провалов до сотен кбит/с (обычно 2000–8000) |
| Две демки, `minInboundFps` | ≥ 55 (разрешение может быть ниже 1080p) |
| `maxLossPercent` (худшие 2 с) / `avgLossPercent` | < 5 / < 2 |
| `disconnects`, `reconnectAttempts` | 0 |
| `outboundLimitations` у стримящего | без `bandwidth` при одной демке |

Ещё в отчёте: `userAgent` (версия браузера), `startedAt` и `durationSec`
(когда начался и сколько длился этап), `disconnectReasons` (почему рвалась
связь).

Если у кого-то транспорт не UDP или большие потери — сохраните отчёты: по ним
решаем, нужен ли TURN/TLS на 443.

### Если связь не работает

- **Страница открывается, но подключение не устанавливается.** Скорее всего,
  не проброшены 7881/TCP и 7882/UDP или их режет файрвол сервера.
- **LiveKit не может узнать внешний IP.** При запуске LiveKit один раз
  спрашивает свой внешний IP у STUN-серверов (по умолчанию Google и Twilio).
  В `logs livekit` при успехе есть строка `found external IP via STUN`.
  Если STUN недоступен, есть два варианта (оба в `deploy/.env`):
  - указать другие STUN-серверы:
    `LIVEKIT_STUN_SERVERS=stun.cloudflare.com:3478,stun.l.google.com:19302`;
  - указать IP вручную: `LIVEKIT_NODE_IP=<ваш внешний IP>` (узнать можно на
    <https://2ip.ru>). Тогда STUN не используется вовсе, но при каждой смене
    IP это значение придётся менять самому.

  После правки `.env`: `docker compose -f deploy/docker-compose.yml up -d`.
- **Сменился внешний IP (у домашнего интернета он часто динамический).**
  Контейнер `duckdns` обновит только имя `<домен>` — страница и вход будут
  работать, но LiveKit продолжит сообщать участникам старый IP, и звук с видео
  пропадут. Перезапустите LiveKit, чтобы он заново узнал IP:

  ```bash
  docker compose -f deploy/docker-compose.yml restart livekit
  ```

  Если задан `LIVEKIT_NODE_IP`, сначала впишите в него новый IP и выполните
  `docker compose -f deploy/docker-compose.yml up -d`.
- **Порт в ссылке.** Если HTTPS-порт не 443 и `ZAZZZOR_PUBLIC_PORT` не задан,
  скрипт добавляет порт в ссылку.
