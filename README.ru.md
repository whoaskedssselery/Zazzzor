# Zazzzor

[English](README.md) · Русский

[![CI](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/ci.yml)
[![Images](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/images.yml/badge.svg)](https://github.com/whoaskedssselery/Zazzzor/actions/workflows/images.yml)
[![Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Своя платформа для общения с друзьями на одном домашнем сервере. Здесь есть
серверы и каналы, чат, голос и демонстрация экрана в 1080p при 60 кадрах/с.
Облачные сервисы и VPN не нужны. Всё работает в Docker, готовые образы
публикуются в Docker Hub.

Скачайте Windows-клиент со страницы [релизов](https://github.com/whoaskedssselery/Zazzzor/releases).
Подробности в [инструкции по установке](deploy/desktop.ru.md).

## Возможности

**Общение**

- Серверы с текстовыми и голосовыми каналами, категориями и приглашениями.
- Ответы, редактирование и удаление сообщений, реакции, упоминания, эмодзи,
  вложения, превью ссылок и поиск по сообщениям.
- Друзья, личные сообщения, групповые чаты и блокировки.
- Индикатор набора, непрочитанные сообщения и звуковые уведомления об упоминаниях.

**Голос и демонстрация**

- Голосовые каналы и личные звонки со звуковым сигналом.
- Демонстрация до 1080p и 60 кадров/с с переходом на 720p, если частота
  опускается ниже 30 кадров/с. В Chrome и Edge можно передавать звук вкладки.
- Громкость каждого участника и каждой демонстрации от 0 до 200 %.
- Шумоподавление на компьютере пользователя через DeepFilterNet3, запасной
  вариант — RNNoise.
- Индикатор речи по слышимому звуку и сигналы подключения, выхода и выключения микрофона.

**Работа сервера**

- Роли и права на уровне сервера и каналов, журнал действий.
- Управление пользователями, регистрацией и настройками экземпляра.
- Один стек Docker с API, PostgreSQL, LiveKit и Caddy для HTTPS.
- Резервные копии по расписанию с проверенным восстановлением.
- Образы для каждого коммита в `main`, успешно прошедшего CI.

## Стек

| Часть | Технологии |
|---|---|
| Сервер | Go (модульный монолит), PostgreSQL, sqlc, goose, WebSocket ([coder/websocket](https://github.com/coder/websocket)) |
| Голос и видео | [LiveKit](https://livekit.io) (SFU, TURN) |
| Веб-клиент | React 19, TypeScript, Vite, TanStack Router и Query, Tailwind CSS, Feature-Sliced Design |
| Контракт | OpenAPI 3.1 в `api/openapi.yaml`. Из контракта генерируются типы клиента, серверные тесты сверяют с ним ответы |
| Развёртывание | Docker Compose или Swarm, Caddy (Let's Encrypt), Traefik |
| Проверки | Go race-тесты с настоящим Postgres (testcontainers), Vitest, Playwright в Chromium и Firefox, govulncheck, gitleaks |

## Состояние

| Этап | Что вошло | Состояние |
|---|---|---|
| 1.1 | Проверка связи из сетей друзей: LiveKit, Caddy и тестовая страница | готово |
| 1.2 | Сервер и веб-клиент: каналы, чат, роли, администрирование | готово |
| 1.3 | Голос, звонки и демонстрация в браузере | работает на живом сервере |
| 1.4 | Windows-клиент на Tauri с нативным захватом экрана и звука | выпущен |

Веб-клиент работает в актуальных Chrome, Edge и Firefox. В браузере звук
демонстрации доступен при показе вкладки Chrome или Edge. Windows-клиент
может передавать звук окна и игры.

## Структура проекта

| Путь | Содержимое |
|---|---|
| [`server/`](server) | API и шлюз событий (`cmd/zazzzor`, модули в `internal/`) |
| [`web/`](web) | Веб-клиент `@zazzzor/web` и сценарии Playwright в `web/e2e/` |
| [`api/openapi.yaml`](api/openapi.yaml) | Контракт REST и шлюза |
| [`deploy/`](deploy) | Стеки Docker, Caddy, образ бэкапов, скрипты и [руководство владельца](deploy/README.ru.md) |
| [`tools/connectivity-test/`](tools/connectivity-test) | Страница проверки связи с сервером |
| [`tools/agents/`](tools/agents) | Общие правила, проверки, git-хуки и навыки для агентов |

## Запуск сервера

Порядок установки Docker, создания первого администратора и настройки
бэкапов описан в [руководстве по развёртыванию](deploy/README.ru.md).
Готовые образы:

```
ssselery/zazzzor-api
ssselery/zazzzor-caddy
ssselery/zazzzor-backup
```

Образы доступны по тегам `sha-<commit>`, `main` и `latest`. Они также
публикуются в `ghcr.io/whoaskedssselery/`.

## Разработка

Понадобятся Go версии из `server/go.mod`, Node 22 с pnpm (`corepack enable`)
и Docker. Серверные тесты запускают настоящий Postgres.

### Сервер

```bash
cd server
go vet ./...
go test ./...
```

Локальный запуск с Postgres в Docker на порту 55432, чтобы не занимать 5432:

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

API слушает `:8080`, метрики Prometheus — `:9090`. Миграции применяются
при запуске. Проверка готовности — `curl http://127.0.0.1:8080/api/v1/readyz`.
Для проверки основных сценариев выполните
`go run ./cmd/zazzzor smoke http://127.0.0.1:8080`. Остальные настройки
описаны в `server/internal/config`.

### Веб-клиент

Из корня репозитория при работающем сервере:

```bash
pnpm install
pnpm dev        # http://localhost:5173
```

Vite пересылает `/api` и WebSocket шлюза на `http://127.0.0.1:8080`. Для
сервера нужны `ZAZZZOR_ALLOWED_ORIGINS=http://localhost:5173` и
`ZAZZZOR_COOKIE_SECURE=false`. Открывайте именно `http://localhost:5173`.

Проверки и генерация типов:

```bash
pnpm lint       # ESLint
pnpm test       # Vitest во всех пакетах
pnpm build      # проверка типов и сборка
pnpm gen:api    # обновление типов клиента из api/openapi.yaml
```

После изменений в `api/openapi.yaml` выполните `pnpm gen:api` и включите
`web/src/shared/api/schema.gen.ts` в коммит. CI сверяет его с контрактом.

### Браузерные тесты

Playwright проверяет собранный клиент в Chromium и Firefox с настоящими
API и LiveKit. Команды из `web/`:

```bash
pnpm exec playwright install chromium firefox   # один раз
pnpm e2e:up     # Postgres, API и LiveKit из web/e2e/compose.yml
pnpm e2e        # сборка, vite preview с CSP Caddy и все сценарии
pnpm e2e:down   # удаление тестового стека вместе с его базой
```

### Агенты для разработки

Общие настройки Claude Code и Codex включают [правила проекта](AGENTS.ru.md),
стиль кода, скрипт проверок, git-хуки и закреплённые версии навыков.
После клонирования:

```bash
bash tools/agents/setup.sh
```

Подробности в [описании инструментов](tools/agents/README.ru.md).

## Участие и безопасность

Как подготовить, проверить и отправить изменение, описано в
[правилах участия](CONTRIBUTING.ru.md). Об уязвимостях сообщайте закрыто,
как описано в [политике безопасности](SECURITY.ru.md), а не в публичной
задаче.

## Лицензия

[Apache License 2.0](LICENSE).
