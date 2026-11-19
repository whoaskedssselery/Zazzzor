//! Push-to-talk outside the window. The browser sees keys only while the
//! window is in front; the app watches the one push-to-talk key everywhere
//! with a low-level keyboard hook, which only looks: the key still reaches
//! whatever program is in front, as with Discord. The hook runs only while
//! the page asks for a key (a call with push-to-talk on).
//!
//! The page hears the key as `zazzzor:ptt` events on its window, with
//! `detail` true when it goes down and false when it comes up.

use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Mutex, OnceLock, mpsc};
use std::thread::JoinHandle;

use tauri::{AppHandle, Manager, State, WebviewWindow};
use windows::Win32::Foundation::{LPARAM, LRESULT, WPARAM};
use windows::Win32::System::Threading::GetCurrentThreadId;
use windows::Win32::UI::WindowsAndMessaging::{
    CallNextHookEx, GetMessageW, HC_ACTION, KBDLLHOOKSTRUCT, MSG, PostThreadMessageW, SetWindowsHookExW,
    UnhookWindowsHookEx, WH_KEYBOARD_LL, WM_KEYDOWN, WM_QUIT, WM_SYSKEYDOWN,
};

use crate::bridge::from_instance;
use crate::instance;

/// The commands of this module, as the app's ACL names them.
pub const COMMANDS: [&str; 1] = ["hotkeys_ptt"];

/// The virtual key watched now; 0 for none.
static WATCHED: AtomicU32 = AtomicU32::new(0);
/// Whether the watched key is down.
static HELD: AtomicBool = AtomicBool::new(false);
/// Where the hook reports the key going down (true) and up (false).
static EVENTS: OnceLock<mpsc::Sender<bool>> = OnceLock::new();

/// The hook's thread, while a key is watched.
#[derive(Default)]
pub struct Hotkeys {
    hook: Mutex<Option<Hook>>,
}

struct Hook {
    thread_id: u32,
    thread: JoinHandle<()>,
}

/// Called by Windows for every key of every program: it must return at once.
unsafe extern "system" fn on_key(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if code == HC_ACTION as i32 {
        // SAFETY: for HC_ACTION, lparam points to the event's KBDLLHOOKSTRUCT.
        let key = unsafe { &*(lparam.0 as *const KBDLLHOOKSTRUCT) };
        let watched = WATCHED.load(Ordering::Relaxed);
        if watched != 0 && key.vkCode == watched {
            let down = matches!(wparam.0 as u32, WM_KEYDOWN | WM_SYSKEYDOWN);
            // A held key repeats its key-down: only the changes count.
            if HELD.swap(down, Ordering::Relaxed) != down
                && let Some(events) = EVENTS.get()
            {
                let _ = events.send(down);
            }
        }
    }
    unsafe { CallNextHookEx(None, code, wparam, lparam) }
}

impl Hook {
    /// Installs the hook on a thread of its own, which pumps the messages
    /// a low-level hook is called through.
    fn start() -> Result<Hook, String> {
        let (ready_tx, ready_rx) = mpsc::channel::<Result<u32, String>>();
        let thread = std::thread::Builder::new()
            .name("ptt-hook".into())
            .spawn(move || {
                let hook = match unsafe { SetWindowsHookExW(WH_KEYBOARD_LL, Some(on_key), None, 0) } {
                    Ok(h) => h,
                    Err(e) => {
                        let _ = ready_tx.send(Err(format!("keyboard hook: {e}")));
                        return;
                    }
                };
                let _ = ready_tx.send(Ok(unsafe { GetCurrentThreadId() }));
                let mut msg = MSG::default();
                while unsafe { GetMessageW(&mut msg, None, 0, 0) }.as_bool() {}
                let _ = unsafe { UnhookWindowsHookEx(hook) };
            })
            .map_err(|e| e.to_string())?;
        let thread_id = ready_rx.recv().map_err(|e| e.to_string())??;
        Ok(Hook { thread_id, thread })
    }

    fn stop(self) {
        let _ = unsafe { PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0)) };
        let _ = self.thread.join();
    }
}

/// Sends the hook's reports to the instance's page in `MAIN`, and to no
/// other page.
pub fn deliver(app: &AppHandle) {
    let (tx, rx) = mpsc::channel::<bool>();
    if EVENTS.set(tx).is_err() {
        return;
    }
    let app = app.clone();
    std::thread::spawn(move || {
        for down in rx {
            let Some(window) = app.get_webview_window(crate::MAIN) else { continue };
            let home = app.state::<crate::App>().instance.lock().unwrap().clone();
            let ours = matches!((window.url(), home), (Ok(url), Some(home)) if instance::same_origin(&url, &home));
            if ours {
                let _ = window.eval(format!("window.dispatchEvent(new CustomEvent('zazzzor:ptt',{{detail:{down}}}))"));
            }
        }
    });
}

/// Watches the key `code` (a KeyboardEvent.code, e.g. "KeyV") for
/// push-to-talk everywhere, or nothing when `code` is null. Fails for a key
/// it cannot watch; the page then keeps push-to-talk to its own window.
#[tauri::command]
pub fn hotkeys_ptt(
    code: Option<String>,
    window: WebviewWindow,
    app: State<'_, crate::App>,
    hotkeys: State<'_, Hotkeys>,
) -> Result<(), String> {
    from_instance(&window, &app.instance)?;
    let vk = match code.as_deref() {
        None => 0,
        Some(code) => virtual_key(code).ok_or_else(|| format!("no global key for {code}"))?,
    };
    WATCHED.store(vk, Ordering::Relaxed);
    HELD.store(false, Ordering::Relaxed);
    let mut hook = hotkeys.hook.lock().unwrap();
    match (vk, hook.is_some()) {
        (0, true) => hook.take().unwrap().stop(),
        (0, false) | (_, true) => {}
        (_, false) => *hook = Some(Hook::start()?),
    }
    Ok(())
}

/// The Windows virtual key of a KeyboardEvent.code. Left and right
/// modifiers stay apart, as the low-level hook reports them.
fn virtual_key(code: &str) -> Option<u32> {
    let one = |s: &str| (s.len() == 1).then(|| s.as_bytes()[0]);
    if let Some(c) = code.strip_prefix("Key").and_then(one).filter(u8::is_ascii_uppercase) {
        return Some(c as u32);
    }
    if let Some(c) = code.strip_prefix("Digit").and_then(one).filter(u8::is_ascii_digit) {
        return Some(c as u32);
    }
    if let Some(c) = code.strip_prefix("Numpad").and_then(one).filter(u8::is_ascii_digit) {
        return Some(0x60 + (c - b'0') as u32);
    }
    if let Some(n) = code.strip_prefix('F').and_then(|n| n.parse::<u32>().ok()).filter(|n| (1..=24).contains(n)) {
        return Some(0x70 + n - 1);
    }
    Some(match code {
        "Backquote" => 0xC0,
        "Minus" => 0xBD,
        "Equal" => 0xBB,
        "BracketLeft" => 0xDB,
        "BracketRight" => 0xDD,
        "Backslash" => 0xDC,
        "Semicolon" => 0xBA,
        "Quote" => 0xDE,
        "Comma" => 0xBC,
        "Period" => 0xBE,
        "Slash" => 0xBF,
        "Space" => 0x20,
        "Tab" => 0x09,
        "CapsLock" => 0x14,
        "Enter" => 0x0D,
        "Backspace" => 0x08,
        "Insert" => 0x2D,
        "Delete" => 0x2E,
        "Home" => 0x24,
        "End" => 0x23,
        "PageUp" => 0x21,
        "PageDown" => 0x22,
        "ArrowLeft" => 0x25,
        "ArrowUp" => 0x26,
        "ArrowRight" => 0x27,
        "ArrowDown" => 0x28,
        "ShiftLeft" => 0xA0,
        "ShiftRight" => 0xA1,
        "ControlLeft" => 0xA2,
        "ControlRight" => 0xA3,
        "AltLeft" => 0xA4,
        "AltRight" => 0xA5,
        "Pause" => 0x13,
        "ScrollLock" => 0x91,
        "NumpadAdd" => 0x6B,
        "NumpadSubtract" => 0x6D,
        "NumpadMultiply" => 0x6A,
        "NumpadDivide" => 0x6F,
        "NumpadDecimal" => 0x6E,
        _ => return None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn browser_key_codes_become_windows_keys() {
        assert_eq!(virtual_key("KeyV"), Some(0x56));
        assert_eq!(virtual_key("Digit0"), Some(0x30));
        assert_eq!(virtual_key("Numpad5"), Some(0x65));
        assert_eq!(virtual_key("F1"), Some(0x70));
        assert_eq!(virtual_key("F24"), Some(0x87));
        assert_eq!(virtual_key("Backquote"), Some(0xC0));
        assert_eq!(virtual_key("ControlLeft"), Some(0xA2));
        assert_eq!(virtual_key("AltRight"), Some(0xA5));
        for unknown in ["", "Key", "Keyv", "KeyAB", "F0", "F25", "Fn", "MetaLeft", "MediaPlayPause"] {
            assert_eq!(virtual_key(unknown), None, "{unknown}");
        }
    }
}
