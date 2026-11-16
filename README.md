# Zazzzor

[![CI](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/ci.yml)
[![Images](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/images.yml/badge.svg)](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/images.yml)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Свой Discord для компании друзей на одном домашнем сервере: серверы и
каналы, чат, голос и демонстрация экрана в 1080p/60, без облачных сервисов и
без VPN. Всё запускается в Docker; образы публикуются на Docker Hub.

## Возможности

**Общение**
- Серверы с текстовыми и голосовыми каналами, категориями и приглашениями.
- Сообщения с ответами, правкой, удалением, реакциями, упоминаниями,
  эмодзи, вложениями и превью ссылок; поиск по сообщениям.
- Друзья, личные сообщения и группы; блокировка.
- Индикатор «печатает», непрочитанное, уведомления об упоминаниях со звуком.

**Голос и экран**
- Голосовые каналы и личные звонки со звонком собеседнику.
- Демонстрация экрана до 1080p/60 с автоматическим снижением до 720p, когда
  не держится 30 fps; звук вкладки в Chrome и Edge.
- Громкость каждого человека и каждой демонстрации от 0 до 200 %.
- Шумоподавление на компьютере пользователя: DeepFilterNet3, с запасным
  RNNoise.
- Кольцо говорящего по реальному звуку, звуки входа, выхода и микрофона.

**Управление**
- Роли и права на уровне сервера и канала, журнал аудита.
- Администрирование инстанса: пользователи, регистрация, настройки.

**Эксплуатация**
- Один стек Docker: API, Postgres, LiveKit, Caddy с HTTPS.
- Резервные копии по расписанию и проверенное восстановление.
- Готовые образы на каждый зелёный коммит в `main`.

## Стек

| Часть | Технологии |
|---|---|
| Сервер | Go (модульный монолит), PostgreSQL, sqlc, goose, WebSocket ([coder/websocket](https://github.com/coder/websocket)) |
| Голос и видео | [LiveKit](https://livekit.io) (SFU, TURN) |
| Веб-клиент | React 19, TypeScript, Vite, TanStack Router и Query, Tailwind CSS; архитектура Feature-Sliced Design |
| Контракт | OpenAPI 3.1 в `api/openapi.yaml`: типы клиента генерируются из него, тесты сервера сверяют с ним каждый ответ |
| Развёртывание | Docker Compose или Swarm, Caddy (Let's Encrypt), Traefik |
| Проверки | Go race-тесты с настоящим Postgres (testcontainers), Vitest, Playwright в Chromium и Firefox, govulncheck, gitleaks |

## Состояние

| Этап | Что | Состояние |
|---|---|---|
| 1.1 | Проверка связи у друзей: LiveKit, Caddy, страница теста | готово |
| 1.2 | Бэкенд и веб-клиент: серверы, каналы, чат, роли, администрирование | готово |
| 1.3 | Голос, звонки и демонстрация экрана в браузере | готово, проверено на живом сервере |
| 1.4 | Десктоп-клиент для Windows (Tauri, нативный захват экрана и звука) | впереди |

Веб-клиент работает в актуальных Chrome, Edge и Firefox. Звук демонстрации
браузер отдаёт только из вкладки Chrome и Edge; полный звук окна и игр будет
в десктоп-клиенте.

## Структура

| Каталог | Что |
|---|---|
| [`server/`](server) | API и realtime-шлюз (`cmd/zazzzor`, модули в `internal/`) |
| [`web/`](web) | веб-клиент (`@zazzzor/web`), сценарии Playwright в `web/e2e/` |
| [`api/openapi.yaml`](api/openapi.yaml) | описание REST API |
| [`deploy/`](deploy) | стеки Docker, Caddy, образ резервных копий, скрипты и [инструкция по запуску](deploy/README.md) |
| [`tools/connectivity-test/`](tools/connectivity-test) | страница проверки связи до сервера |

## Запуск на сервере

Пошагово, от установки Docker до первого администратора и резервных копий:
[deploy/README.md](deploy/README.md). Готовые образы:

```
ssselery/zazzzor-api
ssselery/zazzzor-caddy
ssselery/zazzzor-backup
```

с тегами `sha-<коммит>`, `main` и `latest`; те же образы есть в
`ghcr.io/whoaskedssselery/`.

## Разработка

Нужны Go (версия из `server/go.mod`), Node 22 с pnpm (`corepack enable`) и
Docker: тесты сервера поднимают настоящий Postgres.

### Сервер

```bash
cd server
go vet ./...
go test ./...
```

Локальный запуск с Postgres в Docker (порт 55432, чтобы не мешать другому
Postgres на 5432):

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
миграции. Проверка: `curl http://127.0.0.1:8080/api/v1/readyz`; сквозная
проверка: `go run ./cmd/zazzzor smoke http://127.0.0.1:8080`. Остальные
переменные описаны в `server/internal/config`.

### Веб-клиент

Из корня репозитория, рядом с запущенным сервером:

```bash
pnpm install
pnpm dev        # http://localhost:5173
```

Vite пересылает `/api` и WebSocket шлюза на `http://127.0.0.1:8080`, поэтому
сервер должен быть запущен с `ZAZZZOR_ALLOWED_ORIGINS=http://localhost:5173`
и `ZAZZZOR_COOKIE_SECURE=false`. Открывайте именно `http://localhost:5173`.

Проверки (их же запускает CI):

```bash
pnpm lint       # ESLint
pnpm test       # Vitest во всех пакетах
pnpm build      # проверка типов и сборка
pnpm gen:api    # перегенерировать типы из api/openapi.yaml
```

После изменения `api/openapi.yaml` запустите `pnpm gen:api` и закоммитьте
`web/src/shared/api/schema.gen.ts`: CI сверяет его с контрактом.

### Браузерные тесты

Сценарии Playwright идут в Chromium и Firefox против собранного клиента,
настоящего API и LiveKit. Из `web/`:

```bash
pnpm exec playwright install chromium firefox   # один раз
pnpm e2e:up     # Postgres, API и LiveKit из web/e2e/compose.yml
pnpm e2e        # сборка, vite preview с CSP из Caddy, все сценарии
pnpm e2e:down   # убрать стек вместе с базой
```

## Лицензия

[Apache License 2.0](LICENSE).
