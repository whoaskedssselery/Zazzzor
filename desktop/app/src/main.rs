//! Zazzzor for Windows: one window that opens the instance's own web client,
//! as Discord's desktop app opens discord.com, with a tray icon.
//!
//! The instance's pages are remote content and get no access to the app: the
//! only pages that may call the commands below are the bundled setup page
//! (`setup/`), where the person types the instance's address. Links to other
//! sites open in the default browser.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod autostart;
mod bridge;
mod hotkeys;
mod instance;
#[cfg(windows)]
mod permissions;
mod share;
mod update;
#[cfg(test)]
mod update_tests;

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem};
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
    bridge::allow_instance(window.app_handle(), &url).map_err(|e| e.to_string())?;
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

/// The tray's "Остановить демонстрацию", enabled while the app shares a screen.
struct StopShare(MenuItem<tauri::Wry>);

/// Shows in the tray whether the app shares a screen now: the tooltip says
/// so and the menu can stop it, so a share is never out of sight.
pub fn sharing_shown(app: &AppHandle, on: bool) {
    if let Some(item) = app.try_state::<StopShare>() {
        let _ = item.0.set_enabled(on);
    }
    if let Some(tray) = app.tray_by_id(MAIN) {
        let _ = tray.set_tooltip(Some(if on {
            "Zazzzor — идёт демонстрация экрана"
        } else {
            "Zazzzor"
        }));
    }
}

fn tray(app: &AppHandle) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open", "Открыть Zazzzor", true, None::<&str>)?;
    let stop_share = MenuItem::with_id(app, "stop-share", "Остановить демонстрацию", false, None::<&str>)?;
    app.manage(StopShare(stop_share.clone()));
    let change = MenuItem::with_id(app, "change", "Сменить сервер…", true, None::<&str>)?;
    let startup = CheckMenuItem::with_id(
        app,
        "autostart",
        "Запускать вместе с Windows",
        true,
        autostart::enabled(),
        None::<&str>,
    )?;
    let updates = MenuItem::with_id(app, "check-update", "Проверить обновления", true, None::<&str>)?;
    let separator = PredefinedMenuItem::separator(app)?;
    let exit = MenuItem::with_id(app, "quit", "Выйти", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &stop_share, &change, &startup, &updates, &separator, &exit])?;
    let mut builder = TrayIconBuilder::with_id(MAIN)
        .tooltip("Zazzzor")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(move |app, event| match event.id.as_ref() {
            "open" => show(app),
            // The page learns of it from the share's status, as of a share that ended.
            "stop-share" => {
                let app = app.clone();
                tauri::async_runtime::spawn(async move { share::stop(&app).await });
            }
            "change" => change_instance(app),
            // The tick follows what Windows will do, whatever the click managed.
            "autostart" => {
                let _ = autostart::set(!autostart::enabled());
                let _ = startup.set_checked(autostart::enabled());
            }
            "check-update" => update::check(app),
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
    #[cfg(windows)]
    set_big_icon(&window);
    Ok(window)
}

/// Gives the window its large icon, which the taskbar and Alt+Tab show.
/// Tauri sets only the small one, and without a large one Windows takes the
/// exe's icon from its icon cache: after an update with a new icon the
/// taskbar kept the old one. The icon is the exe's own, at the system's size.
#[cfg(windows)]
fn set_big_icon(window: &WebviewWindow) {
    use windows::Win32::Foundation::{HWND, LPARAM, WPARAM};
    use windows::Win32::System::LibraryLoader::GetModuleHandleW;
    use windows::Win32::UI::WindowsAndMessaging::{
        GetSystemMetrics, ICON_BIG, IMAGE_ICON, LR_DEFAULTCOLOR, LoadImageW, SM_CXICON, SM_CYICON, SendMessageW,
        WM_SETICON,
    };
    use windows::core::PCWSTR;

    let Ok(hwnd) = window.hwnd() else { return };
    // SAFETY: the module handle is this process's own exe; the resource id is
    // an integer resource (MAKEINTRESOURCE); the icon lives as long as the process.
    unsafe {
        let Ok(module) = GetModuleHandleW(None) else { return };
        let id = PCWSTR(usize::from(tauri::utils::platform::WINDOWS_APP_ICON_RESOURCE_ID) as *const u16);
        let (w, h) = (GetSystemMetrics(SM_CXICON), GetSystemMetrics(SM_CYICON));
        let Ok(icon) = LoadImageW(Some(module.into()), id, IMAGE_ICON, w, h, LR_DEFAULTCOLOR) else { return };
        SendMessageW(HWND(hwnd.0), WM_SETICON, Some(WPARAM(ICON_BIG as usize)), Some(LPARAM(icon.0 as isize)));
    }
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
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_dialog::init())
        .manage(share::Sharing::default())
        .manage(hotkeys::Hotkeys::default())
        .invoke_handler(tauri::generate_handler![
            current_instance,
            set_instance,
            open_instance,
            share::share_sources,
            share::share_thumbnail,
            share::share_start,
            share::share_stop,
            share::share_status,
            hotkeys::hotkeys_ptt,
            hotkeys::hotkeys_toggles
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
                bridge::allow_instance(app.handle(), url)?;
            }
            hotkeys::deliver(app.handle());
            tray(app.handle())?;
            let window = window(app.handle(), saved, shared.clone(), setup_requested)?;
            #[cfg(windows)]
            permissions::grant_to_instance(&window, shared)?;
            // Started by Windows at sign-in: stay in the tray until called.
            if !std::env::args().any(|a| a == autostart::FLAG) {
                window.show()?;
                window.set_focus()?;
            }
            update::start(app.handle())?;
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
