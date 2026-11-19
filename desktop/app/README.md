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
CLI or Node is needed: `setup/` is plain HTML, CSS and JS. The screen
share links the prebuilt libwebrtc, which `webrtc-sys` downloads at build
time (~110 MB; `LK_CUSTOM_WEBRTC` points at a local copy, see
`desktop/share/README.md`), and with it the C runtime statically
(`.cargo/config.toml`).

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
- **The native screen share** (`desktop/share`): a screen or a window at
  up to 1080p60 on the GPU's encoder, with one application's sound (or the
  whole system's but the app's own, so the call is not sent back). The
  server's pages, and only the chosen server's, may call five share commands:
  `share_sources`, `share_thumbnail`, `share_start`, `share_stop` and
  `share_status`. A capability for the server's origin is added at run
  time (an ACL manifest in `build.rs` makes every command need a
  permission), and each command checks the calling page again. The page
  asks its server for the companion token (`POST
  /channels/{id}/voice/screen`) and hands it over; the app joins only the
  LiveKit of the same host. The share stops when the app quits. The
  chosen server is trusted with this as Discord's app trusts discord.com:
  its pages could list windows and start a share without the picker, so
  only a server the person picked gets the commands.
- **Push-to-talk everywhere.** In a call with push-to-talk the page asks
  the app to watch its key (`hotkeys_ptt`); a low-level keyboard hook
  reports it while the window is behind, so it works in a game. The hook
  only looks (the key still reaches the program in front) and runs only
  while a key is asked for.
- **Mute and deafen combinations everywhere.** The page asks the app to
  watch the combinations saved in the voice settings (`hotkeys_toggles`,
  e.g. "Control+Shift+KeyM"); the same hook reports each press while the
  window is behind, and a held combination flips once.
- **Start with Windows.** "Запускать вместе с Windows" in the tray menu
  writes the app under the user's Run key with `--autostart`; started so,
  the app stays in the tray.
- **Tray.** Closing the window hides it to the tray; a left click on the
  icon or "Открыть Zazzzor" brings it back; "Сменить сервер…" opens the
  setup page with the current address and a "Отмена" back; "Остановить
  демонстрацию" (enabled, and the tooltip says so, while a share runs)
  stops the share; "Выйти" quits.
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

- An auto-update and an installer.
- A tray badge for unread mentions, a native notification bridge.
