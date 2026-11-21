# Contributing to Zazzzor

**English** · [Русский](CONTRIBUTING.ru.md)

Thanks for helping. This page is the short path from a clone to a merged
pull request. The rules themselves live in [`AGENTS.md`](AGENTS.md), the
house style in [`tools/agents/STYLE.md`](tools/agents/STYLE.md): people
follow them just as coding agents do. Issues and pull requests in Russian
are welcome too.

Found a vulnerability? Don't open an issue: see [SECURITY.md](SECURITY.md).

## Setting up

You need Go (the version in `server/go.mod`), Node 22 with pnpm
(`corepack enable`), Docker (the server's tests start a real Postgres) and,
for the desktop client, Windows with Rust and the MSVC build tools. After a
clone, once:

```bash
bash tools/agents/setup.sh
```

It installs the git hooks that check your commits, and the skills for
Claude Code and Codex. Details: [`tools/agents/README.md`](tools/agents/README.md).

## Running it locally

- **Server and web client:** the [Development](README.md#development)
  section of the README, from Postgres in Docker to `pnpm dev` and the
  Playwright scenarios.
- **Desktop client:** [`desktop/app/README.md`](desktop/app/README.md);
  the screen share is in [`desktop/share/README.md`](desktop/share/README.md).
- **A whole server with Docker:** [`deploy/README.md`](deploy/README.md).

## Checks

Before you push:

```bash
bash tools/agents/check.sh
pnpm lint
```

`check.sh` runs only what applies to your change (Go vet, lint, race tests
and govulncheck; the contract; the web client; deploy configs; the desktop
crates; shellcheck; secrets; commit messages) and ends with a PASS/FAIL
table. CI runs the same and more. Write the test first and see it fail for
the right reason before you make it pass.

## The API contract comes first

[`api/openapi.yaml`](api/openapi.yaml) is the source of truth for REST and
the WebSocket gateway (`x-gateway`). A change in behaviour updates the
contract in the same commit. Then regenerate the client's types and commit
them as well:

```bash
pnpm gen:api
```

CI compares `web/src/shared/api/schema.gen.ts` with the contract, and the
server's tests check every response against it.

## Texts and translations

Everything users read exists in English and Russian; English is the
source.

- **Web client:** `web/src/shared/i18n/locales/en/*.json`, with the same
  keys in `locales/ru/`.
- **Desktop client:** `desktop/app/src/lang.rs` (the tray, the update
  dialogs and the setup page).
- **Docs:** every `name.md` has a `name.ru.md` twin, and each links the
  other at the top.

A change to a text updates both languages in the same pull request. If you
don't write one of them, say so in the pull request and someone will fill
it in.

## Commits

- [Conventional Commits](https://www.conventionalcommits.org), imperative,
  lower case, at most 72 characters: `fix(server): …`, `feat(web): …`,
  `docs: …`.
- Atomic, logical commits. A `feat`, `fix`, `refactor` or `perf` commit
  over 40 lines has a body that says why; no commit is over 600 lines.
- No `Co-authored-by`, "Generated with" or any AI attribution or tool name.
- Stage files explicitly; never `git add .` or `git add -A`.
- Never commit secrets or local agent and editor state; `check.sh` and the
  pre-commit hook enforce the list.

## Branches and pull requests

1. Work on a branch; nothing is pushed to `main` directly.
2. Run the checks above, push and open a pull request against `main`. The
   template has a short checklist.
3. CI must be green and the pull request reviewed before it is merged.

Self-hosted CI runners are described in
[`tools/ci/runner/README.md`](tools/ci/runner/README.md).

## License

By contributing you agree that your work is licensed under the
[Apache License 2.0](LICENSE), like the rest of the project.
