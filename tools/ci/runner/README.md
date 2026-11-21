# Linux CI runners

[Русский](README.ru.md)

1. Build the image from the repository root. The build installs the
   Chromium and Firefox system dependencies and reads the Playwright
   version from `pnpm-lock.yaml`. Rebuild the image after updating
   Playwright.

   ```bash
   docker build -f tools/ci/runner/Dockerfile -t zazzzor-runner:playwright .
   ```

2. CI installs the browsers without apt into
   `/var/lib/zz-runner/playwright/<Playwright version>`. The existing
   volumes `zz-work-1`, `zz-work-2`, and `zz-work-3` persist them
   across jobs and container replacements. GitHub-hosted runners
   continue to use `--with-deps`. If the runner image is old or was
   built for a different Playwright version, the step warns that a
   rebuild is due and falls back to installing system packages with
   apt. CI takes longer on that fallback.

3. Wait for the selected runner to finish its job before replacing it.
   Check Actions → Runners and the runner log. Only stop an idle
   runner. The commands below replace the selected `zazzzor-runner-N`,
   preserve its work volumes, and reuse its existing `zz-dind-N` container.
   You need `gh` with runner management access and a fresh registration
   token.

   ```bash
   i=1 # Then repeat separately for 2 and 3, checking each runner is idle.
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

4. Run CI and check the browser installation step and both browser
   projects. The first run downloads the browsers. Later runs use the
   work volume. Keep the `zz-work-*` and `zz-tmp-*` volumes and the DinD
   containers in place during runner replacement.

5. To roll back, wait for the runner to become idle and repeat step 3
   with the previous `zazzzor-runner:local` image. The existing workflow
   works with either image.

The original Dockerfile and `start.sh` lived in a temporary local build
directory. This directory now keeps them alongside the Playwright
dependency installation. Apply the replacement commands above to update
an existing runner.
