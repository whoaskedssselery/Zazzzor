# Zazzzor for Windows

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
CLI or Node is needed: `setup/` is plain HTML, CSS and JS.

On the first start the window asks for the server's address
("Подключение к серверу"): the address Zazzzor opens at in a browser. A
bare host means `https://`; a path is dropped. For the local e2e stack
(`pnpm e2e:up` and the `web-e2e` dev server, see `web/e2e/compose.yml`)
it is `http://localhost:4173`.

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
  (`getDisplayMedia` opens WebView2's picker).
- **Permissions.** The chosen server gets the microphone, the camera and
  notifications without WebView2's prompt, as a browser remembers them;
  any other page, and any other permission, gets WebView2's default.
- **Its own pages only.** Pages of the server stay in the window; links to
  other sites, `target=_blank` and `window.open` open in the default
  browser. The server's pages get no access to the app at all: the three
  commands (`current_instance`, `set_instance`, `open_instance`) answer
  only the bundled setup page, which the server's pages cannot navigate to.
- **Tray.** Closing the window hides it to the tray; a left click on the
  icon or "Открыть Zazzzor" brings it back; "Сменить сервер…" opens the
  setup page with the current address and a "Отмена" back; "Выйти" quits.
- **One instance.** A second launch brings the running window to the front
  and exits.
- **Window state.** Size, position and the maximised state are restored on
  the next start (saved when the window hides and when the app quits).

## Checked

On the owner's PC against the local e2e stack, driving the WebView2 over
its DevTools protocol (`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=
--remote-debugging-port=9222`) and the window through Win32:

- first run shows the setup page; `ftp://x` is refused with a message;
  `http://localhost:4173` is saved and opened;
- the server's page calling `set_instance` or `current_instance` is
  refused, and it cannot navigate to the setup page or to another origin;
- sign-in, the channel list, "В сети" (gateway connected) and "Голос
  подключён" (LiveKit) in a voice channel; after a restart the session is
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

## Not done yet

- **The native screen share** from `desktop/capture-spike` (one app's
  sound, NVENC, 1080p60): it waits for the server to accept a second
  LiveKit connection per user for the screen (task C19), then the page
  asks the app to start it.
- Global hotkeys, push-to-talk, auto-update, an installer and code signing.
- Start with Windows, a tray badge for unread mentions, a native
  notification bridge.
- A Windows CI job; until then run `cargo test`, `cargo clippy
  --all-targets -- -D warnings` and `cargo fmt --check` here.
