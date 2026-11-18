//! The screen share as a CLI, for measuring it outside the app:
//!
//!   cargo run --release --example share -- list
//!   cargo run --release --example share -- share --window "Notepad" --server http://localhost:4173 --channel 123 --username me
//!
//! The password comes from ZAZZZOR_PASSWORD. See README.md.

mod join;

use std::time::{Duration, Instant};

use anyhow::{Result, bail};
use clap::{Args, Parser, Subcommand, ValueEnum};
use zazzzor_share::audio::Scope;
use zazzzor_share::capture::{self, Target};
use zazzzor_share::session::{Encoder, Options, Session};
use zazzzor_share::{procs, sources};

#[derive(Parser)]
#[command(about = "Share a Windows screen or window, with one app's sound, to a Zazzzor call")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// List the monitors and windows that can be shared.
    List,
    /// Join a call and share.
    Share(Box<Share>),
}

#[derive(Args)]
struct Share {
    /// The monitor to share, counted from 1 (see `list`).
    #[arg(long, conflicts_with = "window")]
    monitor: Option<usize>,
    /// The window to share: part of its title.
    #[arg(long)]
    window: Option<String>,

    /// Share the sound of this process and its children.
    #[arg(long, conflicts_with_all = ["audio_process", "audio_system", "no_audio"])]
    audio_pid: Option<u32>,
    /// Share the sound of the process with this executable name (e.g. chrome.exe).
    #[arg(long, conflicts_with_all = ["audio_system", "no_audio"])]
    audio_process: Option<String>,
    /// Share all the sound of the system except this program's own.
    #[arg(long, conflicts_with = "no_audio")]
    audio_system: bool,
    /// Share no sound. With --window the default is the window's application.
    #[arg(long)]
    no_audio: bool,

    #[arg(long, value_enum, default_value_t = EncoderChoice::Auto)]
    encoder: EncoderChoice,
    /// The most frames per second captured and sent.
    #[arg(long, default_value_t = 60)]
    fps: u32,
    /// The most bit/s the video may use; WebRTC lowers it on a short link.
    #[arg(long, default_value_t = 10_000_000)]
    max_bitrate: u32,
    /// Stop after this many seconds and print a summary; runs until Ctrl+C otherwise.
    #[arg(long)]
    seconds: Option<u64>,

    /// The instance as the browser sees it, e.g. http://localhost:4173.
    #[arg(long, requires_all = ["channel", "username"])]
    server: Option<String>,
    /// The voice channel (or DM) to join.
    #[arg(long)]
    channel: Option<String>,
    #[arg(long)]
    username: Option<String>,
    #[arg(long, env = "ZAZZZOR_PASSWORD", hide_env_values = true)]
    password: Option<String>,
    /// Share as the screen of --username, who is already in the call in a
    /// browser: one person in the call, not a second participant.
    #[arg(long, requires = "server")]
    companion: bool,

    /// Connect straight to LiveKit instead of signing in (with --token).
    #[arg(long, conflicts_with = "server", requires = "token")]
    livekit_url: Option<String>,
    #[arg(long, env = "LIVEKIT_TOKEN", hide_env_values = true)]
    token: Option<String>,
}

#[derive(Clone, Copy, PartialEq, Eq, ValueEnum)]
enum EncoderChoice {
    /// A hardware H.264 encoder when there is one, the software VP8 one otherwise.
    Auto,
    /// A hardware H.264 encoder or nothing.
    Hardware,
    /// libwebrtc's software VP8, as the browser sends it.
    Software,
}

fn main() -> Result<()> {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info,livekit=warn,libwebrtc=warn"))
        .init();
    match Cli::parse().command {
        Command::List => list(),
        Command::Share(args) => {
            tokio::runtime::Builder::new_multi_thread().enable_all().build()?.block_on(share(*args))
        }
    }
}

fn list() -> Result<()> {
    for s in sources::list(std::process::id())? {
        let app = s.app.as_deref().unwrap_or("");
        println!("{:<24} {:>5}x{:<5} {app:<24} {}", s.id, s.width, s.height, s.name);
    }
    Ok(())
}

async fn share(args: Share) -> Result<()> {
    let target = match (&args.monitor, &args.window) {
        (Some(m), None) => Target::Monitor(*m),
        (None, Some(w)) => capture::find_window(w)?,
        (None, None) => Target::Monitor(1),
        _ => unreachable!("clap keeps them apart"),
    };
    let sound = sound(&args, &target)?;

    let ticket = match (&args.server, &args.livekit_url) {
        (Some(server), _) => {
            let Some(password) = &args.password else { bail!("set ZAZZZOR_PASSWORD to sign in") };
            let (channel, username) = (args.channel.as_deref().unwrap(), args.username.as_deref().unwrap());
            let role = if args.companion { join::Role::Companion } else { join::Role::Participant };
            join::join(server, username, password, channel, role)?
        }
        (None, Some(url)) => join::Ticket { url: url.clone(), token: args.token.clone().unwrap() },
        (None, None) => bail!("say where to share: --server … or --livekit-url … --token …"),
    };

    let encoder = match args.encoder {
        EncoderChoice::Auto => Encoder::Auto,
        EncoderChoice::Hardware => Encoder::Hardware,
        EncoderChoice::Software => Encoder::Software,
    };
    let options = Options { encoder, fps: args.fps, max_bitrate: args.max_bitrate };
    let session = Session::start(&ticket.url, &ticket.token, &target, sound, options).await?;
    log::info!("Ctrl+C stops");

    // The session logs a line every two seconds; this waits for a reason to stop.
    let started = Instant::now();
    let mut tick = tokio::time::interval(Duration::from_millis(250));
    loop {
        tokio::select! {
            _ = tick.tick() => {}
            _ = tokio::signal::ctrl_c() => break,
        }
        if session.is_finished() || args.seconds.is_some_and(|s| started.elapsed() >= Duration::from_secs(s)) {
            break;
        }
    }
    let (end, summary) = session.stop().await;
    println!("{summary} ({end:?})");
    Ok(())
}

/// Whose sound goes with the picture.
fn sound(args: &Share, target: &Target) -> Result<Option<Scope>> {
    if args.no_audio {
        return Ok(None);
    }
    if let Some(pid) = args.audio_pid {
        return Ok(Some(Scope::Only(pid)));
    }
    if let Some(exe) = &args.audio_process {
        return Ok(Some(Scope::Only(procs::find(exe)?)));
    }
    if args.audio_system {
        return Ok(Some(Scope::AllBut(std::process::id())));
    }
    match target {
        Target::Window(hwnd) => Ok(Some(Scope::Only(capture::window_process(*hwnd)?))),
        Target::Monitor(_) => Ok(None),
    }
}
