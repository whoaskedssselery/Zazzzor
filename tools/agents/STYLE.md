# House style for Zazzzor: the mistakes already made, and the house way

[Русский](STYLE.ru.md)

Every rule here comes from a real code review of this repository. When a review finds a new recurring mistake, it is added here. Before you commit, walk this list against your diff.

Most important first.

---

## 1. Reuse before you write

Before you add a helper, client, script or config loader, search for one that exists: `rg -n "func load_env|smokeSession|NewService\(" server deploy`. Extend it, or move it somewhere both callers can share. Never copy it.

- ❌ `deploy/scripts/env.sh`, a copy of `stack.sh`'s `load_env`.
- ❌ `deploy/tests/restore-data.go`, a second HTTP client next to the `smoke` command.
- ✅ The scripts run the Go code that already exists inside the image: `exec docker exec "$swarm_api" /zazzzor smoke "$base_url"`. Extend `smokeSession` in `server/cmd/zazzzor/smoke.go`. Share shell code by `source`-ing one file that both scripts use.

## 2. Go code lives in the module

Every Go entry point is a subcommand of `server/cmd/zazzzor` (`healthcheck`, `smoke`, `files …`). Scripts call the binary. Go files outside `server/` are never vetted, linted or tested.

## 3. Don't leak test hooks into the production API

- ❌ The unexported field `heartbeatInterval` in the exported `Options`, set through `export_test.go`, only so a test can scale time.
- ✅ Test what the code actually enforces, with the options it already has. The gateway enforces only `HeartbeatTimeout`, so tests set that. If time must be faked, the package takes a `Now func() time.Time` or a clock in its `Options`, the way `auth.Options`/`guilds.Options` take `Now`, defaulting to `time.Now`.

## 4. One constructor, `NewX(Options)`; defaults in one const block

- ❌ `NewDiskWithOptions` + `DiskOptions` next to `NewDisk`.
- ❌ `"192h"` written in five places.
- ✅ `guilds.NewService(guilds.Options{Pool: pool, IDs: ids, …})`. Zero fields take defaults inside `NewX`.
- ✅ Defaults are named constants in the package's `const` block, like `config.go`:
  ```go
  const (
  	DefaultHTTPAddr = ":8080"
  	DefaultDataDir  = "/data"
  )
  ```
  Every other place uses the constant: code, docs and `.env.example` (in docs, write the value once and say where it comes from).

## 5. No `panic` for invariants that only code controls

Options get defaults; they don't panic. Validate what comes from outside (env, requests) and return errors. A relationship between two constants is a test, not a runtime check.

## 6. Wrap every returned error as `pkg: what: %w`

- ❌ A bare `return err`, or `fmt.Errorf("failed")` without a verb.
- ✅ `return fmt.Errorf("files: create temp file: %w", err)`, from `files.go` `write`.

Sentinel errors are `var ErrX = errors.New("pkg: …")`, and callers check them with `errors.Is`.

## 7. Fail with the real cause

- ❌ `mkdir "$dir/.lock" 2>/dev/null || fail 'another backup … holds .lock'`. This reports a permission error and a stale lock as "another backup is running".
- ✅ Read the state, then name it with the fix, like `stack.sh` `require_swarm`:
  ```bash
  die "Docker не в режиме Swarm (состояние: ${state:-неизвестно}). Включите его: docker swarm init"
  ```
  Never send stderr to `/dev/null` on a step that can fail for more than one reason.

## 8. Anything that runs unattended must be able to recover and must report its health

A scheduled job:
- survives its own crash: use `flock`, never a lock directory;
- checks its preconditions (directory writable, free space) and names what's wrong;
- exposes freshness that a healthcheck can read, for example the age of the newest backup.

Nothing may stop forever without a signal.

## 9. Doc comments describe behaviour, on every non-trivial identifier

- Every exported identifier and every non-trivial unexported function has a doc comment. The comment says what it does and why, in plain English.
- Keep the package doc (`doc.go` or the package comment) in sync with the layout.
- ✅ From `files.go`:
  ```go
  // syncDir makes the rename durable where the platform allows it (Unix);
  // failure only weakens durability after a crash, so it is ignored.
  ```
- Don't narrate changes ("now we…", "added…"). Match the surrounding comment density; don't add more than it.

## 10. Tests: external package, shared helpers, `got = …, want …`, one behaviour per test

- Use the `pkg_test` package unless you really need internals. If you do, name the file `*_internal_test.go`.
- Reuse the package's helpers (`envFrom` in `config_test.go`, `newDisk` in `files_test.go`, `newEnv` in gateway tests). Helpers call `t.Helper()`.
- Messages follow `X = %v, want %v`. Fatal errors carry context: `t.Fatalf("Load: %v", err)`, never a bare `t.Fatal(err)`.
- Each test covers one behaviour, and its name says which (`TestReconcileKeepsReferencedFiles`, not `TestReconcile`).
- ❌ Tests that compare a constant with its literal (`HeartbeatInterval != 30*time.Second`). Test behaviour instead: the default `New` applies, or what the timeout allows.
- Time: inject the clock; avoid `time.Sleep` when you can. Where real time is unavoidable (the gateway), scale it down and keep the margins wide.
- Write the failing test first and see it fail for the right reason before you make it pass.

## 11. Lint suppressions sit on their own line, with that line's reason

✅ `//nolint:gosec // G304: dir is built from validated components.` goes directly on the `os.Open(dir)` it excuses. Never move a suppression onto an unrelated line to silence a new finding. Fix the finding instead.

## 12. Shell scripts look like `deploy/scripts/stack.sh`

- `#!/usr/bin/env bash` and `set -euo pipefail`. Use bash, not POSIX sh, unless the script runs inside a busybox/alpine image. If it does, say so in the header and test it there.
- Write an English header that explains the script and a `Usage` block that lists every command and variable. Localized help may supplement the English help when the existing output contract is preserved. Maintain operator guides in both languages.
- One statement per line. A why-comment over each function and each non-obvious step.
- Errors give the operator a concrete fix and go through `die` (exit 2). Use English for new messages. Preserve existing diagnostics unless the task explicitly changes them.
- Quote every expansion. Arrays for argument lists.
- Make it executable in git: `git update-index --chmod=+x path`, and check with `git ls-files -s path`; it must show 100755.
- Run `shellcheck -x` clean (the check script does this).

## 13. Keep owner-facing docs short, numbered and bilingual

Maintain the [primary English guide](../../deploy/README.md) and the [Russian translation](../../deploy/README.ru.md) together. Use short sentences, numbered steps and `bash` blocks for commands. Put a warning before any destructive action. Declare each new variable once in the shared `.env.example` and give it a one-line comment in each language, English first.

## 14. Commits: one logical change each, with a body when the why isn't obvious

- ❌ 2 commits for 4 logical changes. ❌ An 870-line commit mixing deploy, CI, scripts and docs. ❌ `fix(server): …` with no body for a design change.
- ✅ `b0c6518`:
  ```
  fix(server): make sending a message safe to retry

  Create and Edit read the author and the replied-to message before the
  write, so nothing can fail between storing a message and announcing it.
  ```
- The body explains *why* and states any decision a reviewer would otherwise have to dig out. Wrap it at 72 columns.
- If the task suggests a commit split, follow it, or say in the pull request why not.

## 15. Restore and upgrade paths are part of the feature

For anything that changes data (backups, migrations, reconcile), ask:
- What happens if it runs against a newer or older schema?
- What happens if the app is running at the same time?
- What happens if one item is bad?

One bad item must not abort the whole run; log it, skip it, report it. Never destroy data that might still be referenced: use a grace window, keyed on the *run's start time*.

## 16. Optional settings in `.env.example` are commented out

`load_env` keeps the **first** value of a key, whereas compose keeps the last. An active line in the example therefore shadows anything the owner appends later. New switches, and variables that only one deployment variant uses, go in commented out with their default. Tell the reader to uncomment and edit the line.
- ❌ `ZAZZZOR_EDGE=caddy` and `TRAEFIK_SUBNET=` as active lines (C3).
- ✅ The existing pattern:
  ```
  # How long deleted images stay in the trash. Use more than one day (192h = 8 days).
  # Сколько удалённые картинки лежат в корзине; больше суток (192h = 8 суток).
  # ZAZZZOR_FILE_RETENTION=192h
  ```

## 17. A migration from what runs today is part of the deploy feature

When a change replaces something the owner already runs, the docs give the **old → new mapping** of every variable, secret and volume. Existing secrets, such as the DB password already stored in the volume, are reused and never regenerated. Prefer designs that update in place with `stack deploy` over procedures that need `stack rm`, downtime or manual backups: for example, a new network name instead of changing an existing network's properties.

## 18. Diagnose a lint finding before suppressing it

A suppression's reason must name the real cause, shown by a reproduction. "It appeared when X was linked" is not a cause.
- ❌ `//nolint:contextcheck` on `m.NewGateway(...)` "because the LiveKit SDK was linked" (C4). The real cause was older code: `hub.dispatch` derived a context from the `g.ctx` field without taking ctx as a parameter.
- ✅ Thread the context through as a parameter (`dispatchLoop(ctx)`, `consume(ctx, …)`), and the finding disappears.

## 19. Idempotency keys are recorded only after the work succeeds

A dedupe that records the key first turns the sender's retry of a failure into a silent no-op. Mark the key after success, or forget it on error. Test a retry that follows a 500.

## 20. Every concurrency guard gets a test that breaks without it

Each revision check, SID or generation check, and "skip if rejoined" check needs one test that interleaves the race through a blocking fake. RED is produced by deleting the guard.

## 21. A rejected commit leaves its files staged

When a hook rejects a commit, the staged files stay staged, and the next `git commit` takes them along into an unrelated commit. After any rejected commit, run `git status` and fix the message before you stage anything else. Never chain `git commit … && git add … && git commit …` without checking that the first commit really happened.

## 22. Test credentials use the allowed placeholder

A test that needs a secret uses `0123456789abcdef0123456789abcdef`. That is the only value allowed in `.gitleaks.toml`, and it is 32 characters, as LiveKit requires. Any other realistic-looking key fails the secret scan, and once the branch is pushed it fails CI on every open PR, not just yours.

## 23. An inverse request cancels a pending effect

When an operation schedules a deferred effect (a move fallback, a removal), the inverse request cancels it: moving back, deleting the destination, losing the permission. A request answered "no-op, 204" must not leave an earlier pending effect running. Test it this way: issue the request and its inverse, advance the clock past the deadline, and expect nothing.

## 24. Pin narrowly; don't revoke global state to build a pin

To protect one pinned session, guard the exception (a `s.moves` check). Never zero user-wide state such as the token intent: the pin can then never be released cleanly.

## 25. Prove §20 by deleting each guard

Before handing over, delete each concurrency or authorisation guard in a scratch copy and run the package. A guard that survives either gets a test, or is deleted as redundant. If it stays as defence in depth, give it a comment that says so.

## 26. Every "act on another member" endpoint applies the hierarchy rule

Spec §5 says you may act on a member only if your highest role is above theirs, and the owner may act on anyone. Reuse `permissions.CanActOn` alongside the permission check, and answer `403 role_hierarchy` the way kick and ban do.

## 27. Retries of security-relevant removals outlast the safety net

A capped retry that gives up before the periodic reconcile leaves a removed member connected in the gap. Keep retrying with capped backoff, or trigger the safety net early.

## 28. Claim panic safety only where the unlocks are deferred

If a test claims that a panic releases ownership, every lock taken on that path must be released by `defer`. If that isn't true, state the limit.
