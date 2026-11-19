//! Starting with Windows: the app's line under the user's Run key, which
//! Windows starts at sign-in. It passes `FLAG`, so the app starts in the
//! tray rather than in the person's face.

use windows::Win32::Foundation::{ERROR_FILE_NOT_FOUND, ERROR_SUCCESS};
use windows::Win32::System::Registry::{
    HKEY_CURRENT_USER, REG_SZ, RRF_RT_REG_SZ, RegDeleteKeyValueW, RegGetValueW, RegSetKeyValueW,
};
use windows::core::{PCWSTR, w};

/// What the app is started with at sign-in.
pub const FLAG: &str = "--autostart";

const RUN: PCWSTR = w!(r"Software\Microsoft\Windows\CurrentVersion\Run");
const NAME: PCWSTR = w!("Zazzzor");

/// The command line Windows runs: this executable, quoted, with `FLAG`.
fn command() -> Option<String> {
    let exe = std::env::current_exe().ok()?;
    Some(format!("\"{}\" {FLAG}", exe.display()))
}

/// Whether Windows starts this copy of the app at sign-in (a line left by a
/// copy that moved does not count).
pub fn enabled() -> bool {
    let mut buf = [0u16; 1024];
    let mut len = size_of_val(&buf) as u32;
    let got = unsafe {
        RegGetValueW(HKEY_CURRENT_USER, RUN, NAME, RRF_RT_REG_SZ, None, Some(buf.as_mut_ptr().cast()), Some(&mut len))
    };
    if got != ERROR_SUCCESS {
        return false;
    }
    let chars = (len as usize / 2).saturating_sub(1);
    command().is_some_and(|c| String::from_utf16_lossy(&buf[..chars]) == c)
}

/// Starts the app with Windows, or stops doing so.
pub fn set(on: bool) -> Result<(), String> {
    let result = if on {
        let command = command().ok_or("the app's path is unknown")?;
        let wide: Vec<u16> = command.encode_utf16().chain(Some(0)).collect();
        unsafe {
            RegSetKeyValueW(HKEY_CURRENT_USER, RUN, NAME, REG_SZ.0, Some(wide.as_ptr().cast()), (wide.len() * 2) as u32)
        }
    } else {
        match unsafe { RegDeleteKeyValueW(HKEY_CURRENT_USER, RUN, NAME) } {
            ERROR_FILE_NOT_FOUND => ERROR_SUCCESS,
            other => other,
        }
    };
    if result == ERROR_SUCCESS { Ok(()) } else { Err(format!("autostart: {result:?}")) }
}
