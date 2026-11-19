# Linux-раннеры CI

1. Из корня репозитория соберите образ. Системные зависимости Chromium и
   Firefox устанавливаются при сборке, версия Playwright берётся из
   `pnpm-lock.yaml`. После обновления Playwright пересоберите образ.

   ```bash
   docker build -f tools/ci/runner/Dockerfile -t zazzzor-runner:playwright .
   ```

2. В CI браузеры ставятся без apt в
   `/var/lib/zz-runner/playwright/<версия Playwright>`. Существующие тома
   `zz-work-1`, `zz-work-2`, `zz-work-3` сохраняют их между заданиями и
   пересозданиями контейнеров. GitHub-hosted раннеры продолжают использовать
   `--with-deps`. Если образ собран под другую версию Playwright (или старый),
   шаг предупреждает, что его пора пересобрать, и ставит системные пакеты
   через apt, как раньше: CI не ломается, только медленнее.

3. Перед заменой дождитесь завершения задания нужного раннера. Проверьте
   вкладку Actions → Runners и его журнал; не останавливайте занятой раннер.
   Команды ниже заменяют только выбранный `zazzzor-runner-N`, сохраняют его
   рабочие тома и используют существующий `zz-dind-N`. Нужны `gh` с доступом
   к управлению раннерами и новый регистрационный токен.

   ```bash
   i=1 # затем отдельно 2 и 3, каждый раз проверяя, что раннер свободен
   repo=whoaskedssselery/Zazzzor
   token="$(gh api --method POST "repos/$repo/actions/runners/registration-token" --jq .token)"
   docker stop "zazzzor-runner-$i"
   docker rm "zazzzor-runner-$i"
   docker run -d --name "zazzzor-runner-$i" --restart unless-stopped \
     --network "container:zz-dind-$i" \
     -v "zz-work-$i:/var/lib/zz-runner" -v "zz-tmp-$i:/tmp" \
     -e REPO_URL="https://github.com/$repo" -e RUNNER_NAME="local-$i" \
     -e RUNNER_WORK="/var/lib/zz-runner/local-$i" -e RUNNER_TOKEN="$token" \
     -e DOCKER_HOST=tcp://127.0.0.1:2375 zazzzor-runner:playwright
   unset token
   docker logs --tail 30 "zazzzor-runner-$i"
   ```

4. Запустите CI и проверьте шаг браузеров и оба браузерных проекта.
   Первый прогон скачивает браузеры, следующие используют рабочий том.
   Не удаляйте `zz-work-*`, `zz-tmp-*` или контейнеры dind при замене раннера.

5. Для отката дождитесь освобождения раннера и повторите шаг 3 с прежним
   образом `zazzzor-runner:local`; workflow менять не нужно.

Исходный Dockerfile и `start.sh` находились во временной папке локальной
сборки. Эта папка в репозитории хранит их вместе с установкой зависимостей
Playwright; команды не меняют существующие контейнеры автоматически.
