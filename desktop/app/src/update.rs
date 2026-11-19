//! Signed desktop updates run in Rust, never through the instance's pages.

use std::fs::{self, OpenOptions};
use std::io::Write;
use std::sync::mpsc::{self, Receiver, SyncSender};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use tauri::{AppHandle, Manager};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};
use tauri_plugin_updater::{Update, UpdaterExt};
use tauri_plugin_window_state::AppHandleExt;
use url::Url;

const INTERVAL: Duration = Duration::from_secs(6 * 60 * 60);
const TIMEOUT: Duration = Duration::from_secs(30);
const DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(10 * 60);

/// Prepares a candidate for a separately bounded installer download.
pub(super) fn bounded_download(mut update: Update, timeout: Duration) -> Update {
    update.timeout = Some(timeout);
    update
}

struct Requests(SyncSender<()>);

/// Starts one worker: a bounded channel coalesces tray requests while it is busy.
pub fn start(app: &AppHandle) -> std::io::Result<()> {
    let (send, receive) = mpsc::sync_channel(1);
    app.manage(Requests(send));
    let app = app.clone();
    std::thread::Builder::new().name("desktop-update".into()).spawn(move || {
        let backend = Desktop(app);
        let mut offered = false;
        let mut next = Instant::now();
        loop {
            let manual = match receive.recv_timeout(next.saturating_duration_since(Instant::now())) {
                Ok(()) => true,
                Err(mpsc::RecvTimeoutError::Timeout) => false,
                Err(mpsc::RecvTimeoutError::Disconnected) => return,
            };
            next = next_check(Instant::now(), next);
            if let Err(error) = tauri::async_runtime::block_on(perform_check(&backend, &mut offered, manual, &receive))
            {
                log_error(&backend.0, &error);
            }
        }
    })?;
    Ok(())
}

/// A tray request between scheduled checks does not postpone the background one.
pub(super) fn next_check(now: Instant, scheduled: Instant) -> Instant {
    if scheduled <= now { now + INTERVAL } else { scheduled }
}

/// Requests the same check as the background worker, with feedback if up to date.
pub fn check(app: &AppHandle) {
    if let Err(mpsc::TrySendError::Disconnected(())) = app.state::<Requests>().0.try_send(()) {
        log_error(app, "update worker stopped");
    }
}

/// The transport verifies downloads; the coordinator owns consent and suppression.
pub(super) trait Backend {
    type Candidate;
    fn check(&self) -> impl std::future::Future<Output = Result<Option<Self::Candidate>, String>>;
    fn version<'a>(&self, candidate: &'a Self::Candidate) -> &'a str;
    fn download(&self, candidate: &Self::Candidate) -> impl std::future::Future<Output = Result<Vec<u8>, String>>;
    fn confirm(&self, version: &str) -> bool;
    fn install(&self, candidate: &Self::Candidate, data: Vec<u8>) -> Result<(), String>;
    fn latest(&self);
}

/// Drains requests after every outcome, including errors and a visible dialog.
/// A manual click during a background check shares its result, not another download.
pub(super) async fn perform_check<B: Backend>(
    backend: &B,
    offered: &mut bool,
    manual: bool,
    requests: &Receiver<()>,
) -> Result<(), String> {
    let result = async {
        let Some(update) = backend.check().await? else { return Ok(true) };
        if *offered {
            return Ok(false);
        }
        let data = backend.download(&update).await?;
        *offered = true;
        if backend.confirm(backend.version(&update)) {
            backend.install(&update, data)?;
        }
        Ok(false)
    }
    .await;
    // Do not short-circuit this read when the initiating request was manual.
    let queued = requests.try_recv().is_ok();
    if matches!(result, Ok(true)) && (manual || queued) {
        backend.latest();
    }
    result.map(|_| ())
}

/// Only assets of this repository and announced desktop version are accepted.
/// The source comes from the bundled configuration, never instance settings.
pub(super) fn validate_asset(source: &Url, asset: &Url, version: &str) -> Result<(), String> {
    let prefix = format!("/whoaskedssselery/Zazzzor/releases/download/desktop-v{version}/");
    if !crate::instance::same_origin(source, asset)
        || !asset.username().is_empty()
        || asset.password().is_some()
        || asset.query().is_some()
        || asset.fragment().is_some()
        || !asset.path().starts_with(&prefix)
        || !asset.path().ends_with(".exe")
    {
        return Err("update asset is outside the configured repository release".into());
    }
    Ok(())
}

struct Desktop(AppHandle);

impl Backend for Desktop {
    type Candidate = Update;

    async fn check(&self) -> Result<Option<Update>, String> {
        let updater = self.0.updater_builder().timeout(TIMEOUT).build().map_err(|e| format!("create updater: {e}"))?;
        let update = updater.check().await.map_err(|e| format!("check update: {e}"))?;
        if let Some(ref update) = update {
            let config = self.0.config();
            let source = config
                .plugins
                .0
                .get("updater")
                .and_then(|updater| updater.get("endpoints"))
                .and_then(|endpoints| endpoints.get(0))
                .and_then(|endpoint| endpoint.as_str())
                .ok_or("missing bundled update source")?;
            let source = Url::parse(source).map_err(|e| format!("parse bundled update source: {e}"))?;
            validate_asset(&source, &update.download_url, &update.version)?;
        }
        Ok(update)
    }

    fn version<'a>(&self, update: &'a Update) -> &'a str {
        &update.version
    }

    async fn download(&self, update: &Update) -> Result<Vec<u8>, String> {
        // The plugin verifies the minisign signature before returning the bytes.
        bounded_download(update.clone(), DOWNLOAD_TIMEOUT)
            .download(|_, _| {}, || {})
            .await
            .map_err(|e| format!("download/verify update: {e}"))
    }

    fn confirm(&self, version: &str) -> bool {
        self.0
            .dialog()
            .message(format!("Доступно обновление Zazzzor {version}. Перезапустить и установить?"))
            .title("Zazzzor")
            .buttons(MessageDialogButtons::OkCancelCustom("Да".into(), "Позже".into()))
            .blocking_show()
    }

    fn install(&self, update: &Update, data: Vec<u8>) -> Result<(), String> {
        let _ = self.0.save_window_state(crate::state_flags());
        // On Windows the plugin starts NSIS with restart enabled and exits.
        update.install(data).map_err(|e| format!("install update: {e}"))
    }

    fn latest(&self) {
        self.0.dialog().message("Установлена последняя версия").title("Zazzzor").show(|_| {});
    }
}

/// Release builds have no console; keep quiet update failures in the app's logs.
fn log_error(app: &AppHandle, error: &str) {
    let result = (|| -> std::io::Result<()> {
        let dir = app.path().app_log_dir().map_err(std::io::Error::other)?;
        fs::create_dir_all(&dir)?;
        let mut file = OpenOptions::new().create(true).append(true).open(dir.join("update.log"))?;
        let time = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_secs();
        writeln!(file, "{time} {error}")
    })();
    if result.is_err() {
        eprintln!("desktop updater: {error}");
    }
}
