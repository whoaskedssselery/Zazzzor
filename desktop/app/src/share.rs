//! The native screen share, for the instance's own pages: the list of
//! screens and windows with thumbnails, and a share started with a token
//! the page got from its server (`POST /channels/{id}/voice/screen`), which
//! joins the call as the user's screen companion. One share at a time.
//!
//! The page cannot share or look at anything on its own: it sees the
//! sources and their thumbnails only while the window is shown and for a
//! minute after it listed them for its picker.

use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Serialize;
use tauri::async_runtime::Mutex as AsyncMutex;
use tauri::ipc::Response;
use tauri::{AppHandle, Manager, State, Url, WebviewWindow};
use zazzzor_share::audio::Scope;
use zazzzor_share::capture::{self, Target};
use zazzzor_share::session::{End, Options, Session, Stats};
use zazzzor_share::sources::{self, Source};

use crate::bridge::from_instance;

/// The commands of this module, as the app's ACL names them; each checks
/// `from_instance` first (bridge.rs).
pub const COMMANDS: [&str; 5] = ["share_sources", "share_thumbnail", "share_start", "share_stop", "share_status"];

/// The share in progress and how the last one ended.
#[derive(Default)]
pub struct Sharing {
    session: AsyncMutex<Option<Session>>,
    ended: Mutex<Option<End>>,
    picker: Mutex<Picker>,
}

/// How long after the page listed the sources it may take their thumbnails.
const PICKER_OPEN: Duration = Duration::from_secs(60);
/// How often the sources are listed again; a page asking sooner gets the last list.
const LIST_EVERY: Duration = Duration::from_secs(2);

/// What the page's picker may see.
#[derive(Default)]
struct Picker {
    /// The last list and when it was taken.
    listed: Option<(Instant, Vec<Source>)>,
    /// The sources of the last list whose thumbnail was taken: one each.
    thumbnailed: Vec<String>,
}

impl Picker {
    /// The last list, if it is recent enough to be given again.
    fn recent(&self, now: Instant) -> Option<Vec<Source>> {
        let (at, list) = self.listed.as_ref()?;
        (now.duration_since(*at) < LIST_EVERY).then(|| list.clone())
    }

    /// Keeps a new list: its sources' thumbnails may be taken for a while.
    fn list(&mut self, now: Instant, list: Vec<Source>) {
        self.listed = Some((now, list));
        self.thumbnailed.clear();
    }

    /// Whether the thumbnail of `id` may be taken now; it is counted if so.
    fn thumbnail(&mut self, id: &str, now: Instant) -> bool {
        let Some((at, list)) = &self.listed else { return false };
        let ok = now.duration_since(*at) < PICKER_OPEN
            && list.iter().any(|s| s.id == id)
            && !self.thumbnailed.iter().any(|t| t == id);
        if ok {
            self.thumbnailed.push(id.to_owned());
        }
        ok
    }
}

/// Lets the page see the sources only while the window is shown.
fn shown(window: &WebviewWindow) -> Result<(), String> {
    let shown = window.is_visible().unwrap_or(false) && !window.is_minimized().unwrap_or(true);
    if shown { Ok(()) } else { Err("the window is not shown".into()) }
}

/// The screens, then the windows, that can be shared. Listed at most once
/// every two seconds, while the window is shown; a minute of thumbnails follows.
#[tauri::command]
pub async fn share_sources(
    window: WebviewWindow,
    app: State<'_, crate::App>,
    sharing: State<'_, Sharing>,
) -> Result<Vec<Source>, String> {
    from_instance(&window, &app.instance)?;
    shown(&window)?;
    if let Some(list) = sharing.picker.lock().unwrap().recent(Instant::now()) {
        return Ok(list);
    }
    let own = std::process::id();
    let list = tauri::async_runtime::spawn_blocking(move || sources::list(own))
        .await
        .map_err(|e| e.to_string())?
        .map_err(|e| format!("{e:#}"))?;
    sharing.picker.lock().unwrap().list(Instant::now(), list.clone());
    Ok(list)
}

/// A thumbnail of the source `id`, at most 320×180: its width and height as
/// two little-endian u32, then RGBA rows. Only for a source of the last
/// list, once, within a minute of it, while the window is shown.
#[tauri::command]
pub async fn share_thumbnail(
    id: String,
    window: WebviewWindow,
    app: State<'_, crate::App>,
    sharing: State<'_, Sharing>,
) -> Result<Response, String> {
    from_instance(&window, &app.instance)?;
    shown(&window)?;
    if !sharing.picker.lock().unwrap().thumbnail(&id, Instant::now()) {
        return Err("not a source of the open picker".into());
    }
    let target = Target::parse(&id).map_err(|e| format!("{e:#}"))?;
    let thumb = tauri::async_runtime::spawn_blocking(move || sources::thumbnail(target, 320, 180))
        .await
        .map_err(|e| e.to_string())?
        .map_err(|e| format!("{e:#}"))?;
    let mut bytes = Vec::with_capacity(8 + thumb.rgba.len());
    bytes.extend_from_slice(&thumb.width.to_le_bytes());
    bytes.extend_from_slice(&thumb.height.to_le_bytes());
    bytes.extend_from_slice(&thumb.rgba);
    Ok(Response::new(bytes))
}

/// Starts sharing the source `id` over the LiveKit `url` with `token`,
/// replacing a share in progress. With `sound`, a window brings its
/// application's sound and a screen the whole system's except this app's
/// (so the call itself is not sent back). At most `fps` frames per
/// second (30 or 60; 60 when not said).
#[tauri::command]
pub async fn share_start(
    url: String,
    token: String,
    id: String,
    sound: bool,
    fps: Option<u32>,
    window: WebviewWindow,
    app: State<'_, crate::App>,
) -> Result<(), String> {
    let home = from_instance(&window, &app.instance)?;
    let sharing = window.app_handle().state::<Sharing>();
    check_signaling(&url, &home)?;
    let target = Target::parse(&id).map_err(|e| format!("{e:#}"))?;
    let scope = match (sound, target) {
        (false, _) => None,
        (true, Target::Window(hwnd)) => capture::window_process(hwnd).ok().map(Scope::Only),
        (true, Target::Monitor(_)) => Some(Scope::AllBut(call_process(&window).await)),
    };
    let mut current = sharing.session.lock().await;
    if let Some(old) = current.take() {
        old.stop().await;
    }
    *sharing.ended.lock().unwrap() = None;
    let session = Session::start(&url, &token, &target, scope, options(fps)).await.map_err(|e| format!("{e:#}"))?;
    *current = Some(session);
    crate::sharing_shown(window.app_handle(), true);
    Ok(())
}

/// The process whose tree plays the call: WebView2's browser process. A
/// whole screen's sound leaves it out, or the others would hear themselves
/// back. WebView2 runs outside the app's own process tree, so leaving out
/// the app alone is not enough; the app's id is the fallback.
async fn call_process(window: &WebviewWindow) -> u32 {
    let (tx, rx) = std::sync::mpsc::channel();
    let asked = window.with_webview(move |webview| {
        let mut pid = 0u32;
        // SAFETY: a plain query on the live controller, on the thread that owns it.
        let ok = unsafe { webview.controller().CoreWebView2().and_then(|core| core.BrowserProcessId(&mut pid)) };
        let _ = tx.send(ok.ok().map(|()| pid).filter(|&p| p != 0));
    });
    let answer = match asked {
        Ok(()) => tauri::async_runtime::spawn_blocking(move || rx.recv_timeout(Duration::from_secs(2)).ok().flatten())
            .await
            .ok()
            .flatten(),
        Err(_) => None,
    };
    answer.unwrap_or_else(std::process::id)
}

/// Stops the share in progress, if any.
#[tauri::command]
pub async fn share_stop(window: WebviewWindow, app: State<'_, crate::App>) -> Result<(), String> {
    from_instance(&window, &app.instance)?;
    stop(window.app_handle()).await;
    Ok(())
}

/// Stops the share in progress, if any: from the page or the tray menu.
pub async fn stop(app: &AppHandle) {
    let sharing = app.state::<Sharing>();
    // Held until the tray is updated: a share started meanwhile waits, so this
    // stop cannot mark it as stopped.
    let mut current = sharing.session.lock().await;
    if let Some(session) = current.take() {
        let (end, _) = session.stop().await;
        *sharing.ended.lock().unwrap() = Some(end);
    }
    crate::sharing_shown(app, false);
    drop(current);
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase", tag = "state")]
pub enum Status {
    /// Nothing shared since the app started, or since the last share's end was read.
    Idle,
    Sharing {
        stats: Option<Stats>,
    },
    /// The last share ended on its own or was stopped; read once.
    Ended {
        end: End,
    },
}

/// Whether a share is going, with its last figures, or how it ended.
#[tauri::command]
pub async fn share_status(
    window: WebviewWindow,
    app: State<'_, crate::App>,
    sharing: State<'_, Sharing>,
) -> Result<Status, String> {
    from_instance(&window, &app.instance)?;
    let mut current = sharing.session.lock().await;
    if current.as_ref().is_some_and(Session::is_finished) {
        let (end, _) = current.take().unwrap().stop().await;
        *sharing.ended.lock().unwrap() = Some(end);
        crate::sharing_shown(window.app_handle(), false);
    }
    if let Some(session) = current.as_ref() {
        return Ok(Status::Sharing { stats: session.stats() });
    }
    Ok(match sharing.ended.lock().unwrap().take() {
        Some(end) => Status::Ended { end },
        None => Status::Idle,
    })
}

/// The share's options for at most `fps` frames per second: 30 or 60, as the
/// picker offers; anything else is 60.
fn options(fps: Option<u32>) -> Options {
    Options { fps: if fps == Some(30) { 30 } else { 60 }, ..Options::default() }
}

/// The LiveKit URL must be the instance's own host (the API derives it
/// from the page's origin): a share never goes anywhere else.
fn check_signaling(url: &str, home: &Url) -> Result<(), String> {
    let url = Url::parse(url).map_err(|_| "bad LiveKit URL".to_owned())?;
    let secure = match url.scheme() {
        "wss" => true,
        "ws" => false,
        _ => return Err("bad LiveKit URL".into()),
    };
    let home_secure = home.scheme() == "https";
    if url.host_str() != home.host_str()
        || url.port_or_known_default() != home.port_or_known_default()
        || secure != home_secure
    {
        return Err("the LiveKit URL is not the instance's".into());
    }
    Ok(())
}

/// Stops the share when the app quits, so the call sees it end at once.
pub fn stop_on_exit(app: &AppHandle) {
    let sharing = app.state::<Sharing>();
    if let Some(session) = tauri::async_runtime::block_on(sharing.session.lock()).take() {
        tauri::async_runtime::block_on(session.stop());
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use zazzzor_share::sources::Kind;

    fn source(id: &str) -> Source {
        Source {
            id: id.into(),
            kind: Kind::Window,
            name: "Notes".into(),
            app: None,
            pid: None,
            width: 800,
            height: 600,
        }
    }

    #[test]
    fn the_sources_are_listed_at_most_every_two_seconds() {
        let start = Instant::now();
        let mut picker = Picker::default();
        assert!(picker.recent(start).is_none());
        picker.list(start, vec![source("window:1")]);
        assert_eq!(picker.recent(start + Duration::from_millis(1500)).map(|l| l.len()), Some(1));
        assert!(picker.recent(start + LIST_EVERY).is_none());
    }

    #[test]
    fn thumbnails_only_of_the_last_list_once_and_for_a_minute() {
        let start = Instant::now();
        let mut picker = Picker::default();
        assert!(!picker.thumbnail("window:1", start), "nothing listed yet");
        picker.list(start, vec![source("window:1"), source("window:2")]);
        assert!(picker.thumbnail("window:1", start));
        assert!(!picker.thumbnail("window:1", start), "the same source again");
        assert!(!picker.thumbnail("window:3", start), "not in the list");
        assert!(!picker.thumbnail("window:2", start + PICKER_OPEN), "the minute is over");
        picker.list(start + PICKER_OPEN, vec![source("window:1")]);
        assert!(picker.thumbnail("window:1", start + PICKER_OPEN));
        assert!(!picker.thumbnail("window:2", start + PICKER_OPEN), "no longer listed");
    }

    #[test]
    fn a_share_runs_at_30_or_60_fps() {
        assert_eq!(options(Some(30)).fps, 30);
        for other in [None, Some(60), Some(5), Some(240)] {
            assert_eq!(options(other).fps, 60, "{other:?}");
        }
    }

    #[test]
    fn a_share_goes_only_to_the_instance() {
        let home = Url::parse("https://zz.example.com/").unwrap();
        assert!(check_signaling("wss://zz.example.com", &home).is_ok());
        assert!(check_signaling("wss://zz.example.com:443/rtc", &home).is_ok());
        for bad in [
            "ws://zz.example.com",
            "wss://evil.example.com",
            "wss://zz.example.com:8443",
            "https://zz.example.com",
            "x",
        ] {
            assert!(check_signaling(bad, &home).is_err(), "{bad}");
        }
        let local = Url::parse("http://localhost:4173/").unwrap();
        assert!(check_signaling("ws://localhost:4173", &local).is_ok());
        assert!(check_signaling("ws://localhost:7880", &local).is_err());
    }
}
