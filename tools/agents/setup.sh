#!/usr/bin/env bash
# Prepares a fresh clone for coding agents (Claude Code and Codex), in one
# command from anywhere in the clone:
#
#   bash tools/agents/setup.sh
#
# 1. The workspace skills declared in .claude/workspace.yaml, through the
#    claude-workspace CLI (its own core skills and the remote ones).
# 2. Every other skill pinned in skills-lock.json (Superpowers, Taste,
#    Impeccable, UI UX Pro Max), through the skills CLI, copied rather than
#    linked so they work on Windows too.
# 3. The same skills for Codex, which reads .agents/skills.
# 4. The repository's git hooks (tools/agents/hooks).
#
# Needs git, bash and Node 22 with npx; it is safe to run again. Pass
# --no-hooks to leave the git hooks as they are.
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

echo "==> workspace skills (.claude/workspace.yaml)"
npx --yes "$WORKSPACE_CLI" sync

echo "==> pinned skills (skills-lock.json)"
# One line per source: "<owner/repo> <skill> <skill>...", for the skills
# that are not installed yet.
# shellcheck disable=SC2016 # JavaScript, not shell
missing="$(node -e '
  const fs = require("fs");
  const lock = JSON.parse(fs.readFileSync("skills-lock.json", "utf8"));
  const by = {};
  for (const [name, s] of Object.entries(lock.skills)) {
    if (fs.existsSync(`.claude/skills/${name}/SKILL.md`)) continue;
    (by[s.source] ??= []).push(name);
  }
  for (const [source, names] of Object.entries(by)) console.log(source, ...names);
')"
if [[ -z "$missing" ]]; then
  echo "all installed"
fi
while read -r source names; do
  [[ -n "$source" ]] || continue
  # shellcheck disable=SC2086 # one skill name per word
  npx --yes "$SKILLS_CLI" add "$source" -s $names -a claude-code -a codex --copy -y
done <<<"$missing"

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
