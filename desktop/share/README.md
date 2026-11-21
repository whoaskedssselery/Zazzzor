# zazzzor-share

**English** · [Русский](README.ru.md)

The native screen share of the Zazzzor desktop client: a monitor or a
window, with the sound of one application, sent to a Zazzzor call over a
LiveKit connection of its own. The [desktop app](../app/README.md) runs it as
the user's screen companion (`<user id>.screen`, see `joinVoiceScreen` in
the API); the web client shows that share as the person's own.

- `sources`: the monitors and windows that can be shared, with an ID each
  and a thumbnail from one captured frame.
- `session`: a share from `Session::start` to `Session::stop`, with its
  figures every two seconds and why it ended (stopped, the connection
  removed, the window closed).

## The CLI

`examples/share` is the same share as a command line tool, for measuring
it outside the app. Windows 10 2004 or later (the per-process loopback),
x64, with the MSVC build tools and the Windows SDK.

```bash
cargo build --release --examples
target/release/examples/share list
```

`list` prints each source's ID, size, application and title. To share,
sign in as a member of the voice channel (the password comes from
`ZAZZZOR_PASSWORD`, never the command line):

```bash
ZAZZZOR_PASSWORD=… target/release/examples/share share --window "Firefox" --server http://localhost:4173 --channel <voice channel ID> --username sharer
```

- `--monitor N` or `--window "part of the title"` (monitor 1 by default).
- Sound: a window brings its application's sound by default;
  `--audio-process chrome.exe`, `--audio-pid N`, `--audio-system` (everything
  except this program) or `--no-audio`.
- `--encoder auto|hardware|software`, `--fps 60`, `--max-bitrate 10000000`,
  `--seconds N` (stop and print a summary).
- `--livekit-url ws://… --token …` (or `LIVEKIT_TOKEN`) skips the sign-in.
- `--companion` joins as the screen of `--username`, who must already be
  in the call in a browser (otherwise `409 not_in_voice`): one person in
  the call, as the app does. Without it the sharer is a participant of its
  own. When the owner leaves, the companion is removed and the CLI stops.

Every two seconds it logs capture fps, encoded and sent fps, the bitrate and
WebRTC's bandwidth estimate, the capture-to-encoded delay, its CPU use and
the quality level.

### Building on this machine

Two local quirks, neither in the code:

- `cl.exe` and `lib.exe` failed with `D8050` and `LNK1104` when cargo ran
  them with `%TEMP%` under `AppData\Local\Temp`; pointing `TMP` and `TEMP`
  into `target\tmp` fixed it.
- `webrtc-sys` downloads a ~110 MB prebuilt libwebrtc from GitHub at build
  time. On a flaky link download it by hand (`curl -C -` resumes) from
  `https://github.com/livekit/rust-sdks/releases/download/webrtc-a545de4/webrtc-win-x64-release.zip`,
  unpack it and set `LK_CUSTOM_WEBRTC` to the `win-x64-release` folder.

## How it works

```
Windows Graphics Capture ─ BGRA ─▶ libyuv: NV12, scaled to 1080p or 720p ─┬▶ Media Foundation H.264 (GPU) ─▶ LiveKit, pre-encoded
     (monitor or window, ≤60 fps)                                         └▶ libwebrtc VP8 (software)     ─▶ LiveKit
process loopback (one app's tree) ─ 48 kHz stereo PCM, 10 ms chunks ─▶ LiveKit audio source (no AEC/NS/AGC) ─▶ Opus
```

- **Capture**: `windows-capture` (Windows Graphics Capture), cursor on, the
  yellow border off, at most `--fps` frames per second. WGC only delivers a
  frame when the picture changes, so a still screen costs nothing; the
  encoder repeats the last frame every 500 ms so a viewer who joins still
  gets a keyframe.
- **Encode**: the first hardware H.264 encoder Media Foundation lists (NVENC
  here), as an asynchronous MFT: CBR, low-latency mode, baseline profile,
  no B-frames, a keyframe only when WebRTC asks (a viewer joining, a lost
  packet), the bitrate following WebRTC's rate control. The access units go
  to LiveKit through `NativeVideoSource::new_encoded` and the SDK's
  pass-through encoder, so libwebrtc does not encode again. Without a
  hardware encoder (or with `--encoder software`) raw NV12 frames go to
  libwebrtc, which encodes VP8 in software, as the browser does.
- **Sound**: `ActivateAudioInterfaceAsync` on
  `VAD\Process_Loopback` with the target process tree (Windows 10 2004+), the
  only way to take one application's sound; Chrome cannot. 48 kHz stereo,
  published as `ScreenShareAudio` without echo cancellation, noise
  suppression or gain control, at 128 kbit/s.
- **Quality**: the rule of the web client (`screen-quality.ts`): at least
  30 fps in any case; 1080p (up to 60 fps) while it holds; 720p after two
  samples (4 s) of fewer than 30 fps sent while the screen moves, or of a
  bandwidth estimate under 2.5 Mbit/s (a hardware encoder sends every frame,
  so a short link shows there, not as lost frames); back to 1080p after
  10 s with room for 6 Mbit/s, slower after each fall.
- **Join**: the app gets the token from the web client, which asks
  `POST /channels/{id}/voice/screen`; the CLI signs in and asks the same.

## Crates

| Crate | Version | Why |
| --- | --- | --- |
| `livekit` | 0.10.0 | The LiveKit Rust SDK; has pre-encoded video sources (`new_encoded`, pass-through encoder) |
| `webrtc-sys` | 0.3.52 (locked) | The libwebrtc it wraps; held at a release two weeks old |
| `windows-capture` | 2.0.1 | Windows Graphics Capture with D3D11 readback, monitor and window enumeration |
| `windows` | 0.62.2 | Media Foundation, WASAPI process loopback, ToolHelp |
| `tokio`, `serde`, `anyhow`, `log` | | Runtime, the figures for the app; the CLI adds `clap`, `ureq`, `serde_json` |

Rejected: FFmpeg bindings for encoding (a large native dependency for what
Media Foundation does with the driver's encoder); `scap` and `xcap` (no
window-level control or no per-process sound); the SDK's own NVENC path
(Linux only in this release).

## Measured

On the owner's PC (Windows 11 build 26300, RTX 4060 with driver 621.01,
monitors 2560×1440@200, 1920×1080@165 and 1920×1080@60), sharing to the
local e2e stack ([web/e2e/compose.yml](../../web/e2e/compose.yml)) and
watched in the web client on the same machine. CPU is this process's
share of all cores, averaged over two-second samples.

| What | Encoder | Captured / sent fps | Encode | Convert | CPU | Bitrate |
| --- | --- | --- | --- | --- | --- | --- |
| 2560×1440 monitor → 1920×1080 | NVENC (Media Foundation) | 60 / 60 | 1.6 ms | 5.1 ms | 4.0 % | 7–11 Mbit/s |
| 1920×1080 monitor, partly moving | NVENC | 34 / 35 (what changed) | 1.6 ms | 4.4 ms | 2.0 % | 1.5 Mbit/s |
| 1920×1080 monitor, same content | libwebrtc VP8 (software) | 47 / 47 at peak | — | 2.9 ms | 6–7 % | — |
| 1266×712 window with a clock | NVENC | 50 / 50 (its repaint rate) | 0.9 ms | 1.5 ms | 2 % | 0.9 Mbit/s |

- **The viewer** (the web client, Chromium) decoded 49.8 fps of the window
  share with no dropped frames, at its full size. The web client needed no
  change: it showed the share as "sharer — screen share", "Watch" worked as
  for a browser share, and the sound played.
- **Latency, glass to glass**: a millisecond clock in the shared window
  next to the current time drawn on the viewer's page, read from
  screenshots: 7, 46 and 56 ms (one machine, no network; the clock itself
  repaints every 10–16 ms). Encoding is ~1–2 ms of it, conversion 1.5–5 ms.
- **Sound**: a 440 Hz tone at −21 dBFS played by one process arrived at the
  viewer at −20.6 dBFS; the rest of the system's sound did not.
- **Rate**: Windows rounds the capture's minimum update interval up
  (16.7 ms gave 20 ms, 50 fps), so the capture is asked for twice the rate
  and paced in the frame callback, before the GPU readback; that gave a
  steady 60.0 fps.
- **The 720p fallback** is covered by unit tests (`quality.rs`) and wired
  in; it did not trigger here, where the link is local and the encoder
  idles. It still needs a run over a throttled link.

Not measured: a game in exclusive full screen (WGC captures it, but the
spike was not tried on one), and a run between two machines over the
internet.

### Local quirks met on the way

- **Capture refused under the repository folder.** Windows Graphics Capture
  answered `E_ACCESSDENIED` for every monitor and window when the binary
  ran from anywhere under `WebstormProjects\projects\Zazzzor`, and worked
  for the same binary copied to `%USERPROFILE%\capture-probe`. The privacy
  settings allow capture for desktop apps; the cause is the folder's sandbox
  ACL (see the [desktop app README](../app/README.md)). `list` leaves out what cannot be captured.
- **A minimised window gives no frames**; the window must be restored.

## Next

- **Zero-copy next.** The frame now goes GPU → CPU (BGRA readback) → libyuv
  → CPU → GPU (the encoder's input). The next step keeps it on the GPU:
  the WGC texture through the D3D11 video processor (BGRA → NV12 and the
  scale in one pass) into the encoder via an `IMFDXGIDeviceManager` on the
  same device. That removes the readback and most of the CPU time.
- **Window resize and minimise.** A resized window changes the frame size
  and reopens the encoder (a keyframe, a short hitch); a minimised window
  gives no frames. The UI should say so rather than show a frozen picture.
- **HEVC or AV1 later.** The pass-through accepts H.265 and AV1, and NVENC
  makes both; browsers decode AV1 everywhere, H.265 not reliably. AV1 at the
  same bitrate is visibly sharper on text, worth a measured follow-up.
