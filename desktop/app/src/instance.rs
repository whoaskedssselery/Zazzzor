//! The instance this app opens: its address as the person typed it, checked
//! and reduced to an origin, and kept in a small file in the user's app
//! config folder.

use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use url::Url;

use crate::lang::texts;

/// What the settings file holds.
#[derive(Debug, Default, Serialize, Deserialize)]
struct Settings {
    instance: Option<String>,
}

/// Turns what someone typed into the instance's origin, or says, in the
/// app's language for the setup page, why it is not one. A missing scheme means https, as in
/// a browser's address bar; a path is dropped, since the web client lives at
/// the root.
pub fn parse(input: &str) -> Result<Url, String> {
    let input = input.trim();
    if input.is_empty() {
        return Err(texts().enter_address.into());
    }
    let with_scheme = if input.contains("://") { input.to_owned() } else { format!("https://{input}") };
    let url = Url::parse(&with_scheme).map_err(|_| texts().not_an_address.to_owned())?;
    if url.scheme() != "https" && url.scheme() != "http" {
        return Err(texts().wrong_scheme.into());
    }
    if url.host_str().is_none_or(str::is_empty) {
        return Err(texts().no_host.into());
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err(texts().has_credentials.into());
    }
    let origin = url.origin().ascii_serialization();
    Url::parse(&format!("{origin}/")).map_err(|_| texts().not_an_address.to_owned())
}

/// Whether `url` belongs to the instance at `origin` (same scheme, host and port).
pub fn same_origin(url: &Url, origin: &Url) -> bool {
    url.origin() == origin.origin()
}

/// The settings file inside the app's config folder.
pub fn settings_path(config_dir: &Path) -> PathBuf {
    config_dir.join("settings.json")
}

/// The saved instance, if there is one and it still parses.
pub fn load(path: &Path) -> Option<Url> {
    let text = fs::read_to_string(path).ok()?;
    let settings: Settings = serde_json::from_str(&text).ok()?;
    parse(&settings.instance?).ok()
}

/// Saves `url` as the instance.
pub fn save(path: &Path, url: &Url) -> std::io::Result<()> {
    if let Some(dir) = path.parent() {
        fs::create_dir_all(dir)?;
    }
    let settings = Settings { instance: Some(url.to_string()) };
    fs::write(path, serde_json::to_string_pretty(&settings).expect("settings serialize"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_the_origin_and_drops_the_path() {
        assert_eq!(
            parse("https://zazzzor.example.org/channels/1/2?x=1").unwrap().as_str(),
            "https://zazzzor.example.org/"
        );
        assert_eq!(parse("http://localhost:4173").unwrap().as_str(), "http://localhost:4173/");
    }

    #[test]
    fn a_bare_host_means_https() {
        assert_eq!(parse("  zazzzor.duckdns.org ").unwrap().as_str(), "https://zazzzor.duckdns.org/");
    }

    #[test]
    fn refuses_what_is_not_a_web_address() {
        assert!(parse("").is_err());
        assert!(parse("ftp://example.org").is_err());
        assert!(parse("file:///C:/x").is_err());
        assert!(parse("https://").is_err());
        assert!(parse("https://me:secret@example.org").is_err());
        assert!(parse("javascript:alert(1)").is_err());
    }

    #[test]
    fn same_origin_needs_scheme_host_and_port() {
        let home = parse("https://zazzzor.example.org").unwrap();
        assert!(same_origin(&Url::parse("https://zazzzor.example.org/channels/@me").unwrap(), &home));
        assert!(!same_origin(&Url::parse("http://zazzzor.example.org/").unwrap(), &home));
        assert!(!same_origin(&Url::parse("https://zazzzor.example.org:8443/").unwrap(), &home));
        assert!(!same_origin(&Url::parse("https://evil.example.org/").unwrap(), &home));
    }

    #[test]
    fn saves_and_loads() {
        let dir = std::env::temp_dir().join(format!("zazzzor-settings-{}", std::process::id()));
        let path = settings_path(&dir);
        assert!(load(&path).is_none());
        let url = parse("https://zazzzor.example.org").unwrap();
        save(&path, &url).unwrap();
        assert_eq!(load(&path), Some(url));
        fs::remove_dir_all(dir).unwrap();
    }
}
