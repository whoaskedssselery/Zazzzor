# Agent harness

What coding agents (Claude Code, Codex) share in this repository, and how a
fresh clone gets their skills back in one command.

## After a clone

```bash
bash tools/agents/setup.sh
```

Needs git, bash (Git Bash on Windows) and Node 22. It is safe to run again;
`--no-hooks` leaves the git hooks alone.

## What is shared

| File | What it is |
|---|---|
| [`AGENTS.md`](../../AGENTS.md) | The rules for every agent and person: project layout, code rules, checks, commits, safety. Codex reads it directly |
| [`CLAUDE.md`](../../CLAUDE.md) | Imports `AGENTS.md` for Claude Code; its `claude-workspace` block lists the skills and is rewritten by `setup.sh` |
| [`STYLE.md`](STYLE.md) | The house style: each rule comes from a real review finding |
| [`check.sh`](check.sh) | Every check that applies to a branch, with a PASS/FAIL table; run it before a push |
| [`hooks/`](hooks) | `pre-commit` and `commit-msg`, enabled by `setup.sh` (`core.hooksPath`) |
| [`.claude/workspace.yaml`](../../.claude/workspace.yaml) | The skill manifest of the [claude-workspace](https://www.npmjs.com/package/claude-workspace) CLI: its core skills and the remote ones |
| [`skills-lock.json`](../../skills-lock.json) | Every other skill, pinned to a source commit, for the [skills](https://www.npmjs.com/package/skills) CLI |
| `.claude/agents/`, `.codex/agents/` | Subagent definitions (the Impeccable design reviewers) for Claude Code and Codex |
| `.codex/hooks.json` | Codex hooks: the Impeccable design check after edits |
| `.claude/launch.json` | Dev servers the Claude Code preview starts (`web`, `web-e2e`) |
| `.claude/hide.yaml` | What `claude-workspace hide` stashes |
| `.impeccable/config.json` | Impeccable's detector settings for this project |

The skills themselves are not committed: `setup.sh` downloads them into
`.claude/skills` and `.agents/skills`. Personal settings
(`.claude/settings.local.json`, `.impeccable/config.local.json`), caches
and logs stay local too; `.gitignore` lists exactly what is shared.

## Adding a skill

```bash
npx --yes skills@1.9.0 add <owner/repo> -s <skill> -a claude-code -a codex --copy -y
```

It installs the skill and records it in `skills-lock.json`; commit that
file. A skill from the claude-workspace catalog goes through
`npx claude-workspace add <name>` instead, which records it in
`.claude/workspace.yaml`.
