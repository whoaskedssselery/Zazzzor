//! What the instance's own pages may ask of the app: the screen share's
//! commands (share.rs) and the push-to-talk key (hotkeys.rs), nothing else.
//! A remote capability for the instance's origin allows them, and each
//! command checks again that the calling page is the instance's.

use std::sync::{Arc, Mutex};

use tauri::ipc::CapabilityBuilder;
use tauri::{AppHandle, Manager, Url, WebviewWindow};

use crate::{hotkeys, instance, share};

/// Allows the instance's pages at `origin` to call the commands they may.
/// A capability cannot be taken back, so one for an earlier instance stays;
/// the commands still refuse any page but the current instance's.
pub fn allow_instance(app: &AppHandle, origin: &Url) -> tauri::Result<()> {
    let base = origin.origin().ascii_serialization();
    let mut capability = CapabilityBuilder::new(format!("instance-{base}")).remote(format!("{base}/*")).local(false);
    capability = capability.window(crate::MAIN);
    for command in share::COMMANDS.iter().chain(&hotkeys::COMMANDS) {
        capability = capability.permission(format!("allow-{}", command.replace('_', "-")));
    }
    app.add_capability(capability)
}

/// Refuses a command called from anywhere but the instance's own pages;
/// gives the instance's address otherwise.
pub fn from_instance(window: &WebviewWindow, instance: &Arc<Mutex<Option<Url>>>) -> Result<Url, String> {
    let home = instance.lock().unwrap().clone();
    match (window.url(), home) {
        (Ok(url), Some(home)) if instance::same_origin(&url, &home) => Ok(home),
        _ => Err("not allowed".into()),
    }
}
