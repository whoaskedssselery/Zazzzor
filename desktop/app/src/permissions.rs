//! What the instance may use without asking: the microphone, the camera and
//! notifications, as a browser remembers once they are allowed. WebView2
//! would otherwise ask with its own small dialog on every new profile. Only
//! the instance the person chose gets them; any other page, and every other
//! kind of permission, is left to WebView2's default (it asks). Screen
//! capture is not a permission here: getDisplayMedia always shows the
//! picker.

use std::sync::{Arc, Mutex};

use tauri::{Url, WebviewWindow};
use webview2_com::Microsoft::Web::WebView2::Win32::{
    COREWEBVIEW2_PERMISSION_KIND, COREWEBVIEW2_PERMISSION_KIND_CAMERA, COREWEBVIEW2_PERMISSION_KIND_MICROPHONE,
    COREWEBVIEW2_PERMISSION_KIND_NOTIFICATIONS, COREWEBVIEW2_PERMISSION_STATE_ALLOW,
};
use webview2_com::{PermissionRequestedEventHandler, take_pwstr};
use windows::core::PWSTR;

use crate::instance;

/// The kinds of permission the instance gets without a prompt.
fn granted(kind: COREWEBVIEW2_PERMISSION_KIND) -> bool {
    kind == COREWEBVIEW2_PERMISSION_KIND_MICROPHONE
        || kind == COREWEBVIEW2_PERMISSION_KIND_CAMERA
        || kind == COREWEBVIEW2_PERMISSION_KIND_NOTIFICATIONS
}

/// Answers the window's permission requests for the instance in `home`.
pub fn grant_to_instance(window: &WebviewWindow, home: Arc<Mutex<Option<Url>>>) -> tauri::Result<()> {
    window.with_webview(move |webview| {
        let result = (|| -> windows::core::Result<()> {
            let core = unsafe { webview.controller().CoreWebView2() }?;
            let handler = PermissionRequestedEventHandler::create(Box::new(move |_, args| {
                let Some(args) = args else { return Ok(()) };
                let mut kind = COREWEBVIEW2_PERMISSION_KIND::default();
                unsafe { args.PermissionKind(&mut kind) }?;
                if !granted(kind) {
                    return Ok(());
                }
                let mut uri = PWSTR::null();
                unsafe { args.Uri(&mut uri) }?;
                let uri = take_pwstr(uri);
                let ours = Url::parse(&uri)
                    .ok()
                    .zip(home.lock().unwrap().clone())
                    .is_some_and(|(url, home)| instance::same_origin(&url, &home));
                if ours {
                    unsafe { args.SetState(COREWEBVIEW2_PERMISSION_STATE_ALLOW) }?;
                }
                Ok(())
            }));
            let mut token = 0i64;
            unsafe { core.add_PermissionRequested(&handler, &mut token) }
        })();
        if let Err(e) = result {
            eprintln!("permissions: could not watch permission requests: {e}");
        }
    })
}
