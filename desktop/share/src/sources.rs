//! What can be shared: the monitors and the windows, each with an ID the
//! picker hands back.

use anyhow::{Result, anyhow};
use serde::Serialize;
use windows_capture::monitor::Monitor;
use windows_capture::window::Window;

use crate::capture::{Target, check};

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
}
