#!/usr/bin/env bash
# Every check a branch must pass before it is pushed, by a person or an
# agent.
#
# Usage, from anywhere in a clone (Git Bash on Windows, or any bash):
#   bash tools/agents/check.sh [base]
# base defaults to origin/main. The script works out what changed since
# the merge base and runs only the checks that apply: Go, contract, web,
# deploy, shell, commits and hygiene. It ends with a PASS/FAIL table and
# writes the full log to .check.log at the root of the clone. The exit code
# is 0 only if every check passed. While still iterating before a commit,
# ALLOW_DIRTY=1 skips the "everything committed" check; the final run must
# be without it.
# The checks are functions called through run (SC2317), and the commands
# given to bash -c are single-quoted on purpose (SC2016).
# shellcheck disable=SC2016,SC2317
set -uo pipefail

# hostpath turns a path into one Docker on this machine can mount: Git Bash
# on Windows needs the Windows form, everywhere else it is the path itself.
hostpath() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s\n' "$1"; fi
}

base_ref="${1:-origin/main}"
root="$(git rev-parse --show-toplevel)" || { echo "not inside a git worktree" >&2; exit 2; }
cd "$root" || exit 2
log="$root/.check.log"
: >"$log"
base="$(git merge-base "$base_ref" HEAD)" || { echo "no merge base with $base_ref" >&2; exit 2; }

# Lint, the race suite, the stack render and shellcheck all run in Docker;
# without it they would fail for the wrong reason and "test containers
# removed" would pass by seeing nothing.
if ! docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
  echo "Docker is not running; start Docker Desktop and run check.sh again" >&2
  exit 2
fi

declare -a names=() results=()
failed=0

# record NAME STATUS appends one row to the summary table.
record() {
  names+=("$1")
  results+=("$2")
  [[ "$2" == PASS || "$2" == SKIP ]] || failed=1
}

# run NAME CMD... runs one check, sends its output to the log and records
# PASS or FAIL from its exit code.
run() {
  local name="$1"
  shift
  echo "===== $name: $*" >>"$log"
  if "$@" >>"$log" 2>&1; then
    record "$name" PASS
  else
    record "$name" FAIL
  fi
}

changed="$(git diff --name-only "$base"...HEAD; git diff --name-only; git diff --name-only --cached)"
changed="$(sort -u <<<"$changed" | sed '/^$/d')"
has() { grep -qE "$1" <<<"$changed"; }

ci=".github/workflows/ci.yml"
ci_value() { grep -E "^\s+$1:" "$ci" | head -1 | sed -E 's/.*:\s*//'; }
gomodcache="$(go env GOMODCACHE)"
# The build and lint caches of the Linux containers below, kept between runs
# in a volume of their own (Windows paths would not hold Linux builds).
gocache="zazzzor-check-cache:/root/.cache"

# --- hygiene: files that must never be committed ---------------------------
# Agent and editor state stays local; the shared harness files are listed
# in .gitignore and tools/agents/README.md (skills are downloaded, never
# committed).
private_paths='^(docs/|\.superpowers/|\.codegraph/|\.agents/|\.idea/|\.vscode/)|^\.claude/(?!workspace\.yaml$|hide\.yaml$|launch\.json$|agents/)|^\.codex/(?!agents/|hooks\.json$)|^\.impeccable/(?!config\.json$)'
hygiene() {
  local bad=0
  if git diff --name-only "$base"...HEAD | grep -P "$private_paths"; then
    echo "^ these paths must never be committed"
    bad=1
  fi
  if git diff --name-only "$base"...HEAD -- '*.go' | grep -v '^server/'; then
    echo "^ Go code outside server/ is never vetted, linted or tested (STYLE §2)"
    bad=1
  fi
  # grep -P needs a UTF-8 locale; without one it errors and would read as
  # "nothing found". Status 2 is an error, not a pass.
  local rc=0
  git diff "$base"...HEAD |
    LC_ALL=C.UTF-8 grep -nP '^\+.*[\x{200B}-\x{200F}\x{202A}-\x{202E}\x{2066}-\x{2069}\x{FEFF}]' || rc=$?
  if [[ $rc -eq 0 ]]; then
    echo "^ invisible or bidi characters were added"
    bad=1
  elif [[ $rc -ne 1 ]]; then
    echo "the invisible-character check itself failed (grep -P, status $rc)"
    bad=1
  fi
  local f
  while IFS= read -r f; do
    [[ -n "$f" && -f "$f" ]] || continue
    if [[ "$(git ls-files -s -- "$f" | cut -c1-6)" != 100755 ]]; then
      echo "$f is not executable in git (git update-index --chmod=+x $f)"
      bad=1
    fi
  done < <(git diff --name-only --diff-filter=AM "$base"...HEAD -- '*.sh')
  if [[ "${ALLOW_DIRTY:-0}" != 1 && -n "$(git status --porcelain)" ]]; then
    echo "uncommitted changes (including untracked files):"
    git status --short
    bad=1
  fi
  return "$bad"
}
run hygiene hygiene

# --- commits ---------------------------------------------------------------
# Only this branch's own commits: merge commits carry GitHub's subjects, and
# what origin/main already has was checked when it merged, so an older base
# does not fail on history.
commits() {
  local bad=0 sha subject body lines merged
  merged="$(git rev-parse -q --verify origin/main)"
  for sha in $(git rev-list --reverse --no-merges "$base"..HEAD ${merged:+--not "$merged"}); do
    subject="$(git log -1 --format=%s "$sha")"
    body="$(git log -1 --format=%b "$sha")"
    lines="$(git show --numstat --format= "$sha" | awk '{s += $1 + $2} END {print s + 0}')"
    if ! grep -qE '^(feat|fix|refactor|perf|test|docs|build|ci|chore|style)(\([a-z0-9-]+\))?: [a-z].{0,70}$' <<<"$subject"; then
      echo "$sha: subject is not Conventional Commits, lower-case and at most 72 chars: $subject"
      bad=1
    fi
    if grep -qiE 'co-authored-by|generated (with|by)|\bcodex\b|\bclaude\b|\bopenai\b|\bai[- ]generated' <<<"$subject$body"; then
      echo "$sha: AI attribution or tool name in the message"
      bad=1
    fi
    if [[ "$subject" =~ ^(feat|fix|refactor|perf) && "$lines" -gt 40 && -z "${body//[[:space:]]/}" ]]; then
      echo "$sha: $lines changed lines and no body explaining why (STYLE §14)"
      bad=1
    fi
    if [[ "$lines" -gt 600 ]]; then
      echo "$sha: $lines changed lines; split it into logical commits (STYLE §14)"
      bad=1
    fi
  done
  return "$bad"
}
run commits commits

# --- secrets -------------------------------------------------------------------
# CI scans the history of every pushed branch with the repo's .gitleaks.toml,
# so a test credential that looks real fails every open PR. Scan the files
# this branch changed, the same rules, before anything is pushed. Test
# secrets use the allowed placeholder 0123456789abcdef0123456789abcdef.
secrets() {
  local files tmp
  mapfile -t files < <(git diff --name-only --diff-filter=AM "$base"...HEAD)
  ((${#files[@]})) || return 0
  tmp="$(mktemp -d)"
  for f in "${files[@]}"; do
    [[ -f "$f" ]] || continue
    mkdir -p "$tmp/$(dirname "$f")"
    cp "$f" "$tmp/$f"
  done
  MSYS_NO_PATHCONV=1 docker run --rm -v "$(hostpath "$tmp"):/scan" -v "$(hostpath "$PWD")/.gitleaks.toml:/cfg.toml" \
    "ghcr.io/gitleaks/gitleaks:v$(ci_value GITLEAKS_VERSION)" dir --redact --no-banner -c /cfg.toml /scan
  local rc=$?
  rm -rf "$tmp"
  return "$rc"
}
run "secrets (gitleaks)" secrets

# --- Go --------------------------------------------------------------------
if has '^server/'; then
  run gofmt bash -c 'cd server && out="$(gofmt -l .)"; [[ -z "$out" ]] || { echo "$out"; exit 1; }'
  run "go vet" bash -c 'cd server && go vet ./...'
  run "go mod tidy" bash -c 'cd server && go mod tidy -diff'
  run "sqlc diff" bash -c 'cd server && go tool sqlc diff'
  run golangci-lint env MSYS_NO_PATHCONV=1 docker run --rm -v "$(hostpath "$PWD"):/repo" \
    -v "$(hostpath "$gomodcache"):/go/pkg/mod" -v "$gocache" -w /repo/server \
    "golangci/golangci-lint:$(ci_value GOLANGCI_LINT_VERSION)" golangci-lint run ./...
  run "go test -race" env MSYS_NO_PATHCONV=1 docker run --rm -v "$(hostpath "$PWD"):/repo" \
    -v "$(hostpath "$gomodcache"):/go/pkg/mod" -v "$gocache" -w /repo/server \
    -v /var/run/docker.sock:/var/run/docker.sock -e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal \
    golang:1.27.4 go test -race -count=1 -p 4 -parallel 4 ./...
  run govulncheck env MSYS_NO_PATHCONV=1 docker run --rm -v "$(hostpath "$PWD"):/repo" \
    -v "$(hostpath "$gomodcache"):/go/pkg/mod" -v "$gocache" -w /repo/server \
    golang:1.27.4 go run "golang.org/x/vuln/cmd/govulncheck@$(ci_value GOVULNCHECK_VERSION)" ./...
  run "test containers removed" bash -c '[[ -z "$(docker ps -aq --filter label=zazzzor.test=1)" ]]'
else
  record "go" SKIP
fi

# --- contract ----------------------------------------------------------------
if has '^api/openapi\.yaml$'; then
  redocly="@redocly/cli@$(ci_value REDOCLY_CLI_VERSION)"
  contract() {
    local before after tmp
    tmp="$(mktemp)"
    git show "$base:api/openapi.yaml" >"$tmp"
    before="$(npx --yes "$redocly" lint "$tmp" 2>&1 | grep -oE 'You have [0-9]+ warning' | grep -oE '[0-9]+' || echo 0)"
    rm -f "$tmp"
    npx --yes "$redocly" lint api/openapi.yaml || return 1
    after="$(npx --yes "$redocly" lint api/openapi.yaml 2>&1 | grep -oE 'You have [0-9]+ warning' | grep -oE '[0-9]+' || echo 0)"
    echo "warnings: before $before, after $after"
    [[ "$after" -le "$before" ]]
  }
  run "openapi lint" contract
else
  record "openapi" SKIP
fi

# --- web contract ------------------------------------------------------------
# The web client consumes the contract: a schema change that compiles on the
# server can still break the client's types and runtime validators, which
# only surfaced at integration before (voice_states). Fix the contract or
# the client in the same branch.
if has '^api/openapi\.yaml$|^web/' && [[ -d web ]]; then
  run "web install" pnpm install --frozen-lockfile
  run "web contract fresh" bash -c 'pnpm gen:api && git diff --exit-code -- "$(git ls-files "web/**/schema.gen.ts")"'
  run "web typecheck" pnpm --filter @zazzzor/web exec tsc -b --pretty false
  run "web tests" pnpm --filter @zazzzor/web test
else
  record "web" SKIP
fi

# --- deploy ------------------------------------------------------------------
if has '^deploy/|^\.github/'; then
  run "compose config" docker compose --env-file deploy/.env.ci -f deploy/docker-compose.yml config --quiet
  run "stack config" bash -c 'out="$(ENV_FILE=deploy/.env.ci ZAZZZOR_API_TAG=ci ZAZZZOR_CADDY_TAG=ci bash deploy/scripts/stack.sh config)" && ! grep -n "\${" <<<"$out"'
  run "init-env" bash deploy/scripts/init-env-test.sh
else
  record "deploy" SKIP
fi

# --- desktop (Rust) --------------------------------------------------------------
# Each Cargo crate under desktop/, as CI's desktop job checks it. Its native
# code builds on Windows only. The MSVC tools of this machine failed with a
# temp folder under AppData, so the build keeps its own; LK_CUSTOM_WEBRTC,
# when set, saves downloading libwebrtc again.
#
# cargo audit reads only the lockfiles and needs no Rust toolchain. It is
# the prebuilt binary of the version CI pins, checked against CI's
# checksum and kept in the user's cache folder.
cargo_audit() {
  local version asset ext sum bin name dir lock bad=0
  version="$(ci_value CARGO_AUDIT_VERSION)"
  case "$(uname -s)" in
    MINGW* | MSYS* | CYGWIN*)
      asset=x86_64-pc-windows-msvc ext=zip bin=cargo-audit.exe
      sum="$(ci_value CARGO_AUDIT_SHA256_WINDOWS)"
      ;;
    Linux)
      asset=x86_64-unknown-linux-musl ext=tgz bin=cargo-audit
      sum="$(ci_value CARGO_AUDIT_SHA256_LINUX)"
      ;;
    *)
      echo "no prebuilt cargo-audit for $(uname -s) is pinned in $ci"
      return 1
      ;;
  esac
  name="cargo-audit-$asset-v$version"
  dir="${XDG_CACHE_HOME:-$HOME/.cache}/zazzzor-check"
  if [[ ! -x "$dir/$name/$bin" ]]; then
    mkdir -p "$dir" || return 1
    curl -fsSL -o "$dir/$name.$ext" \
      "https://github.com/rustsec/rustsec/releases/download/cargo-audit/v$version/$name.$ext" || return 1
    if ! echo "$sum  $dir/$name.$ext" | sha256sum --check --strict; then
      rm -f "$dir/$name.$ext"
      return 1
    fi
    if [[ "$ext" == zip ]]; then
      unzip -oq "$dir/$name.$ext" -d "$dir" || return 1
    else
      tar -xzf "$dir/$name.$ext" -C "$dir" || return 1
    fi
    rm -f "$dir/$name.$ext"
  fi
  for lock in desktop/*/Cargo.lock; do
    echo "== $lock"
    "$dir/$name/$bin" audit --file "$lock" || bad=1
  done
  return "$bad"
}

if has '^desktop/'; then
  run "desktop cargo audit" cargo_audit
  [[ -d "$HOME/.cargo/bin" ]] && PATH="$HOME/.cargo/bin:$PATH"
  if ! command -v cargo >/dev/null 2>&1; then
    echo "===== desktop: cargo not found, skipped" >>"$log"
    record "desktop (no cargo)" SKIP
  else
    for manifest in desktop/*/Cargo.toml; do
      crate="$(dirname "$manifest")"
      mkdir -p "$crate/target/tmp"
      tmpdir="$(hostpath "$root/$crate/target/tmp")"
      run "$crate fmt" bash -c "cd '$crate' && cargo fmt --check"
      # A shared CARGO_TARGET_DIR gets a folder per crate, as in CI.
      target="${CARGO_TARGET_DIR:+$CARGO_TARGET_DIR/$(basename "$crate")}"
      run "$crate clippy" env TMP="$tmpdir" TEMP="$tmpdir" ${target:+CARGO_TARGET_DIR="$target"} bash -c "cd '$crate' && cargo clippy --release --all-targets -- -D warnings"
      run "$crate test" env TMP="$tmpdir" TEMP="$tmpdir" ${target:+CARGO_TARGET_DIR="$target"} bash -c "cd '$crate' && cargo test --release"
    done
  fi
else
  record "desktop" SKIP
fi

# --- shell ---------------------------------------------------------------------
mapfile -t scripts < <(git diff --name-only --diff-filter=AM "$base"...HEAD -- '*.sh')
if ((${#scripts[@]})); then
  run shellcheck env MSYS_NO_PATHCONV=1 docker run --rm -v "$(hostpath "$PWD"):/mnt" -w /mnt \
    koalaman/shellcheck:v0.10.0 -x "${scripts[@]}"
else
  record "shellcheck" SKIP
fi

echo
printf '%-26s %s\n' CHECK RESULT
for i in "${!names[@]}"; do
  printf '%-26s %s\n' "${names[$i]}" "${results[$i]}"
done | tee -a "$log"
echo "full log: $log"
exit "$failed"
