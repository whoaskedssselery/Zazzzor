#!/usr/bin/env bash
# Prepares a fresh clone for coding agents (Claude Code and Codex), in one
# command from anywhere in the clone:
#
#   bash tools/agents/setup.sh
#
# 1. The skills pinned in skills-lock.json (Superpowers, Taste, Impeccable,
#    UI UX Pro Max), through the skills CLI, copied rather than linked so
#    they work on Windows too.
# 2. The workspace skills declared in .claude/workspace.yaml, through the
#    claude-workspace CLI (its core skills and the remote ones), which also
#    lists every skill in CLAUDE.md.
# 3. Whatever a flaky download left out, once more; then a check that every
#    declared skill is there.
# 4. The same skills for Codex, which reads .agents/skills.
# 5. The repository's git hooks (tools/agents/hooks).
#
# It restores, it does not upgrade: the committed CLAUDE.md and
# skills-lock.json are left as they were. Needs git, bash and Node 22 with
# npx; safe to run again. --no-hooks leaves the git hooks alone.
set -euo pipefail

WORKSPACE_CLI="claude-workspace@1.0.3"
SKILLS_CLI="skills@1.9.0"

hooks=1
for arg in "$@"; do
  case "$arg" in
    --no-hooks) hooks=0 ;;
    *) echo "usage: bash tools/agents/setup.sh [--no-hooks]" >&2; exit 2 ;;
  esac
done

root="$(git rev-parse --show-toplevel)"
cd "$root"
command -v npx >/dev/null || { echo "setup: npx not found; install Node 22 first" >&2; exit 1; }

# The CLIs rewrite these as they install; the committed versions are kept.
saved="$(mktemp -d)"
cp CLAUDE.md skills-lock.json "$saved/"
restore() {
  cp "$saved/CLAUDE.md" "$saved/skills-lock.json" "$root/"
  rm -rf "$saved"
}
trap restore EXIT

# missing [all] prints "<owner/repo> <skill>..." per source for the skills in
# skills-lock.json that are not installed; without "all" it leaves out the
# sources claude-workspace fetches itself (the "remote" list in
# .claude/workspace.yaml).
missing() {
  # shellcheck disable=SC2016 # JavaScript, not shell
  node -e '
    const fs = require("fs");
    const all = process.argv[1] === "all";
    const lock = JSON.parse(fs.readFileSync("skills-lock.json", "utf8"));
    const remote = new Set(
      [...fs.readFileSync(".claude/workspace.yaml", "utf8").matchAll(/^\s*-\s*[\w.-]+=(\S+)\s*$/gm)].map((m) => m[1]),
    );
    const by = {};
    for (const [name, s] of Object.entries(lock.skills)) {
      if (fs.existsSync(`.claude/skills/${name}/SKILL.md`)) continue;
      if (!all && remote.has(s.source)) continue;
      (by[s.source] ??= []).push(name);
    }
    for (const [source, names] of Object.entries(by)) console.log(source, ...names);
  ' "${1:-}"
}

# install reads missing's lines and installs each source's skills.
install() {
  local source names
  while read -r source names; do
    [[ -n "$source" ]] || continue
    # stdin is the list being read; npx must not consume it.
    # shellcheck disable=SC2086 # one skill name per word
    npx --yes "$SKILLS_CLI" add "$source" -s $names -a claude-code -a codex --copy -y </dev/null ||
      echo "setup: $source failed; it is retried below" >&2
  done
}

echo "==> pinned skills (skills-lock.json)"
missing | install

echo "==> workspace skills (.claude/workspace.yaml)"
npx --yes "$WORKSPACE_CLI" sync </dev/null || echo "setup: claude-workspace sync failed; retrying what is missing" >&2

left="$(missing all)"
if [[ -n "$left" ]]; then
  echo "==> retrying what did not download"
  install <<<"$left"
  npx --yes "$WORKSPACE_CLI" sync </dev/null || true
fi
left="$(missing all)"
if [[ -n "$left" ]]; then
  echo "setup: still missing (network?), run setup.sh again:" >&2
  echo "$left" >&2
  exit 1
fi

echo "==> skills for Codex (.agents/skills)"
mkdir -p .agents/skills
for dir in .claude/skills/*/; do
  name="$(basename "$dir")"
  [[ -e ".agents/skills/$name" ]] || cp -R "$dir" ".agents/skills/$name"
done

if ((hooks)); then
  echo "==> git hooks (tools/agents/hooks)"
  git config core.hooksPath tools/agents/hooks
fi

echo
echo "Done: $(find .claude/skills -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ') skills in .claude/skills."
echo "Before you push, run: bash tools/agents/check.sh"
