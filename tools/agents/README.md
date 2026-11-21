# Agent harness

[Русский](README.ru.md)

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

## Releasing the server

Server releases are tags `vX.Y.Z` on `main` ([semver](https://semver.org/);
the desktop app has its own `desktop-vX.Y.Z`, see
[`desktop/app/README.md`](../../desktop/app/README.md)). Changes land in
[`CHANGELOG.md`](../../CHANGELOG.md) and
[`CHANGELOG.ru.md`](../../CHANGELOG.ru.md) under **Unreleased** with the
pull request that makes them, in user-facing words.

1. In a pull request, rename **Unreleased** to `## [X.Y.Z] - YYYY-MM-DD`
   in both changelogs, start an empty **Unreleased** above it and update
   the links at the bottom. Check the notes with
   `bash tools/release/changelog-section.sh X.Y.Z`.
2. After the merge, wait for green CI on that `main` commit, then tag it
   (only the owner does this):

   ```bash
   git fetch origin && git tag -a vX.Y.Z origin/main -m "Zazzzor X.Y.Z" && git push origin vX.Y.Z
   ```

3. The Images workflow publishes the images as `X.Y.Z` and `X.Y` (never
   `latest`) and drafts a GitHub release from the changelog section.
4. Read the draft and publish it with **Set as the latest release**
   unticked: the desktop updater reads `latest.json` from the latest
   release, which must stay a desktop one.
