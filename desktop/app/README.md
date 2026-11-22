# Zazzzor for Windows

**English** · [Русский](README.ru.md)

The desktop client's shell: a Tauri 2 app with one window that opens the
instance's own web client, the way Discord's desktop app opens discord.com,
and a tray icon. There is no bundled copy of the UI: whatever the server
serves is what the app shows, so the app never falls behind the server.

## Build and run

Windows 10/11 x64 with the MSVC build tools, the Windows SDK and the
WebView2 runtime (part of Windows 11).

```bash
cargo build --release
target/release/zazzzor-desktop.exe
```

`cargo run` works too (a debug build keeps the console window). No Tauri
CLI or Node is needed: `setup/` is plain HTML, CSS and JS. The screen
share links the prebuilt libwebrtc, which `webrtc-sys` downloads at build
time (~110 MB). Set `LK_CUSTOM_WEBRTC` to a local copy if needed.
See the [screen share README](../share/README.md). The app also links the
C runtime statically ([.cargo/config.toml](.cargo/config.toml)).

On the first start the window asks for the server's address
("Where is your server?"): the address Zazzzor opens at in a browser. A
bare host means `https://`; a path is dropped. For the local e2e stack
(`pnpm e2e:up` and the `web-e2e` dev server, see [web/e2e/compose.yml](../../web/e2e/compose.yml))
it is `http://localhost:4173`. Plain `http://` is accepted only for
`localhost` and loopback addresses; any other server needs `https://`.

Where things are kept, per Windows user:

- `%APPDATA%\app.zazzzor.desktop\settings.json`: the server's address and
  `.window-state.json`: the window's size, position and maximised state.
- `%LOCALAPPDATA%\app.zazzzor.desktop\EBWebView`: the WebView2 profile
  (the session cookie, the web client's settings, granted permissions).

### A quirk of the owner's PC

A binary started from under `WebstormProjects\projects\Zazzzor` gets
"access denied" (os error 5) at start-up, and Windows Graphics Capture
refused the capture spike there for the same reason. The folder carries an
inherited sandbox ACL (`CodexSandboxUsers`, a capability SID) and a
mandatory integrity label with no-write-up, so programs started from it run
restricted. A copy of the same `.exe` anywhere else (for example
`%USERPROFILE%\zazzzor`) works. Nothing in the app causes it; changing the
folder's labels is the owner's call.

## What it does

- **The instance in a window.** The web client runs as in Chrome: sign-in
  (the session cookie persists across restarts), the gateway WebSocket,
  voice through LiveKit, notifications and the browser's own screen share
  (`getDisplayMedia` opens WebView2's picker; the web client uses the
  native share below instead).
- **Permissions.** The chosen server gets the microphone, the camera and
  notifications without WebView2's prompt, as a browser remembers them;
  any other page, and any other permission, gets WebView2's default.
- **Its own pages only.** Pages of the server stay in the window; links to
  other sites, `target=_blank` and `window.open` open in the default
  browser. The setup commands (`current_instance`, `set_instance`,
  `open_instance`) answer only the bundled setup page, which the server's
  pages cannot navigate to.
- **The [native screen share](../share/README.md).** Share a screen or a
  window at up to 1080p60 on the GPU's encoder, with one application's
  sound (or the whole system's but the app's own, so the call is not sent back). The
  server's pages, and only the chosen server's, may call five share commands:
  `share_sources`, `share_thumbnail`, `share_start`, `share_stop` and
  `share_status`. A capability for the server's origin is added at run
  time (an ACL manifest in `build.rs` makes every command need a
  permission), and each command checks the calling page again. The page
  asks its server for the companion token (`POST
  /channels/{id}/voice/screen`) and hands it over; the app joins only the
  LiveKit of the same host. The share stops when the app quits. The
  page sees the windows and their thumbnails only while the app's window
  is shown: sources are listed at most every two seconds, and each listed
  source gives one thumbnail within a minute. A share starts from the
  page's own picker, with no second question from the app.
- **Push-to-talk everywhere.** In a call with push-to-talk the page asks
  the app to watch its key (`hotkeys_ptt`); a low-level keyboard hook
  reports it while the window is behind, so it works in a game. The hook
  only looks (the key still reaches the program in front) and runs only
  while a key is asked for.
- **Mute and deafen combinations everywhere.** The page asks the app to
  watch the combinations saved in the voice settings (`hotkeys_toggles`,
  e.g. "Control+Shift+KeyM"); the same hook reports each press while the
  window is behind, and a held combination flips once.
- **Start with Windows.** "Start with Windows" in the tray menu
  writes the app under the user's Run key with `--autostart`; started so,
  the app stays in the tray.
- **Tray.** Closing the window hides it to the tray; a left click on the
  icon or "Open Zazzzor" brings it back. "Change server…" opens the setup
  page with the current address and a "Back to the current server" button.
  "Stop sharing" is available during an active share, which the tooltip
  also indicates. "Quit" exits the app.
- **One instance.** A second launch brings the running window to the front
  and exits.
- **Window state.** Size, position and the maximised state are restored on
  the next start (saved when the window hides and when the app quits).
- **Language.** Setup, tray menus, and update dialogs follow the web client's
  language. Until the web client first reports its language, the app uses
  Russian if Windows is set to Russian, and English otherwise.

## Checked

On the owner's PC against the local e2e stack, driving the WebView2 over
its DevTools protocol (`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=
--remote-debugging-port=9222`) and the window through Win32:

- first run shows the setup page; `ftp://x` is refused with a message;
  `http://localhost:4173` is saved and opened;
- the server's page calling `set_instance` or `current_instance` is
  refused, and it cannot navigate to the setup page or to another origin;
- sign-in, the channel list, "Online" (gateway connected) and "Voice
  connected" (LiveKit) in a voice channel; after a restart the session is
  still signed in;
- `Notification.requestPermission()` and `getUserMedia({audio})` are
  granted without a prompt; a notification fires `show`;
- `getDisplayMedia` waits on WebView2's picker (the picker itself was not
  driven);
- closing hides the window and keeps the process; a second launch exits
  and brings the first window to the front; position, size and maximised
  state survive a restart.

Not checked by hand yet: the tray menu clicks themselves (the code paths
behind them were exercised), a server over https with a real certificate,
and how a notification looks in the Windows notification centre.

## Releases

Desktop versions are independent of server versions. A `desktop-vX.Y.Z` tag
builds a per-user NSIS installer with English first and Russian also available.
WebView2 is downloaded only when needed.
[Installation instructions for friends](../../deploy/desktop.md)
explain the expected SmartScreen warning: there is deliberately no Authenticode
certificate or code-signing step. Update signatures are separate and mandatory.

The owner has already configured `TAURI_SIGNING_PRIVATE_KEY` and
`TAURI_SIGNING_PRIVATE_KEY_PASSWORD` as GitHub repository secrets. The public
key is bundled in `tauri.conf.json`; never replace it with a local test key.
The workflow exposes the private values only to the build step, never the
instance server or its pages. It uses checksum-verified Tauri CLI 2.12.4.

1. Choose the desktop version (the first release is `0.2.0`), update both
   `desktop/app/Cargo.toml` and `desktop/app/tauri.conf.json`, and run the crate's
   tests to refresh the root package version in `Cargo.lock` if necessary.
   Keep `CARGO_TARGET_DIR` outside the checkout, such as
   `%USERPROFILE%\zz-build\desktop-check`. Run `bash tools/agents/check.sh`,
   commit the version change and merge it through review before tagging.
2. From the merged commit, validate and push its desktop tag:

   ```powershell
   $tag = 'desktop-v0.2.0'
   ./tools/desktop/check-version.ps1 -Tag $tag
   git tag $tag
   git push origin $tag
   ```

3. Wait for **Desktop release** in Actions. It verifies that the existing tag
   identifies the checkout and matches both manifests before building. The same
   fmt, clippy and tests run before NSIS packaging. A dedicated release target
   directory under the runner's `CARGO_TARGET_DIR` avoids racing CI builds.
4. Review the resulting **draft**, while the workflow is no longer running.
   It must contain the nonempty x64 setup `.exe`, its `.sig`, and `latest.json`.
   Check the installer size is plausible, the JSON version matches the tag,
   and `platforms.windows-x86_64` names that exact repository/tag/installer
   and carries the `.sig` contents. Notes include only `desktop/` changes
   since the previous desktop tag. Test installation on a normal Windows account.
5. Publish the draft manually and mark it as the latest release. Before that,
   the app cannot see it. Verify the public
   [latest.json](https://github.com/whoaskedssselery/Zazzzor/releases/latest/download/latest.json)
   and test updating an older client on the first real release.

To retry a failed build, use **Actions → Desktop release → Run workflow** with
the existing tag. A rerun replaces assets only while its release is a draft;
published releases are refused. Do not publish a draft while its build is running.
Fixes to a published version need a new version and tag, not moved tags or
replaced published files. To recover from a bad release, publish a newer version
with the fix/revert; the updater intentionally does not downgrade clients.

Updates are checked at startup and every six hours. Download and minisign/version
verification happen before the single **Yes / Later** prompt. **Later** suppresses
further prompts until restart. The tray item uses the same worker and reports
when the client is current. Network/signature failures stay in `update.log`
under Tauri's per-user app log directory. The update source is compiled into the
app, never read from instance settings; instance pages gain no updater permissions.

## Not done yet

- A tray badge for unread mentions, a native notification bridge.
