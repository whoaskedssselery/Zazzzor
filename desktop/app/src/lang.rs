//! The language of the app's own texts: the tray, the update dialogs and the
//! setup page. It is the language of the instance's pages, which report it
//! (`set_language`) when they load and whenever it changes; it is kept for
//! the next start, and before the first report it is Windows' own.

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU8, Ordering};

use tauri::{Manager, State, WebviewWindow};

use crate::bridge::from_instance;

/// The commands the instance's pages may call (bridge.rs); each checks
/// `from_instance` first.
pub const COMMANDS: [&str; 1] = ["set_language"];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Lang {
    En = 0,
    Ru = 1,
}

impl Lang {
    pub fn parse(code: &str) -> Option<Self> {
        match code {
            "en" => Some(Self::En),
            "ru" => Some(Self::Ru),
            _ => None,
        }
    }

    pub fn code(self) -> &'static str {
        match self {
            Self::En => "en",
            Self::Ru => "ru",
        }
    }
}

static CURRENT: AtomicU8 = AtomicU8::new(Lang::En as u8);

pub fn current() -> Lang {
    if CURRENT.load(Ordering::Relaxed) == Lang::Ru as u8 { Lang::Ru } else { Lang::En }
}

pub fn set(lang: Lang) {
    CURRENT.store(lang as u8, Ordering::Relaxed);
}

/// Where the language is kept between starts, beside the instance's file.
pub fn path(config_dir: &Path) -> PathBuf {
    config_dir.join("language")
}

pub fn load(path: &Path) -> Option<Lang> {
    fs::read_to_string(path).ok().and_then(|s| Lang::parse(s.trim()))
}

pub fn save(path: &Path, lang: Lang) -> std::io::Result<()> {
    if let Some(dir) = path.parent() {
        fs::create_dir_all(dir)?;
    }
    fs::write(path, lang.code())
}

/// Windows' display language: Russian for a Russian Windows, English otherwise.
pub fn system() -> Lang {
    #[cfg(windows)]
    {
        // SAFETY: a plain query with no arguments.
        let id = unsafe { windows::Win32::Globalization::GetUserDefaultUILanguage() };
        // The primary language is the low ten bits of a LANGID; 0x19 is Russian.
        if id & 0x3ff == 0x19 {
            return Lang::Ru;
        }
    }
    Lang::En
}

/// The app's own texts in one language.
pub struct Texts {
    pub open: &'static str,
    pub stop_share: &'static str,
    pub change_server: &'static str,
    pub autostart: &'static str,
    pub check_updates: &'static str,
    pub quit: &'static str,
    pub sharing_tooltip: &'static str,
    /// «{version}» is replaced with the new version.
    update_available: &'static str,
    pub update_yes: &'static str,
    pub update_later: &'static str,
    pub up_to_date: &'static str,
    pub enter_address: &'static str,
    pub not_an_address: &'static str,
    pub wrong_scheme: &'static str,
    pub no_host: &'static str,
    pub has_credentials: &'static str,
    pub needs_https: &'static str,
    /// Followed by the error.
    pub save_failed: &'static str,
    pub no_instance: &'static str,
    /// «{host}» is the instance's host, «{name}» the window's title.
    share_ask_window: &'static str,
    /// «{host}» is the instance's host, «{n}» the screen's number.
    share_ask_screen: &'static str,
    pub share_yes: &'static str,
    pub share_no: &'static str,
}

impl Texts {
    pub fn update_available(&self, version: &str) -> String {
        self.update_available.replace("{version}", version)
    }

    /// The question before the instance at `host` shares the window titled
    /// `name` (the host goes in first, so a title cannot pose as it).
    pub fn share_ask_window(&self, name: &str, host: &str) -> String {
        self.share_ask_window.replace("{host}", host).replace("{name}", name)
    }

    /// The question before the instance at `host` shares screen `n`.
    pub fn share_ask_screen(&self, n: usize, host: &str) -> String {
        self.share_ask_screen.replace("{host}", host).replace("{n}", &n.to_string())
    }
}

const EN: Texts = Texts {
    open: "Open Zazzzor",
    stop_share: "Stop sharing",
    change_server: "Change server…",
    autostart: "Start with Windows",
    check_updates: "Check for updates",
    quit: "Quit",
    sharing_tooltip: "Zazzzor — sharing the screen",
    update_available: "Zazzzor {version} is available. Restart and install it?",
    update_yes: "Yes",
    update_later: "Later",
    up_to_date: "You have the latest version",
    enter_address: "Enter the server's address.",
    not_an_address: "This doesn't look like a website address.",
    wrong_scheme: "The address must start with https:// or http://.",
    no_host: "The address has no server name.",
    has_credentials: "The address must not contain a username or password.",
    needs_https: "The address must start with https://. Plain http:// is only for a server on this computer (localhost).",
    save_failed: "Couldn't save the address",
    no_instance: "No server chosen yet.",
    share_ask_window: "{host} wants to show your window “{name}” in the call. Share it?",
    share_ask_screen: "{host} wants to show your screen {n} in the call. Share it?",
    share_yes: "Share",
    share_no: "Cancel",
};

const RU: Texts = Texts {
    open: "Открыть Zazzzor",
    stop_share: "Остановить демонстрацию",
    change_server: "Сменить сервер…",
    autostart: "Запускать вместе с Windows",
    check_updates: "Проверить обновления",
    quit: "Выйти",
    sharing_tooltip: "Zazzzor — идёт демонстрация экрана",
    update_available: "Доступно обновление Zazzzor {version}. Перезапустить и установить?",
    update_yes: "Да",
    update_later: "Позже",
    up_to_date: "Установлена последняя версия",
    enter_address: "Введите адрес сервера.",
    not_an_address: "Это не похоже на адрес сайта.",
    wrong_scheme: "Адрес должен начинаться с https:// или http://.",
    no_host: "В адресе нет имени сервера.",
    has_credentials: "Адрес не должен содержать логин или пароль.",
    needs_https: "Адрес должен начинаться с https://. Простой http:// — только для сервера на этом компьютере (localhost).",
    save_failed: "Не удалось сохранить адрес",
    no_instance: "Сервер ещё не выбран.",
    share_ask_window: "{host} хочет показать в звонке ваше окно «{name}». Показать?",
    share_ask_screen: "{host} хочет показать в звонке ваш экран {n}. Показать?",
    share_yes: "Показать",
    share_no: "Отмена",
};

/// The app's texts in the current language.
pub fn texts() -> &'static Texts {
    texts_in(current())
}

/// The app's texts in `lang`.
pub fn texts_in(lang: Lang) -> &'static Texts {
    match lang {
        Lang::En => &EN,
        Lang::Ru => &RU,
    }
}

/// The instance's pages report their language: the app's own texts follow
/// it at once, and it is kept for the next start.
#[tauri::command]
pub fn set_language(lang: String, window: WebviewWindow, app: State<'_, crate::App>) -> Result<(), String> {
    from_instance(&window, &app.instance)?;
    let Some(lang) = Lang::parse(&lang) else { return Err("unknown language".into()) };
    if lang == current() {
        return Ok(());
    }
    set(lang);
    let _ = save(&app.language, lang);
    crate::tray_relabel(window.app_handle());
    Ok(())
}

/// The setup page asks which language to show itself in.
#[tauri::command]
pub fn current_language(window: WebviewWindow) -> Result<&'static str, String> {
    crate::from_setup(&window)?;
    Ok(current().code())
}

/// Sets the language at start: the kept one, else Windows' own.
pub fn init(path: &Path) {
    set(load(path).unwrap_or_else(system));
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_the_language_between_starts() {
        let dir = std::env::temp_dir().join(format!("zazzzor-lang-{}", std::process::id()));
        let file = path(&dir);
        assert_eq!(load(&file), None);
        save(&file, Lang::Ru).unwrap();
        assert_eq!(load(&file), Some(Lang::Ru));
        fs::write(&file, "klingon").unwrap();
        assert_eq!(load(&file), None);
        let _ = fs::remove_dir_all(dir);
    }

    #[test]
    fn switches_the_texts() {
        set(Lang::Ru);
        assert_eq!(texts().quit, "Выйти");
        assert_eq!(texts().update_available("0.3.0"), "Доступно обновление Zazzzor 0.3.0. Перезапустить и установить?");
        set(Lang::En);
        assert_eq!(texts().quit, "Quit");
    }
}
