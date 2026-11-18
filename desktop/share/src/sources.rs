//! What can be shared: the monitors and the windows, each with an ID the
//! picker hands back, and a small picture of each from one captured frame.

use std::sync::mpsc::{SyncSender, sync_channel};
use std::time::Duration;

use anyhow::{Context as _, Result, anyhow, bail};
use serde::Serialize;
use windows_capture::capture::{Context, GraphicsCaptureApiHandler};
use windows_capture::frame::Frame;
use windows_capture::graphics_capture_api::InternalCaptureControl;
use windows_capture::monitor::Monitor;
use windows_capture::settings::MinimumUpdateIntervalSettings;
use windows_capture::window::Window;

use crate::capture::{Target, check, settings, window};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Kind {
    Screen,
    Window,
}

/// A monitor or a window that can be shared.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Source {
    /// What `Target::parse` takes back: `screen:<n>` or `window:<handle>`.
    pub id: String,
    pub kind: Kind,
    /// A window's title; a monitor's device name.
    pub name: String,
    /// The window's executable (e.g. "chrome.exe"); none for a monitor.
    pub app: Option<String>,
    /// The window's process, whose sound goes with it.
    pub pid: Option<u32>,
    pub width: u32,
    pub height: u32,
}

impl Target {
    /// The ID of this target, as `Source::id`.
    pub fn id(&self) -> String {
        match self {
            Target::Monitor(n) => format!("screen:{n}"),
            Target::Window(hwnd) => format!("window:{hwnd}"),
        }
    }

    /// The target of a `Source::id`.
    pub fn parse(id: &str) -> Result<Target> {
        let parsed = match id.split_once(':') {
            Some(("screen", n)) => n.parse().ok().filter(|n| *n >= 1).map(Target::Monitor),
            Some(("window", h)) => h.parse().ok().map(Target::Window),
            _ => None,
        };
        parsed.ok_or_else(|| anyhow!("sources: no source {id:?}"))
    }
}

/// The monitors that can be captured, then the windows with a title,
/// except those of the process `own_pid` (the app itself: sharing it shows
/// the share inside the share).
pub fn list(own_pid: u32) -> Result<Vec<Source>> {
    let mut all = Vec::new();
    for (i, m) in Monitor::enumerate()?.into_iter().enumerate() {
        if check(m).is_err() {
            continue;
        }
        all.push(Source {
            // Monitor::from_index counts from one.
            id: Target::Monitor(i + 1).id(),
            kind: Kind::Screen,
            name: m.name().unwrap_or_default(),
            app: None,
            pid: None,
            width: m.width().unwrap_or(0),
            height: m.height().unwrap_or(0),
        });
    }
    for w in Window::enumerate()? {
        let title = w.title().unwrap_or_default();
        let pid = w.process_id().unwrap_or(0);
        let (width, height) = (w.width().unwrap_or(0), w.height().unwrap_or(0));
        if title.trim().is_empty() || pid == own_pid || width <= 0 || height <= 0 || cloaked(&w) {
            continue;
        }
        all.push(Source {
            id: Target::Window(w.as_raw_hwnd() as isize).id(),
            kind: Kind::Window,
            name: title,
            app: w.process_name().ok(),
            pid: Some(pid),
            width: width as u32,
            height: height as u32,
        });
    }
    Ok(all)
}

/// Whether Windows keeps `w` out of sight though it counts as visible: the
/// windows of suspended Store apps, the input panel, those on another
/// virtual desktop.
fn cloaked(w: &Window) -> bool {
    use windows::Win32::Foundation::HWND;
    use windows::Win32::Graphics::Dwm::{DWMWA_CLOAKED, DwmGetWindowAttribute};
    let mut cloaked = 0u32;
    let hwnd = HWND(w.as_raw_hwnd());
    let got = unsafe {
        DwmGetWindowAttribute(hwnd, DWMWA_CLOAKED, (&mut cloaked as *mut u32).cast(), size_of::<u32>() as u32)
    };
    got.is_ok() && cloaked != 0
}

/// A small picture: RGBA, rows of `width` pixels with no padding.
pub struct Thumbnail {
    pub width: u32,
    pub height: u32,
    pub rgba: Vec<u8>,
}

struct Snap {
    out: SyncSender<Thumbnail>,
    bounds: (u32, u32),
}

impl GraphicsCaptureApiHandler for Snap {
    type Flags = (SyncSender<Thumbnail>, (u32, u32));
    type Error = anyhow::Error;

    fn new(ctx: Context<Self::Flags>) -> Result<Self, Self::Error> {
        let (out, bounds) = ctx.flags;
        Ok(Snap { out, bounds })
    }

    fn on_frame_arrived(&mut self, frame: &mut Frame, control: InternalCaptureControl) -> Result<(), Self::Error> {
        let (width, height) = (frame.width(), frame.height());
        let mut buffer = frame.buffer()?;
        let pitch = buffer.row_pitch() as usize;
        let thumb = shrink(buffer.as_raw_buffer(), pitch, width, height, self.bounds);
        let _ = self.out.try_send(thumb);
        control.stop();
        Ok(())
    }
}

/// One frame of `target`, shrunk to fit `max_width`×`max_height`. Fails
/// when no frame comes within a second (a minimised window gives none).
pub fn thumbnail(target: Target, max_width: u32, max_height: u32) -> Result<Thumbnail> {
    let (tx, rx) = sync_channel(1);
    let flags = (tx, (max_width.max(1), max_height.max(1)));
    let interval = MinimumUpdateIntervalSettings::Default;
    let control = match target {
        Target::Monitor(index) => {
            let monitor = Monitor::from_index(index).with_context(|| format!("sources: no monitor {index}"))?;
            Snap::start_free_threaded(settings(monitor, interval, flags))
        }
        Target::Window(hwnd) => Snap::start_free_threaded(settings(window(hwnd)?, interval, flags)),
    }
    .map_err(|e| anyhow!("sources: capture: {e}"))?;
    let thumb = rx.recv_timeout(Duration::from_secs(1));
    let _ = control.stop();
    match thumb {
        Ok(t) => Ok(t),
        Err(_) => bail!("sources: no frame of {}", target.id()),
    }
}

/// Averages BGRA rows of `pitch` bytes into an RGBA picture that fits
/// `bounds`, keeping the aspect and never enlarging.
fn shrink(bgra: &[u8], pitch: usize, width: u32, height: u32, bounds: (u32, u32)) -> Thumbnail {
    let scale = f64::min(1.0, f64::min(bounds.0 as f64 / width as f64, bounds.1 as f64 / height as f64));
    let (w, h) = (((width as f64 * scale).round() as u32).max(1), ((height as f64 * scale).round() as u32).max(1));
    let mut rgba = Vec::with_capacity((w * h * 4) as usize);
    for y in 0..h {
        let (y0, y1) = span(y, h, height);
        for x in 0..w {
            let (x0, x1) = span(x, w, width);
            let mut sum = [0u32; 3];
            for sy in y0..y1 {
                let row = &bgra[sy as usize * pitch..];
                for sx in x0..x1 {
                    let p = &row[sx as usize * 4..][..3];
                    sum[0] += p[2] as u32;
                    sum[1] += p[1] as u32;
                    sum[2] += p[0] as u32;
                }
            }
            let n = (y1 - y0) * (x1 - x0);
            rgba.extend_from_slice(&[(sum[0] / n) as u8, (sum[1] / n) as u8, (sum[2] / n) as u8, 255]);
        }
    }
    Thumbnail { width: w, height: h, rgba }
}

/// The source pixels `[from, to)` that destination pixel `i` of `out` covers in `size`.
fn span(i: u32, out: u32, size: u32) -> (u32, u32) {
    let from = (i as u64 * size as u64 / out as u64) as u32;
    let to = (((i + 1) as u64 * size as u64).div_ceil(out as u64) as u32).clamp(from + 1, size);
    (from, to)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ids_go_both_ways() {
        for t in [Target::Monitor(2), Target::Window(0x1a2b)] {
            assert_eq!(Target::parse(&t.id()).unwrap(), t);
        }
        for bad in ["screen:0", "screen:x", "window:", "tab:1", "1"] {
            assert!(Target::parse(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn a_picture_shrinks_to_fit_and_turns_rgba() {
        // 4x2 BGRA with a padded pitch: left half blue, right half red.
        let pitch = 4 * 4 + 8;
        let mut bgra = vec![0u8; pitch * 2];
        for y in 0..2 {
            for x in 0..4 {
                let px = if x < 2 { [255, 0, 0, 255] } else { [0, 0, 255, 255] };
                bgra[y * pitch + x * 4..][..4].copy_from_slice(&px);
            }
        }
        let t = shrink(&bgra, pitch, 4, 2, (2, 2));
        assert_eq!((t.width, t.height), (2, 1));
        assert_eq!(t.rgba, [0, 0, 255, 255, 255, 0, 0, 255]);
    }

    #[test]
    fn a_small_picture_is_not_enlarged() {
        let t = shrink(&[1, 2, 3, 255], 4, 1, 1, (320, 180));
        assert_eq!((t.width, t.height), (1, 1));
        assert_eq!(t.rgba, [3, 2, 1, 255]);
    }
}
