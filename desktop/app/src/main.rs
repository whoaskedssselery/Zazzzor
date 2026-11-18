//! Zazzzor for Windows: one window that opens the instance's own web client,
//! as Discord's desktop app opens discord.com, with a tray icon.
//!
//! The instance's pages are remote content and get no access to the app: the
//! only pages that may call the commands below are the bundled setup page
//! (`setup/`), where the person types the instance's address. Links to other
//! sites open in the default browser.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod instance;
#[cfg(windows)]
mod permissions;
mod share;

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::webview::NewWindowResponse;
use tauri::{AppHandle, Manager, State, Url, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_opener::OpenerExt;
use tauri_plugin_window_state::{AppHandleExt, StateFlags};

const MAIN: &str = "main";

/// Where the bundled setup page is served on Windows.
const SETUP_URL: &str = "http://tauri.localhost/index.html";

/// Everything but visibility: the app decides when the window shows.
fn state_flags() -> StateFlags {
    StateFlags::all() & !StateFlags::VISIBLE
}

struct App {
    settings: PathBuf,
    /// The instance the window belongs to; shared with the navigation filter.
    instance: Arc<Mutex<Option<Url>>>,
    /// Set by "Выйти" so that closing the window really closes it.
    quitting: AtomicBool,
    /// Set just before the app itself opens the setup page: the instance's
    /// pages cannot navigate to it, since it is the one page with commands.
    setup_requested: Arc<AtomicBool>,
}

/// Whether `url` is one of the app's own bundled pages.
fn is_bundled(url: &Url) -> bool {
    url.host_str() == Some("tauri.localhost") || url.scheme() == "tauri"
}

/// Refuses a command called from anywhere but the bundled setup page.
fn from_setup(window: &WebviewWindow) -> Result<(), String> {
    match window.url() {
        Ok(url) if is_bundled(&url) => Ok(()),
        _ => Err("not allowed".into()),
    }
}

/// The saved instance's address, for the setup page to show.
#[tauri::command]
fn current_instance(window: WebviewWindow, app: State<'_, App>) -> Result<Option<String>, String> {
    from_setup(&window)?;
    Ok(app.instance.lock().unwrap().as_ref().map(Url::to_string))
}

/// Checks and saves the address the person typed, then opens it.
#[tauri::command]
fn set_instance(url: String, window: WebviewWindow, app: State<'_, App>) -> Result<(), String> {
    from_setup(&window)?;
    let url = instance::parse(&url)?;
    instance::save(&app.settings, &url).map_err(|e| format!("Не удалось сохранить адрес: {e}"))?;
    *app.instance.lock().unwrap() = Some(url.clone());
    share::allow_instance(window.app_handle(), &url).map_err(|e| e.to_string())?;
    app.setup_requested.store(false, Ordering::Relaxed);
    window.navigate(url).map_err(|e| e.to_string())
}

/// Goes back to the saved instance without changing it.
#[tauri::command]
fn open_instance(window: WebviewWindow, app: State<'_, App>) -> Result<(), String> {
    from_setup(&window)?;
    let Some(url) = app.instance.lock().unwrap().clone() else {
        return Err("Сервер ещё не выбран.".into());
    };
    app.setup_requested.store(false, Ordering::Relaxed);
    window.navigate(url).map_err(|e| e.to_string())
}

/// Brings the window back from the tray or from behind other windows.
fn show(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(MAIN) {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

/// Opens the setup page in the window, to change the instance.
fn change_instance(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(MAIN) {
        app.state::<App>().setup_requested.store(true, Ordering::Relaxed);
        let _ = window.navigate(Url::parse(SETUP_URL).expect("setup URL"));
    }
    show(app);
}

fn quit(app: &AppHandle) {
    app.state::<App>().quitting.store(true, Ordering::Relaxed);
    let _ = app.save_window_state(state_flags());
    app.exit(0);
}

fn tray(app: &AppHandle) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open", "Открыть Zazzzor", true, None::<&str>)?;
    let change = MenuItem::with_id(app, "change", "Сменить сервер…", true, None::<&str>)?;
    let separator = PredefinedMenuItem::separator(app)?;
    let exit = MenuItem::with_id(app, "quit", "Выйти", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &change, &separator, &exit])?;
    let mut builder = TrayIconBuilder::with_id(MAIN)
        .tooltip("Zazzzor")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "open" => show(app),
            "change" => change_instance(app),
            "quit" => quit(app),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. } = event {
                show(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    Ok(())
}

fn window(
    app: &AppHandle,
    instance: Option<Url>,
    shared: Arc<Mutex<Option<Url>>>,
    setup_requested: Arc<AtomicBool>,
) -> tauri::Result<WebviewWindow> {
    let start = match instance {
        Some(url) => WebviewUrl::External(url),
        None => WebviewUrl::App("index.html".into()),
    };
    let opener = app.clone();
    let new_window_opener = app.clone();
    let window = WebviewWindowBuilder::new(app, MAIN, start)
        .title("Zazzzor")
        .inner_size(1280.0, 800.0)
        .min_inner_size(940.0, 560.0)
        .visible(false)
        // Pages of the instance stay here; any other site opens in the browser.
        .on_navigation(move |url| {
            if is_bundled(url) {
                return setup_requested.swap(false, Ordering::Relaxed);
            }
            if url.scheme() == "about" {
                return true;
            }
            if shared.lock().unwrap().as_ref().is_some_and(|home| instance::same_origin(url, home)) {
                return true;
            }
            open_outside(&opener, url);
            false
        })
        // Links with target=_blank and window.open go to the browser too.
        .on_new_window(move |url, _features| {
            open_outside(&new_window_opener, &url);
            NewWindowResponse::Deny
        })
        .build()?;

    let handle = app.clone();
    window.on_window_event(move |event| {
        if let WindowEvent::CloseRequested { api, .. } = event {
            let app = handle.state::<App>();
            if app.quitting.load(Ordering::Relaxed) {
                return;
            }
            // Closing hides to the tray; "Выйти" in the tray menu quits.
            api.prevent_close();
            let _ = handle.save_window_state(state_flags());
            if let Some(window) = handle.get_webview_window(MAIN) {
                let _ = window.hide();
            }
        }
    });
    Ok(window)
}

/// Opens web and mail links in the person's default programs; anything else is ignored.
fn open_outside(app: &AppHandle, url: &Url) {
    if matches!(url.scheme(), "https" | "http" | "mailto") {
        let _ = app.opener().open_url(url.as_str(), None::<&str>);
    }
}

fn main() {
    tauri::Builder::default()
        // First: a second launch hands over to the running one and exits.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| show(app)))
        .plugin(tauri_plugin_window_state::Builder::default().with_state_flags(state_flags()).build())
        .plugin(tauri_plugin_opener::init())
        .manage(share::Sharing::default())
        .invoke_handler(tauri::generate_handler![
            current_instance,
            set_instance,
            open_instance,
            share::share_sources,
            share::share_thumbnail,
            share::share_start,
            share::share_stop,
            share::share_status
        ])
        .setup(|app| {
            let settings = instance::settings_path(&app.path().app_config_dir()?);
            let saved = instance::load(&settings);
            let shared = Arc::new(Mutex::new(saved.clone()));
            // Without a saved instance the window starts on the setup page.
            let setup_requested = Arc::new(AtomicBool::new(saved.is_none()));
            app.manage(App {
                settings,
                instance: shared.clone(),
                quitting: AtomicBool::new(false),
                setup_requested: setup_requested.clone(),
            });
            if let Some(url) = &saved {
                share::allow_instance(app.handle(), url)?;
            }
            tray(app.handle())?;
            let window = window(app.handle(), saved, shared.clone(), setup_requested)?;
            #[cfg(windows)]
            permissions::grant_to_instance(&window, shared)?;
            window.show()?;
            window.set_focus()?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("the app failed to start")
        .run(|app, event| {
            if let tauri::RunEvent::Exit = event {
                share::stop_on_exit(app);
            }
        });
}
