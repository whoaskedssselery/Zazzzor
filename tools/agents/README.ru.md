# Инструменты для агентов

[English (основная версия)](README.md)

Общие файлы для помощников по программированию (Claude Code и Codex) и команда для установки их навыков после клонирования репозитория.

## После клонирования

```bash
bash tools/agents/setup.sh
```

Нужны Git, Bash (Git Bash на Windows) и Node 22. Скрипт можно запускать повторно. С параметром `--no-hooks` Git-хуки остаются в прежнем состоянии.

## Общие файлы

| Файл | Назначение |
|---|---|
| [`AGENTS.md` по-русски](../../AGENTS.ru.md) | Общие правила для агентов и людей. Описывает структуру проекта, соглашения по коду, проверки, коммиты и безопасность. Codex читает английскую версию напрямую |
| [`CLAUDE.md` по-русски](../../CLAUDE.ru.md) | Описывает подключение английского `AGENTS.md` в Claude Code. Блок `claude-workspace` в оригинале перечисляет навыки и обновляется через `setup.sh` |
| [`STYLE.md` по-русски](STYLE.ru.md) | Стиль проекта. Каждое правило выросло из замечания на ревью |
| [`check.sh`](check.sh) | Применимые к ветке проверки и таблица PASS/FAIL. Запускайте перед отправкой ветки |
| [`hooks/`](hooks) | Хуки `pre-commit` и `commit-msg`. Скрипт `setup.sh` включает их через `core.hooksPath` |
| [`.claude/workspace.yaml`](../../.claude/workspace.yaml) | Манифест CLI [claude-workspace](https://www.npmjs.com/package/claude-workspace) с базовыми и удалёнными навыками |
| [`skills-lock.json`](../../skills-lock.json) | Прочие навыки, привязанные к коммитам источников для CLI [skills](https://www.npmjs.com/package/skills) |
| `.claude/agents/`, `.codex/agents/` | Определения подагентов Impeccable для проверки дизайна в Claude Code и Codex |
| `.codex/hooks.json` | Хуки Codex, запускающие проверку дизайна Impeccable после изменений |
| `.claude/launch.json` | Серверы разработки, которые Claude Code запускает для предпросмотра (`web`, `web-e2e`) |
| `.claude/hide.yaml` | Список файлов, которые временно убирает `claude-workspace hide` |
| `.impeccable/config.json` | Настройки проверок Impeccable в этом проекте |

Сами навыки остаются вне коммитов. `setup.sh` скачивает их в `.claude/skills` и `.agents/skills`. Личные настройки (`.claude/settings.local.json`, `.impeccable/config.local.json`), кеши и журналы тоже остаются локальными. Правила хранения перечислены в `.gitignore`.

## Добавление навыка

```bash
npx --yes skills@1.9.0 add <owner/repo> -s <skill> -a claude-code -a codex --copy -y
```

Команда устанавливает навык и записывает его в `skills-lock.json`. Закоммитьте этот файл. Для навыка из каталога claude-workspace используйте `npx claude-workspace add <name>`. Эта команда записывает навык в `.claude/workspace.yaml`.

## Релиз сервера

Релизы сервера — теги `vX.Y.Z` на `main` ([semver](https://semver.org/lang/ru/));
у приложения для Windows свои теги `desktop-vX.Y.Z`, см.
[`desktop/app/README.ru.md`](../../desktop/app/README.ru.md). Изменения
попадают в [`CHANGELOG.md`](../../CHANGELOG.md) и
[`CHANGELOG.ru.md`](../../CHANGELOG.ru.md) в раздел **Unreleased** тем же
pull request, который их вносит, понятными пользователю словами.

1. В pull request переименуйте **Unreleased** в `## [X.Y.Z] - YYYY-MM-DD` в
   обоих журналах, добавьте над ним пустой **Unreleased** и обновите ссылки
   внизу. Проверьте заметки: `bash tools/release/changelog-section.sh X.Y.Z`.
2. После слияния дождитесь зелёного CI на этом коммите `main` и поставьте
   тег (это делает только владелец):

   ```bash
   git fetch origin && git tag -a vX.Y.Z origin/main -m "Zazzzor X.Y.Z" && git push origin vX.Y.Z
   ```

3. Workflow Images публикует образы с тегами `X.Y.Z` и `X.Y` (никогда не
   `latest`) и создаёт черновик релиза на GitHub из раздела журнала.
4. Прочитайте черновик и опубликуйте его, сняв галочку **Set as the latest
   release**: программа обновления приложения для Windows берёт `latest.json`
   из последнего релиза, и он должен оставаться релизом приложения.
