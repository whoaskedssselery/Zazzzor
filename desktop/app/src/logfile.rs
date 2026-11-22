//! The app's log: what the app and its screen share do, in a file the person
//! can send when something goes wrong (the tray opens its folder). Release
//! builds have no console, so without it a share that misbehaves leaves no
//! trace.

use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

/// A log past this size starts over at the next start; the previous one is
/// kept beside it as `zazzzor.log.old`.
const LIMIT: u64 = 4 * 1024 * 1024;

/// The log file in the app's log folder.
pub fn path(dir: &Path) -> PathBuf {
    dir.join("zazzzor.log")
}

struct FileLog {
    file: Mutex<File>,
}

impl log::Log for FileLog {
    fn enabled(&self, metadata: &log::Metadata) -> bool {
        kept(metadata.target(), metadata.level())
    }

    fn log(&self, record: &log::Record) {
        if !self.enabled(record.metadata()) {
            return;
        }
        let ms = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_millis();
        if let Ok(mut file) = self.file.lock() {
            let _ = writeln!(file, "{ms} {:<5} {} {}", record.level(), record.target(), record.args());
        }
    }

    fn flush(&self) {
        if let Ok(mut file) = self.file.lock() {
            let _ = file.flush();
        }
    }
}

/// The app's own crates down to info; the libraries (LiveKit, WebRTC), which
/// talk a lot, only their warnings.
fn kept(target: &str, level: log::Level) -> bool {
    let ours = target.starts_with("zazzzor");
    level <= if ours { log::Level::Info } else { log::Level::Warn }
}

/// Starts writing the log in `dir`.
pub fn init(dir: &Path) -> std::io::Result<()> {
    fs::create_dir_all(dir)?;
    let file = path(dir);
    if fs::metadata(&file).is_ok_and(|m| m.len() > LIMIT) {
        let _ = fs::rename(&file, dir.join("zazzzor.log.old"));
    }
    let opened = OpenOptions::new().create(true).append(true).open(&file)?;
    // Set once for the whole run: leaking it is how a logger lives that long.
    let logger: &'static FileLog = Box::leak(Box::new(FileLog { file: Mutex::new(opened) }));
    log::set_logger(logger).map_err(|_| std::io::Error::other("a logger is already set"))?;
    log::set_max_level(log::LevelFilter::Info);
    log::info!("Zazzzor {} started", env!("CARGO_PKG_VERSION"));
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_our_info_and_only_the_libraries_warnings() {
        assert!(kept("zazzzor_share::session", log::Level::Info));
        assert!(kept("zazzzor_desktop::share", log::Level::Warn));
        assert!(!kept("zazzzor_share::session", log::Level::Debug));
        assert!(!kept("livekit::room", log::Level::Info));
        assert!(kept("libwebrtc", log::Level::Warn));
    }
}
