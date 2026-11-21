# AGENTS.md

[Русский](AGENTS.ru.md)

Shared rules for every coding agent in this repository (Codex reads this
file; Claude Code reads it through `CLAUDE.md`). People follow the same
rules. Also binding: [`tools/agents/STYLE.md`](tools/agents/STYLE.md), the
house style built from real review findings.

## Setup

After a clone, once: `bash tools/agents/setup.sh`. It installs the skills
(`.claude/skills` for Claude Code, `.agents/skills` for Codex) and the git
hooks. Details: [`tools/agents/README.md`](tools/agents/README.md).

## The project

Zazzzor is a self-hosted Discord alternative for a group of friends: one
home Linux server, Docker, no cloud services.

- `server/`: Go modular monolith (chi, pgx with sqlc, goose migrations,
  coder/websocket); tests use real Postgres through testcontainers.
- `api/openapi.yaml`: the REST and WebSocket contract, including the
  `x-gateway` section. It is the source of truth: a behaviour change
  updates the contract in the same commit.
- `web/`: React 19 client, Feature-Sliced Design (layers enforced by
  ESLint), TanStack Router and Query, Tailwind CSS; Biome formats it.
- `deploy/`: Compose and Swarm stacks, Caddy, the backup image, scripts and
  the server owner's guide in English and Russian.
- `desktop/`: the Windows desktop client in Rust (Tauri around the web
  client, native screen capture); each crate builds on Windows only.

## Navigating the code

If the CodeGraph MCP tools are available, use them first
(`codegraph_explore`, callers and impact before you change a symbol); fall
back to search and file reads for what the graph does not cover.

## Code rules

- Read the module you change before you write, and match it: naming,
  comment density, error wrapping (`fmt.Errorf("pkg: what: %w", err)`),
  sentinel errors with `ToProblem`, `db.InTx`, sqlc queries in
  `internal/<module>/queries.sql` (`go tool sqlc generate` from `server/`),
  goose migrations numbered after the last one.
- Reuse before you write (STYLE §1): extend what exists, never copy it.
- Doc comments describe behaviour in plain English; don't narrate changes.
- Test first: see the test fail for the right reason, then make it pass.
- No invisible or bidi characters in the source; build such strings from
  code points.
- No new dependencies without a reason; any version you add is exact and at
  least two weeks old.
- After a change to `api/openapi.yaml`, run `pnpm gen:api` and commit
  `web/src/shared/api/schema.gen.ts`.
- A change the server owner or users notice adds a line under
  **Unreleased** in `CHANGELOG.md` and `CHANGELOG.ru.md`; cutting a
  release is in [`tools/agents/README.md`](tools/agents/README.md#releasing-the-server).

## Checks

Before you push: `bash tools/agents/check.sh`. It runs only what applies
to the change (Go vet, lint, race tests and govulncheck; the contract; the
web client; deploy configs; the desktop crates' rustfmt, clippy and tests;
shellcheck; secrets; commit messages) and ends with a PASS/FAIL table. CI runs the same and more; ESLint for `web/` runs
with `pnpm lint`.

## Commits and branches

- Conventional Commits, imperative, lower case, at most 72 characters:
  `fix(server): …`, `feat(web): …`, `ci: …`. Atomic, logical commits; a
  `feat`/`fix`/`refactor`/`perf` commit over 40 lines has a body that says
  why; no commit over 600 lines.
- No `Co-authored-by`, "Generated with" or any AI attribution or tool name.
- Stage files explicitly; never `git add .` or `-A`.
- Work on a branch and open a pull request; never push to `main` directly.
- Never commit secrets, the installed skills (`.claude/skills`,
  `.agents/skills`; `setup.sh` downloads them) or local agent and editor
  state (`docs/`, `.superpowers/`, `.codegraph/`, caches, logs, personal
  settings, `.idea/`, `.vscode/`). The shared agent files are listed in
  [`tools/agents/README.md`](tools/agents/README.md); `check.sh` and the
  pre-commit hook enforce the list.

## Safety

- The development machine may run unrelated Docker containers. Touch only
  containers, volumes and networks you created; never run
  `docker system prune` or `docker volume prune`.
- Never print secrets. Diagnostics may show a key's name, length or
  fingerprint, never its value.
- Scratch output (logs, probes) stays out of the repository.

## Skills

Skills are installed by `tools/agents/setup.sh` and are not loaded into
context on their own: check their descriptions before matching work and use
every one that applies. Always: **commit-discipline** before a commit,
**health-review** for periodic audits, **codegraph** for navigation. For
UI work also **impeccable** and **ui-ux-pro-max**; for reviews
**code-reviewer** and **security-reviewer**.
