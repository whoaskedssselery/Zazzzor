# Linux CI runners

[Русский](README.ru.md)

## What runs on these runners, and what never does

These runners, and the Windows runner of `CI_WINDOWS_RUNNER`, are not
ephemeral. The container, its `zz-work-N` and `zz-tmp-N` volumes, DinD,
the Go, pnpm and cargo caches and the Windows toolchain all stay between
jobs. Whatever one job leaves there (a changed binary, a cache entry, a
background process) runs in every later job on the same machine. Every job
here runs pull request code: Go tests, Vitest, Playwright, package
scripts, `build.rs`. So two kinds of job stay off them:

- **Pull requests from forks.** In `ci.yml`, a `pull_request` whose head
  repository is not this one runs on `ubuntu-latest` / `windows-latest`,
  which start clean and are thrown away after the job. Branches of this
  repository, `main` and manual runs use these runners. The required
  checks keep their names on either runner. The steps that differ (the
  Go and pnpm caches, the Playwright browsers) check `runner.environment`,
  so they follow the runner the job really got.
- **Release and publish jobs.** `images.yml` (it pushes `latest` and the
  release tags) and `desktop-release.yml` (it signs the desktop update,
  which installs quietly on every user's machine) run on
  `RELEASE_RUNNER` / `RELEASE_WINDOWS_RUNNER`, or on GitHub's runners when
  those variables are unset. They never use `CI_RUNNER` or
  `CI_WINDOWS_RUNNER`. If you set a release variable, it must name a
  machine that runs no other job. The buildx layer cache lives in
  GitHub's cache service under scopes that only `images.yml` on `main`
  and on release tags writes, never in a folder on the runner.

The signing key (`TAURI_SIGNING_PRIVATE_KEY`, `..._PASSWORD`) and
`DOCKERHUB_TOKEN` are secrets of the `release` and `images` environments.
Those environments wait for the owner's approval and accept only `main`
and the release tags. A job outside them cannot read the secrets.

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
