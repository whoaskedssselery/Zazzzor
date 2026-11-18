use std::cell::{Cell, RefCell};
use std::sync::{Arc, Mutex, mpsc};
use std::time::{Duration, Instant};

use crate::update::{Backend, next_check, perform_check, validate_asset};

#[derive(Default)]
struct Fake {
    version: Cell<Option<&'static str>>,
    invalid_signature: Cell<bool>,
    network_failure: Cell<bool>,
    accept: Cell<bool>,
    events: RefCell<Vec<&'static str>>,
}

impl Backend for Fake {
    type Candidate = &'static str;

    async fn check(&self) -> Result<Option<Self::Candidate>, String> {
        self.events.borrow_mut().push("check");
        if self.network_failure.get() {
            return Err("network failure".into());
        }
        Ok(self.version.get())
    }

    fn version<'a>(&self, candidate: &'a Self::Candidate) -> &'a str {
        candidate
    }

    async fn download(&self, _: &Self::Candidate) -> Result<Vec<u8>, String> {
        self.events.borrow_mut().push("verify");
        if self.invalid_signature.get() {
            return Err("invalid signature".into());
        }
        Ok(vec![1])
    }

    fn confirm(&self, _: &str) -> bool {
        self.events.borrow_mut().push("ask");
        self.accept.get()
    }

    fn install(&self, _: &Self::Candidate, data: Vec<u8>) -> Result<(), String> {
        assert_eq!(data, vec![1], "installer must receive verified bytes");
        self.events.borrow_mut().push("install");
        Ok(())
    }

    fn latest(&self) {
        self.events.borrow_mut().push("latest");
    }
}

fn run(fake: &Fake, offered: &mut bool, manual: bool) -> Result<(), String> {
    let (_tx, rx) = mpsc::sync_channel(1);
    tauri::async_runtime::block_on(perform_check(fake, offered, manual, &rx))
}

#[test]
fn automatic_latest_is_quiet_and_manual_latest_answers() {
    let fake = Fake::default();
    let mut offered = false;
    run(&fake, &mut offered, false).unwrap();
    assert_eq!(*fake.events.borrow(), ["check"]);
    run(&fake, &mut offered, true).unwrap();
    assert_eq!(*fake.events.borrow(), ["check", "check", "latest"]);
}

#[test]
fn signature_failure_does_not_prompt_or_install_and_can_retry() {
    let fake = Fake { version: Cell::new(Some("0.2.1")), invalid_signature: Cell::new(true), ..Default::default() };
    let mut offered = false;
    assert!(run(&fake, &mut offered, false).is_err());
    assert!(!offered, "failed verification must not suppress a later valid update");
    assert_eq!(*fake.events.borrow(), ["check", "verify"]);
    fake.invalid_signature.set(false);
    run(&fake, &mut offered, false).unwrap();
    assert_eq!(*fake.events.borrow(), ["check", "verify", "check", "verify", "ask"]);
}

#[test]
fn later_prevents_every_further_prompt_until_restart() {
    let fake = Fake { version: Cell::new(Some("0.2.1")), ..Default::default() };
    let mut offered = false;
    run(&fake, &mut offered, false).unwrap();
    fake.version.set(Some("0.2.2"));
    run(&fake, &mut offered, true).unwrap();
    assert_eq!(*fake.events.borrow(), ["check", "verify", "ask", "check"]);
}

#[test]
fn install_follows_verified_download_and_explicit_acceptance() {
    let fake = Fake { version: Cell::new(Some("0.2.1")), accept: Cell::new(true), ..Default::default() };
    run(&fake, &mut false, false).unwrap();
    assert_eq!(*fake.events.borrow(), ["check", "verify", "ask", "install"]);
}

struct Blocked {
    entered: mpsc::SyncSender<()>,
    release: mpsc::Receiver<()>,
    events: Arc<Mutex<Vec<&'static str>>>,
}

impl Backend for Blocked {
    type Candidate = &'static str;
    async fn check(&self) -> Result<Option<Self::Candidate>, String> {
        self.events.lock().unwrap().push("check");
        self.entered.send(()).unwrap();
        self.release.recv().unwrap();
        Ok(None)
    }
    fn version<'a>(&self, candidate: &'a Self::Candidate) -> &'a str {
        candidate
    }
    async fn download(&self, _: &Self::Candidate) -> Result<Vec<u8>, String> {
        unreachable!()
    }
    fn confirm(&self, _: &str) -> bool {
        unreachable!()
    }
    fn install(&self, _: &Self::Candidate, _: Vec<u8>) -> Result<(), String> {
        unreachable!()
    }
    fn latest(&self) {
        self.events.lock().unwrap().push("latest");
    }
}

#[test]
fn manual_burst_during_background_check_is_coalesced_into_its_result() {
    for manual in [false, true] {
        let (entered, wait) = mpsc::sync_channel(1);
        let (release, resume) = mpsc::sync_channel(1);
        let (request, pending) = mpsc::sync_channel(1);
        let events = Arc::new(Mutex::new(Vec::new()));
        let backend = Blocked { entered, release: resume, events: events.clone() };
        let worker = std::thread::spawn(move || {
            tauri::async_runtime::block_on(perform_check(&backend, &mut false, manual, &pending)).unwrap();
            assert!(pending.try_recv().is_err(), "manual requests must not cause a second download");
        });
        wait.recv().unwrap();
        request.try_send(()).unwrap();
        assert!(matches!(request.try_send(()), Err(mpsc::TrySendError::Full(()))));
        release.send(()).unwrap();
        worker.join().unwrap();
        assert_eq!(*events.lock().unwrap(), ["check", "latest"]);
    }
}

#[test]
fn manual_network_failure_is_quiet_and_does_not_block_a_retry() {
    let fake = Fake { network_failure: Cell::new(true), ..Default::default() };
    let mut offered = false;
    assert!(run(&fake, &mut offered, true).is_err());
    assert_eq!(*fake.events.borrow(), ["check"]);
    fake.network_failure.set(false);
    run(&fake, &mut offered, true).unwrap();
    assert_eq!(*fake.events.borrow(), ["check", "check", "latest"]);
}

#[test]
fn manual_checks_do_not_postpone_the_six_hour_schedule() {
    let start = Instant::now();
    let due = next_check(start, start);
    assert_eq!(due.duration_since(start), Duration::from_secs(6 * 60 * 60));
    assert_eq!(next_check(start + Duration::from_secs(60), due), due);
    assert_eq!(next_check(due, due).duration_since(due), Duration::from_secs(6 * 60 * 60));
}

#[test]
fn assets_stay_in_the_configured_repository_release_and_version() {
    let source =
        url::Url::parse("https://github.com/whoaskedssselery/Zazzzor/releases/latest/download/latest.json").unwrap();
    for (asset, valid) in [
        (
            "https://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/Zazzzor_0.2.1_x64-setup.exe",
            true,
        ),
        ("https://evil.example/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.exe", false),
        ("http://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.exe", false),
        ("https://github.com/evil/Zazzzor/releases/download/desktop-v0.2.1/setup.exe", false),
        ("https://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.0/setup.exe", false),
        (
            "https://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.exe?token=anything",
            false,
        ),
        ("https://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.exe#fragment", false),
        ("https://user@github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.exe", false),
        ("https://:password@github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.exe", false),
        ("https://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.1/setup.msi", false),
    ] {
        let result = validate_asset(&source, &url::Url::parse(asset).unwrap(), "0.2.1");
        assert_eq!(result.is_ok(), valid, "asset = {asset}");
    }
}
