//! Shares a Windows monitor or window, with the sound of one application, to
//! a Zazzzor call: the first piece of the desktop client, on its own.
//!
//!   cargo run --release --example share -- list
//!   cargo run --release --example share -- share --window "Notepad" --server http://localhost:4173 --channel 123 --username me
//!
//! The password comes from ZAZZZOR_PASSWORD. See README.md.

mod join;

use std::borrow::Cow;

use std::sync::Arc;
use std::sync::mpsc::sync_channel;
use zazzzor_share::{audio, capture, mf, procs};

use anyhow::{Context, Result, bail};
use clap::{Args, Parser, Subcommand, ValueEnum};
use livekit::options::{AudioEncoding, TrackPublishOptions, VideoCodec, VideoEncoderBackend, VideoEncoding};
use livekit::prelude::*;
use livekit::webrtc::audio_frame::AudioFrame;
use livekit::webrtc::audio_source::native::NativeAudioSource;
use livekit::webrtc::audio_source::{AudioSourceOptions, RtcAudioSource};
use livekit::webrtc::video_source::native::NativeVideoSource;
use livekit::webrtc::video_source::{RtcVideoSource, VideoResolution};

use zazzzor_share::capture::{Nv12Frame, Shared, Target};
use zazzzor_share::encode::{VideoCounters, encode_hardware, encode_software};
use zazzzor_share::quality::Level;
use zazzzor_share::session::watch;

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
        Command::List => capture::list(),
        Command::Share(args) => {
            tokio::runtime::Builder::new_multi_thread().enable_all().build()?.block_on(share(*args))
        }
    }
}

async fn share(args: Share) -> Result<()> {
    let target = match (&args.monitor, &args.window) {
        (Some(m), None) => Target::Monitor(*m),
        (None, Some(w)) => Target::Window(w.clone()),
        (None, None) => Target::Monitor(1),
        _ => unreachable!("clap keeps them apart"),
    };
    let scope = audio_scope(&args, &target)?;

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

    // Choose the encoder before joining, so a broken one fails before anyone sees a share.
    let hardware = match args.encoder {
        EncoderChoice::Software => None,
        choice => {
            mf::startup()?;
            let found = mf::hardware_encoders()?;
            for c in &found {
                log::info!("hardware H.264 encoder: {}", c.name);
            }
            // COM objects stay on their thread: the encoding thread finds the encoder again by name.
            match found.into_iter().next() {
                Some(c) => Some(c.name),
                None if choice == EncoderChoice::Hardware => bail!("no hardware H.264 encoder on this machine"),
                None => {
                    log::warn!("no hardware H.264 encoder: falling back to software VP8");
                    None
                }
            }
        }
    };

    let mut options = RoomOptions::default();
    options.auto_subscribe = false;
    let (room, mut events) = Room::connect(&ticket.url, &ticket.token, options).await.context("connect to LiveKit")?;
    log::info!("joined room {} as {}", room.name(), room.local_participant().identity());
    // A companion is removed when its owner leaves the call; then the share is over.
    let (gone_tx, gone) = tokio::sync::oneshot::channel::<()>();
    tokio::spawn(async move {
        let mut gone_tx = Some(gone_tx);
        while let Some(event) = events.recv().await {
            if let RoomEvent::Disconnected { reason } = event {
                log::warn!("disconnected: {reason:?}");
                if let Some(tx) = gone_tx.take() {
                    let _ = tx.send(());
                }
            }
        }
    });

    let first = Level::P1080.bounds();
    let resolution = VideoResolution { width: first.0, height: first.1 };
    let source = match hardware {
        Some(_) => NativeVideoSource::new_encoded(resolution),
        None => NativeVideoSource::new(resolution, true),
    };
    let track = LocalVideoTrack::create_video_track("screen", RtcVideoSource::Native(source.clone()));
    room.local_participant()
        .publish_track(
            LocalTrack::Video(track.clone()),
            TrackPublishOptions {
                source: TrackSource::Screenshare,
                video_codec: if hardware.is_some() { VideoCodec::H264 } else { VideoCodec::VP8 },
                video_encoder: if hardware.is_some() {
                    VideoEncoderBackend::PreEncoded
                } else {
                    VideoEncoderBackend::Software
                },
                simulcast: false,
                video_encoding: Some(VideoEncoding {
                    max_bitrate: args.max_bitrate as u64,
                    max_framerate: args.fps as f64,
                }),
                degradation_preference: Some(
                    livekit::webrtc::rtp_parameters::DegradationPreference::MaintainResolution,
                ),
                ..Default::default()
            },
        )
        .await
        .context("publish the screen")?;

    let mut loopback = None;
    let mut audio_track = None;
    if let Some(scope) = scope {
        let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<i16>>(50);
        match audio::start(scope, tx) {
            Ok(l) => {
                let options =
                    AudioSourceOptions { echo_cancellation: false, noise_suppression: false, auto_gain_control: false };
                let src = NativeAudioSource::new(options, audio::SAMPLE_RATE, audio::CHANNELS, 100);
                let t = LocalAudioTrack::create_audio_track("screen-audio", RtcAudioSource::Native(src.clone()));
                room.local_participant()
                    .publish_track(
                        LocalTrack::Audio(t.clone()),
                        TrackPublishOptions {
                            source: TrackSource::ScreenshareAudio,
                            dtx: false,
                            red: false,
                            audio_encoding: Some(AudioEncoding { max_bitrate: 128_000 }),
                            ..Default::default()
                        },
                    )
                    .await
                    .context("publish the screen's sound")?;
                tokio::spawn(async move {
                    while let Some(chunk) = rx.recv().await {
                        let frame = AudioFrame {
                            data: Cow::Owned(chunk),
                            sample_rate: audio::SAMPLE_RATE,
                            num_channels: audio::CHANNELS,
                            samples_per_channel: audio::CHUNK_FRAMES as u32,
                        };
                        if let Err(e) = src.capture_frame(&frame).await {
                            log::warn!("audio: {e}");
                        }
                    }
                });
                log::info!("sharing the sound of {scope:?}");
                loopback = Some(l);
                audio_track = Some(t);
            }
            Err(e) => log::warn!("no sound: {e:#}"),
        }
    }
    let _ = &audio_track;

    let shared = Shared::default();
    let (frames_tx, frames_rx) = sync_channel::<Nv12Frame>(2);
    let counters = Arc::new(VideoCounters::default());
    let video = {
        let (source, counters, fps, bitrate) = (source.clone(), counters.clone(), args.fps, args.max_bitrate);
        std::thread::Builder::new().name("video-encode".into()).spawn(move || match hardware {
            Some(name) => encode_hardware(&name, frames_rx, &source, &counters, fps, bitrate),
            None => encode_software(frames_rx, &source, &counters),
        })?
    };
    let capture = capture::start(&target, args.fps, frames_tx, shared.clone())?;
    log::info!("sharing {target:?}; Ctrl+C stops");

    let summary = watch(&track, &shared, &counters, &loopback, args.seconds, gone).await;
    capture.stop();
    match video.join() {
        Ok(Err(e)) => log::error!("video: {e:#}"),
        Err(_) => log::error!("video: the encoder thread panicked"),
        Ok(Ok(())) => {}
    }
    if let Some(l) = loopback {
        l.stop();
    }
    room.close().await.ok();
    println!("{summary}");
    Ok(())
}

/// Whose sound goes with the picture.
fn audio_scope(args: &Share, target: &Target) -> Result<Option<audio::Scope>> {
    if args.no_audio {
        return Ok(None);
    }
    if let Some(pid) = args.audio_pid {
        return Ok(Some(audio::Scope::Only(pid)));
    }
    if let Some(exe) = &args.audio_process {
        return Ok(Some(audio::Scope::Only(procs::find(exe)?)));
    }
    if args.audio_system {
        return Ok(Some(audio::Scope::AllBut(std::process::id())));
    }
    match target {
        Target::Window(title) => Ok(Some(audio::Scope::Only(capture::window_process(title)?))),
        Target::Monitor(_) => Ok(None),
    }
}
