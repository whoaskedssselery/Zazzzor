//! The native screen share, for the instance's own pages: the list of
//! screens and windows with thumbnails, and a share started with a token
//! the page got from its server (`POST /channels/{id}/voice/screen`), which
//! joins the call as the user's screen companion. One share at a time.

use std::sync::Mutex;

use serde::Serialize;
use tauri::async_runtime::Mutex as AsyncMutex;
use tauri::ipc::Response;
use tauri::{AppHandle, Manager, State, Url, WebviewWindow};
use zazzzor_share::audio::Scope;
use zazzzor_share::capture::{self, Target};
use zazzzor_share::session::{End, Options, Session, Stats};
use zazzzor_share::sources::{self, Source};

use crate::bridge::from_instance;

/// The commands of this module, as the app's ACL names them.
pub const COMMANDS: [&str; 5] = ["share_sources", "share_thumbnail", "share_start", "share_stop", "share_status"];

/// The share in progress and how the last one ended.
#[derive(Default)]
pub struct Sharing {
    session: AsyncMutex<Option<Session>>,
    ended: Mutex<Option<End>>,
}

/// The screens, then the windows, that can be shared.
#[tauri::command]
pub async fn share_sources(window: WebviewWindow, app: State<'_, crate::App>) -> Result<Vec<Source>, String> {
    from_instance(&window, &app.instance)?;
    let own = std::process::id();
    tauri::async_runtime::spawn_blocking(move || sources::list(own))
        .await
        .map_err(|e| e.to_string())?
        .map_err(|e| format!("{e:#}"))
}

/// A thumbnail of the source `id`, at most 320×180: its width and height as
/// two little-endian u32, then RGBA rows.
#[tauri::command]
pub async fn share_thumbnail(
    id: String,
    window: WebviewWindow,
    app: State<'_, crate::App>,
) -> Result<Response, String> {
    from_instance(&window, &app.instance)?;
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
        (true, Target::Monitor(_)) => Some(Scope::AllBut(std::process::id())),
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
    let session = sharing.session.lock().await.take();
    if let Some(session) = session {
        let (end, _) = session.stop().await;
        *sharing.ended.lock().unwrap() = Some(end);
    }
    crate::sharing_shown(app, false);
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
