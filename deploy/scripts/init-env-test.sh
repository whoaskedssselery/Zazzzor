#!/usr/bin/env bash
# Tests deploy/scripts/init-env.sh in a temporary directory: the
# non-interactive and interactive modes, the settings it must fill, file
# mode 600, refusing to overwrite without --force, and that no secret value
# is ever printed. When Docker is available, the written file must also pass
# `docker compose config` for deploy/docker-compose.yml.
#
# Usage: bash deploy/scripts/init-env-test.sh
# Prints the name of each check and "init-env tests: OK" at the end.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
init="$script_dir/init-env.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# A placeholder of the same shape as deploy/.env.ci uses.
token=00000000-0000-0000-0000-000000000000
required=(ZAZZZOR_DOMAIN DUCKDNS_SUBDOMAIN DUCKDNS_TOKEN ACME_EMAIL ZAZZZOR_ADMIN_USERNAMES
  LIVEKIT_API_KEY LIVEKIT_API_SECRET POSTGRES_PASSWORD ZAZZZOR_HTTPS_PORT LIVEKIT_TCP_PORT
  LIVEKIT_UDP_PORT ZAZZZOR_BACKUP_DIR LIVEKIT_VERSION CADDY_VERSION POSTGRES_VERSION)

# value FILE KEY prints the value of KEY in FILE.
value() {
  grep -E "^$2=" "$1" | tail -n 1 | cut -d= -f2-
}

# check_env FILE asserts every required key is set exactly once and the
# generated secrets have their lengths.
check_env() {
  local file="$1" key v
  for key in "${required[@]}"; do
    [[ "$(grep -cE "^$key=" "$file")" -eq 1 ]] || fail "$key is not set exactly once in $file"
    v="$(value "$file" "$key")"
    [[ -n "$v" ]] || fail "$key is empty in $file"
  done
  [[ "$(value "$file" LIVEKIT_API_KEY)" =~ ^[0-9a-f]{32}$ ]] || fail "LIVEKIT_API_KEY is not 32 hex characters"
  [[ "$(value "$file" LIVEKIT_API_SECRET)" =~ ^[0-9a-f]{64}$ ]] || fail "LIVEKIT_API_SECRET is not 64 hex characters"
  [[ "$(value "$file" POSTGRES_PASSWORD)" =~ ^[0-9a-f]{48}$ ]] || fail "POSTGRES_PASSWORD is not 48 hex characters"
  # Git Bash on Windows has no real file modes.
  if [[ "$(uname -s)" != MINGW* && "$(uname -s)" != MSYS* ]]; then
    [[ "$(stat -c %a "$file")" == 600 ]] || fail "$file has mode $(stat -c %a "$file"), not 600"
  fi
}

# check_quiet FILE LOG asserts no secret value from FILE appears in LOG.
check_quiet() {
  local key v
  for key in DUCKDNS_TOKEN LIVEKIT_API_KEY LIVEKIT_API_SECRET POSTGRES_PASSWORD; do
    v="$(value "$1" "$key")"
    if grep -qF -- "$v" "$2"; then fail "the value of $key was printed"; fi
  done
}

echo "non-interactive run writes every setting"
env1="$test_root/one/.env"
mkdir -p "$test_root/one"
DUCKDNS_TOKEN="$token" bash "$init" --yes --lang en --domain Zazzzor.DuckDNS.org \
  --email ivan@example.org --admin ivan --output "$env1" >"$test_root/one.log" 2>&1 ||
  fail "non-interactive run failed: $(cat "$test_root/one.log")"
check_env "$env1"
check_quiet "$env1" "$test_root/one.log"
[[ "$(value "$env1" ZAZZZOR_DOMAIN)" == zazzzor.duckdns.org ]] || fail "domain not normalised"
[[ "$(value "$env1" DUCKDNS_SUBDOMAIN)" == zazzzor ]] || fail "DUCKDNS_SUBDOMAIN not derived"
[[ "$(value "$env1" DUCKDNS_TOKEN)" == "$token" ]] || fail "DUCKDNS_TOKEN not written"
grep -q 'docker compose -f deploy/docker-compose.yml up -d' "$test_root/one.log" || fail "next steps not printed"
grep -q 'create-admin ivan' "$test_root/one.log" || fail "create-admin step not printed"
[[ -z "$(find "$test_root/one" -name '.env.init.*')" ]] || fail "temporary file left behind"

echo "an existing file is kept without --force"
before="$(cat "$env1")"
if DUCKDNS_TOKEN="$token" bash "$init" --yes --domain zazzzor.duckdns.org --email ivan@example.org \
  --admin ivan --output "$env1" >"$test_root/two.log" 2>&1; then
  fail "overwrote an existing file without --force"
fi
grep -q -- '--force' "$test_root/two.log" || fail "the refusal does not mention --force"
[[ "$(cat "$env1")" == "$before" ]] || fail "the existing file changed"

echo "--force replaces the file and keeps a copy"
DUCKDNS_TOKEN="$token" bash "$init" --yes --force --domain zazzzor.duckdns.org --email ivan@example.org \
  --admin ivan --output "$env1" >"$test_root/three.log" 2>&1 || fail "--force run failed: $(cat "$test_root/three.log")"
check_env "$env1"
[[ "$(value "$env1" POSTGRES_PASSWORD)" != "$(grep -E '^POSTGRES_PASSWORD=' <<<"$before" | cut -d= -f2)" ]] ||
  fail "--force kept the old secrets"
copies=("$env1".bak.*)
[[ -f "${copies[0]}" ]] || fail "no copy of the replaced file"
[[ "$(cat "${copies[0]}")" == "$before" ]] || fail "the copy differs from the replaced file"

echo "--yes fails on a missing setting and writes nothing"
env4="$test_root/four/.env"
mkdir -p "$test_root/four"
if env -u DUCKDNS_TOKEN bash "$init" --yes --domain zazzzor.duckdns.org --email ivan@example.org \
  --output "$env4" >"$test_root/four.log" 2>&1; then
  fail "--yes without --admin and a token succeeded"
fi
grep -q -- '--admin' "$test_root/four.log" || fail "the error does not name --admin"
[[ ! -e "$env4" ]] || fail "a file was written despite the error"

echo "invalid values are rejected"
for bad in "--domain example.com" "--email not-an-email" "--admin x" "--duckdns-token nope"; do
  # shellcheck disable=SC2086 # Each case is a flag and its value.
  if DUCKDNS_TOKEN="$token" bash "$init" --yes --domain zazzzor.duckdns.org --email ivan@example.org \
    --admin ivan $bad --output "$env4" >/dev/null 2>&1; then
    fail "accepted $bad"
  fi
  [[ ! -e "$env4" ]] || fail "a file was written for $bad"
done

echo "interactive run in Russian asks again after a bad answer"
env5="$test_root/five/.env"
mkdir -p "$test_root/five"
printf '%s\n' example.com https://friends.duckdns.org/ "$token" ivan@example.org ivan y |
  env -u DUCKDNS_TOKEN LANG=ru_RU.UTF-8 bash "$init" --output "$env5" >"$test_root/five.log" 2>&1 ||
  fail "interactive run failed: $(cat "$test_root/five.log")"
check_env "$env5"
check_quiet "$env5" "$test_root/five.log"
[[ "$(value "$env5" ZAZZZOR_DOMAIN)" == friends.duckdns.org ]] || fail "interactive domain not written"
grep -q 'Домен DuckDNS' "$test_root/five.log" || fail "LANG=ru did not select Russian prompts"
grep -q 'поддомен>.duckdns.org' "$test_root/five.log" || fail "a bad domain was not explained"

echo "answering no writes nothing"
env6="$test_root/six/.env"
mkdir -p "$test_root/six"
printf '%s\n' zazzzor.duckdns.org "$token" ivan@example.org ivan n |
  env -u DUCKDNS_TOKEN bash "$init" --lang en --output "$env6" >/dev/null 2>&1 && fail "answering n succeeded"
[[ ! -e "$env6" ]] || fail "a file was written after answering n"
[[ -z "$(find "$test_root/six" -name '.env.init.*')" ]] || fail "temporary file left behind"

if docker compose version >/dev/null 2>&1; then
  echo "docker compose accepts the written file"
  docker compose --env-file "$env1" -f "$script_dir/../docker-compose.yml" config --quiet ||
    fail "docker compose config rejected the written file"
else
  echo "docker compose not found, compose check skipped"
fi

echo "init-env tests: OK"
