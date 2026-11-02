# Zazzzor

Самостоятельно размещаемый аналог Discord для небольшой компании друзей:
серверы, текстовые каналы, роли и права, приглашения, а дальше — голос и
демонстрация экрана. Всё работает на одном домашнем Linux-сервере в Docker,
без облачных сервисов.

## Статус

| Этап | Что | Состояние |
|---|---|---|
| 1.1 | Проверка связи: LiveKit, Caddy, DuckDNS, страница теста голоса и демки | готово |
| 1.2a | Бэкенд: REST API, realtime-шлюз WebSocket, Postgres, развёртывание | готово: API и шлюз реального времени |
| 1.2b | Веб-клиент | в работе: каркас, раздача через Caddy |
| 1.3 | Голос и демка в приложении | впереди |
| 1.4 | Десктоп-клиент | впереди |
| 1.5 | Эксплуатация: бэкапы, обновления, релизы | впереди |

Пока веб-клиент — заготовка без входа и чата, друзьям после установки
доступна только страница проверки связи.

## Стек

- Сервер: Go (модульный монолит), PostgreSQL, WebSocket
  ([coder/websocket](https://github.com/coder/websocket)), sqlc, goose.
- Голос и видео: [LiveKit](https://livekit.io).
- HTTPS: Caddy с сертификатами Let's Encrypt через DuckDNS (DNS-01).
- Веб-клиент: React 19, Vite, TypeScript, TanStack Router и Query, Tailwind CSS;
  типы API генерируются из `api/openapi.yaml`.
- Страница проверки связи: TypeScript, Vite, pnpm.

## Структура

| Каталог | Что |
|---|---|
| `server/` | сервер API (`go.mod`, `cmd/zazzzor`, `internal/`) |
| `api/openapi.yaml` | описание REST API; тесты сверяют с ним каждый ответ |
| `web/` | веб-клиент (пакет `@zazzzor/web`) |
| `deploy/` | Docker Compose, Caddy, скрипты и [инструкция по запуску на сервере](deploy/README.md) |
| `tools/connectivity-test/` | страница проверки связи (этап 1.1) |

## Запуск на сервере

Пошагово, от установки Docker до первого администратора и резервных копий:
[deploy/README.md](deploy/README.md).

## Разработка

Нужны Go (версия из `server/go.mod`) и Docker: тесты поднимают настоящий
Postgres через testcontainers.

```bash
cd server
go vet ./...
go test ./...
```

Запуск сервера локально с Postgres в Docker (порт 55432, чтобы не мешать
другому Postgres на 5432):

```bash
docker run -d --name zazzzor-dev-pg -e POSTGRES_USER=zazzzor -e POSTGRES_PASSWORD=dev \
  -e POSTGRES_DB=zazzzor -p 127.0.0.1:55432:5432 postgres:18.6-alpine

cd server
ZAZZZOR_DATABASE_URL='postgres://zazzzor:dev@127.0.0.1:55432/zazzzor?sslmode=disable' \
ZAZZZOR_ALLOWED_ORIGINS=http://localhost:5173 \
ZAZZZOR_COOKIE_SECURE=false \
ZAZZZOR_DATA_DIR=/tmp/zazzzor-data \
go run ./cmd/zazzzor
```

Сервер слушает `:8080` (API) и `:9090` (метрики Prometheus) и сам применяет
миграции. Проверка: `curl http://127.0.0.1:8080/api/v1/readyz`. Сквозная
проверка: `go run ./cmd/zazzzor smoke http://127.0.0.1:8080` (с тем же
`ZAZZZOR_ALLOWED_ORIGINS`). Удалить базу: `docker rm -f zazzzor-dev-pg`.

Остальные переменные (`ZAZZZOR_ADMIN_USERNAMES`, `ZAZZZOR_TRUSTED_PROXIES`,
адреса) описаны в `server/internal/config`.

### Веб-клиент

Нужны Node 22 и pnpm из `package.json` (`corepack enable` его поставит).
Из корня репозитория:

```bash
pnpm install
pnpm dev        # http://localhost:5173
```

Dev-сервер Vite пересылает `/api` (включая WebSocket шлюза) на
`http://127.0.0.1:8080`, поэтому рядом должен работать сервер из раздела выше,
запущенный именно с `ZAZZZOR_ALLOWED_ORIGINS=http://localhost:5173` (иначе
API отвечает `403 origin_not_allowed`) и `ZAZZZOR_COOKIE_SECURE=false` (иначе
браузер не сохранит cookie сессии на http). Порт 5173 фиксирован: если он
занят, Vite не стартует, а не уходит молча на другой порт, который API не
разрешает. Открывайте именно `http://localhost:5173`, не `127.0.0.1`.

Проверки (их же запускает CI):

```bash
pnpm lint       # ESLint
pnpm test       # Vitest во всех пакетах
pnpm build      # проверка типов и сборка всех пакетов
pnpm gen:api    # перегенерировать web/src/api/schema.gen.ts из api/openapi.yaml
```

После любого изменения `api/openapi.yaml` запустите `pnpm gen:api` и
закоммитьте `web/src/api/schema.gen.ts`: CI падает, если файл расходится с
контрактом.

### Страница проверки связи

`pnpm --filter @zazzzor/connectivity-test test` и `... build`; инструкция по
ней — в [deploy/README.md](deploy/README.md#проверка-связи-этап-11).

## Лицензия

[Apache License 2.0](LICENSE).
