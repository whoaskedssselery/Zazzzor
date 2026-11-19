#!/usr/bin/env bash
# Запускает Linux-раннер; рабочий том хранит кэши между заданиями.
# Использование: docker run с REPO_URL, RUNNER_TOKEN, RUNNER_NAME, RUNNER_WORK.
set -euo pipefail

cd /home/runner
if [[ ! -f .runner ]]; then
  ./config.sh --unattended --replace --url "$REPO_URL" --token "$RUNNER_TOKEN" \
    --name "$RUNNER_NAME" --labels zazzzor --work "$RUNNER_WORK"
fi
unset RUNNER_TOKEN
exec ./run.sh
