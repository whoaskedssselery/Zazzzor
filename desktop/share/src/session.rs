//! A screen share in a call: the capture, the encoder and the published
//! tracks, from `Session::start` to `Session::stop`, with its figures every
//! two seconds.

use std::borrow::Cow;
use std::sync::atomic::Ordering;
use std::sync::mpsc::sync_channel;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use anyhow::{Context, Result, bail};
use livekit::options::{AudioEncoding, TrackPublishOptions, VideoCodec, VideoEncoderBackend, VideoEncoding};
use livekit::prelude::*;
use livekit::webrtc::audio_frame::AudioFrame;
use livekit::webrtc::audio_source::native::NativeAudioSource;
use livekit::webrtc::audio_source::{AudioSourceOptions, RtcAudioSource};
use livekit::webrtc::stats::RtcStats;
use livekit::webrtc::video_source::native::NativeVideoSource;
use livekit::webrtc::video_source::{RtcVideoSource, VideoResolution};
use serde::Serialize;
use tokio::sync::oneshot;

use crate::audio::{self, Scope};
use crate::capture::{self, Nv12Frame, Shared, Target};
use crate::encode::{VideoCounters, encode_hardware, encode_software};
use crate::quality::{Level, Quality, Sample, fit};
use crate::{mf, procs};

/// Which encoder sends the picture.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Encoder {
    /// A hardware H.264 encoder when there is one, the software VP8 one otherwise.
    Auto,
    /// A hardware H.264 encoder or nothing.
    Hardware,
    /// libwebrtc's software VP8, as the browser sends it.
    Software,
}

#[derive(Clone, Copy, Debug)]
pub struct Options {
    pub encoder: Encoder,
    /// The most frames per second captured and sent.
    pub fps: u32,
    /// The most bit/s the video may use; WebRTC lowers it on a short link.
    pub max_bitrate: u32,
}

impl Default for Options {
    fn default() -> Self {
        Options { encoder: Encoder::Auto, fps: 60, max_bitrate: 10_000_000 }
    }
}

/// The share's figures over the last two seconds.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Stats {
    pub capture_fps: f64,
    pub encoded_fps: f64,
    pub sent_fps: f64,
    pub width: u32,
    pub height: u32,
    pub mbit_s: f64,
    /// WebRTC's estimate of what the link carries, in Mbit/s.
    pub estimate_mbit_s: Option<f64>,
    pub encoder: String,
    pub encode_ms: f64,
    pub convert_ms: f64,
    /// This process's CPU use, as a share of the whole machine.
    pub cpu_percent: f64,
    /// Frames dropped because the encoder was behind, since the start.
    pub dropped: u64,
    pub sound: bool,
    pub level: Level,
}

/// Why a share ended.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum End {
    /// `stop` was called.
    Stopped,
    /// The connection to the call ended: the server removed it (its owner
    /// left the call or reconnected) or the link broke.
    Disconnected,
    /// The shared window closed.
    SourceClosed,
}

/// A share in progress. Dropping it leaves it running: `stop` ends it.
pub struct Session {
    stop: Option<oneshot::Sender<()>>,
    task: tokio::task::JoinHandle<(End, String)>,
    stats: Arc<Mutex<Option<Stats>>>,
}

impl Session {
    /// Joins the room of `url` with `token`, publishes `target` (and the
    /// sound of `sound`, when it can be taken) and starts sending.
    pub async fn start(
        url: &str,
        token: &str,
        target: &Target,
        sound: Option<Scope>,
        options: Options,
    ) -> Result<Session> {
        let hardware = encoder(options.encoder)?;

        let mut room_options = RoomOptions::default();
        room_options.auto_subscribe = false;
        let (room, mut events) = Room::connect(url, token, room_options).await.context("connect to LiveKit")?;
        log::info!("joined room {} as {}", room.name(), room.local_participant().identity());
        let (gone_tx, gone) = oneshot::channel::<()>();
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

        let started = publish(&room, target, sound, options, hardware).await;
        let parts = match started {
            Ok(parts) => parts,
            Err(e) => {
                room.close().await.ok();
                return Err(e);
            }
        };
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let stats = Arc::new(Mutex::new(None));
        let task = tokio::spawn(run(room, parts, stats.clone(), stop_rx, gone));
        Ok(Session { stop: Some(stop_tx), task, stats })
    }

    /// The figures of the last two seconds; none in the first two.
    pub fn stats(&self) -> Option<Stats> {
        self.stats.lock().unwrap().clone()
    }

    /// Whether the share has ended on its own (see `End`).
    pub fn is_finished(&self) -> bool {
        self.task.is_finished()
    }

    /// Ends the share, if it has not ended, and says why it ended and what
    /// it sent on average.
    pub async fn stop(mut self) -> (End, String) {
        if let Some(tx) = self.stop.take() {
            let _ = tx.send(());
        }
        match (&mut self.task).await {
            Ok(done) => done,
            Err(_) => (End::Stopped, "the share's task failed".into()),
        }
    }
}

/// The hardware encoder to use by name, or none for the software one.
/// Chosen before joining, so a broken one fails before anyone sees a share.
fn encoder(choice: Encoder) -> Result<Option<String>> {
    if choice == Encoder::Software {
        return Ok(None);
    }
    mf::startup()?;
    let found = mf::hardware_encoders()?;
    for c in &found {
        log::info!("hardware H.264 encoder: {}", c.name);
    }
    // COM objects stay on their thread: the encoding thread finds the encoder again by name.
    match found.into_iter().next() {
        Some(c) => Ok(Some(c.name)),
        None if choice == Encoder::Hardware => bail!("no hardware H.264 encoder on this machine"),
        None => {
            log::warn!("no hardware H.264 encoder: falling back to software VP8");
            Ok(None)
        }
    }
}

/// Runs the share until it is stopped, the room goes or the window closes,
/// then takes everything down.
async fn run(
    room: Room,
    parts: Parts,
    stats: Arc<Mutex<Option<Stats>>>,
    mut stop: oneshot::Receiver<()>,
    mut gone: oneshot::Receiver<()>,
) -> (End, String) {
    const EVERY: Duration = Duration::from_secs(2);
    let Parts { track, shared, counters, capture, video, loopback } = parts;
    let cpus = std::thread::available_parallelism().map(|n| n.get()).unwrap_or(1) as f64;
    let mut quality = Quality::default();
    let mut prev = (0u64, 0u64, 0u64, 0u64, 0u64, procs::cpu_time(), Instant::now());
    let mut lines: Vec<(f64, f64, f64, f64)> = Vec::new();
    let mut tick = tokio::time::interval(EVERY);
    tick.tick().await;
    let end = loop {
        tokio::select! {
            _ = tick.tick() => {}
            _ = &mut stop => break End::Stopped,
            _ = &mut gone => break End::Disconnected,
        }
        if shared.closed.load(Ordering::Relaxed) {
            break End::SourceClosed;
        }
        let now = Instant::now();
        let dt = (now - prev.6).as_secs_f64();
        let captured = shared.captured.load(Ordering::Relaxed);
        let encoded = counters.encoded.load(Ordering::Relaxed);
        let bytes = counters.bytes.load(Ordering::Relaxed);
        let latency = counters.latency_us.load(Ordering::Relaxed);
        let convert = shared.convert_us.load(Ordering::Relaxed);
        let cpu = procs::cpu_time();

        let capture_fps = (captured - prev.0) as f64 / dt;
        let encoded_fps = (encoded - prev.1) as f64 / dt;
        let encode_ms = (latency - prev.3) as f64 / ((encoded - prev.1).max(1) as f64) / 1000.0;
        let convert_ms = (convert - prev.4) as f64 / ((captured - prev.0).max(1) as f64) / 1000.0;
        let cpu_percent = (cpu - prev.5) as f64 / 1e7 / dt / cpus * 100.0;
        let mbit_s = (bytes - prev.2) as f64 * 8.0 / dt / 1e6;

        let (mut sent_fps, mut bandwidth, mut size, mut encoder) = (encoded_fps, None, (0, 0), String::new());
        if let Ok(all) = track.get_stats().await {
            for s in all {
                match s {
                    RtcStats::OutboundRtp(o) if o.stream.kind == "video" => {
                        sent_fps = o.outbound.frames_per_second.max(0.0);
                        size = (o.outbound.frame_width, o.outbound.frame_height);
                        encoder = o.outbound.encoder_implementation.clone();
                    }
                    RtcStats::CandidatePair(c)
                        if c.candidate_pair.nominated && c.candidate_pair.available_outgoing_bitrate > 0.0 =>
                    {
                        bandwidth = Some(c.candidate_pair.available_outgoing_bitrate);
                    }
                    _ => {}
                }
            }
        }
        let before = quality.level;
        quality = quality.next(Sample { source_fps: capture_fps, sent_fps, bandwidth });
        if quality.level != before {
            log::warn!("quality: {before:?} -> {:?}", quality.level);
            shared.set_level(quality.level);
        }
        let now_stats = Stats {
            capture_fps,
            encoded_fps,
            sent_fps,
            width: size.0,
            height: size.1,
            mbit_s,
            estimate_mbit_s: bandwidth.map(|b| b / 1e6),
            encoder,
            encode_ms,
            convert_ms,
            cpu_percent,
            dropped: shared.dropped.load(Ordering::Relaxed),
            sound: loopback.is_some(),
            level: quality.level,
        };
        log::info!("{}", line(&now_stats, loopback.as_ref()));
        *stats.lock().unwrap() = Some(now_stats);
        lines.push((capture_fps, sent_fps, encode_ms, cpu_percent));
        prev = (captured, encoded, bytes, latency, convert, cpu, now);
    };

    capture.stop();
    // The encoder thread ends once the capture lets go of its frames.
    match tokio::task::spawn_blocking(move || video.join()).await {
        Ok(Ok(Err(e))) => log::error!("video: {e:#}"),
        Ok(Err(_)) | Err(_) => log::error!("video: the encoder thread panicked"),
        Ok(Ok(Ok(()))) => {}
    }
    if let Some(l) = loopback {
        let _ = tokio::task::spawn_blocking(move || l.stop()).await;
    }
    room.close().await.ok();
    log::info!("share ended: {end:?}");
    (end, summarize(&lines, &shared))
}

/// The log line of one reading.
fn line(s: &Stats, loopback: Option<&audio::Loopback>) -> String {
    let sound = match loopback {
        Some(l) => {
            let peak = l.peak.swap(0, Ordering::Relaxed);
            format!("sound peak {:.0} dBFS", 20.0 * (peak.max(1) as f64 / 32768.0).log10())
        }
        None => "no sound".into(),
    };
    format!(
        "capture {:5.1} fps | encoded {:5.1} | sent {:5.1} fps {}x{} ({}) | {:4.1} Mbit/s, estimate {} | \
         encode {:4.1} ms, convert {:4.1} ms | cpu {:4.1}% | dropped {} | {sound} | {:?}",
        s.capture_fps,
        s.encoded_fps,
        s.sent_fps,
        s.width,
        s.height,
        s.encoder,
        s.mbit_s,
        s.estimate_mbit_s.map(|b| format!("{b:.1} Mbit/s")).unwrap_or("-".into()),
        s.encode_ms,
        s.convert_ms,
        s.cpu_percent,
        s.dropped,
        s.level,
    )
}

fn summarize(lines: &[(f64, f64, f64, f64)], shared: &Shared) -> String {
    // The first sample covers the connection, not the steady state.
    let steady = if lines.len() > 2 { &lines[1..] } else { lines };
    let n = steady.len().max(1) as f64;
    let avg = |f: fn(&(f64, f64, f64, f64)) -> f64| steady.iter().map(f).sum::<f64>() / n;
    let min_sent = steady.iter().map(|l| l.1).fold(f64::INFINITY, f64::min);
    let (w, h) = fit(1920, 1080, shared.level());
    format!(
        "summary over {:.0} s: capture {:.1} fps, sent {:.1} fps (min {:.1}), encode {:.1} ms, cpu {:.1}%, \
         dropped {} frames, ended at {w}x{h} bounds",
        n * 2.0,
        avg(|l| l.0),
        avg(|l| l.1),
        if min_sent.is_finite() { min_sent } else { 0.0 },
        avg(|l| l.2),
        avg(|l| l.3),
        shared.dropped.load(Ordering::Relaxed),
    )
}

/// What a running share holds until it ends.
struct Parts {
    track: LocalVideoTrack,
    shared: Shared,
    counters: Arc<VideoCounters>,
    capture: capture::Capture,
    video: std::thread::JoinHandle<Result<()>>,
    loopback: Option<audio::Loopback>,
}

async fn publish(
    room: &Room,
    target: &Target,
    sound: Option<Scope>,
    options: Options,
    hardware: Option<String>,
) -> Result<Parts> {
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
                    max_bitrate: options.max_bitrate as u64,
                    max_framerate: options.fps as f64,
                }),
                degradation_preference: Some(
                    livekit::webrtc::rtp_parameters::DegradationPreference::MaintainResolution,
                ),
                ..Default::default()
            },
        )
        .await
        .context("publish the screen")?;

    let loopback = match sound {
        Some(scope) => publish_sound(room, scope).await,
        None => None,
    };

    let shared = Shared::default();
    let (frames_tx, frames_rx) = sync_channel::<Nv12Frame>(2);
    let counters = Arc::new(VideoCounters::default());
    let video = {
        let (source, counters, fps, bitrate) = (source.clone(), counters.clone(), options.fps, options.max_bitrate);
        std::thread::Builder::new().name("video-encode".into()).spawn(move || match hardware {
            Some(name) => encode_hardware(&name, frames_rx, &source, &counters, fps, bitrate),
            None => encode_software(frames_rx, &source, &counters),
        })?
    };
    let capture = capture::start(target, options.fps, frames_tx, shared.clone())?;
    log::info!("sharing {target:?}");
    Ok(Parts { track, shared, counters, capture, video, loopback })
}

/// Publishes the sound of `scope`; a share goes on without it when the
/// loopback cannot start (Windows before 10 2004, a process gone).
async fn publish_sound(room: &Room, scope: Scope) -> Option<audio::Loopback> {
    let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<i16>>(50);
    let loopback = match audio::start(scope, tx) {
        Ok(l) => l,
        Err(e) => {
            log::warn!("no sound: {e:#}");
            return None;
        }
    };
    let options = AudioSourceOptions { echo_cancellation: false, noise_suppression: false, auto_gain_control: false };
    let src = NativeAudioSource::new(options, audio::SAMPLE_RATE, audio::CHANNELS, 100);
    let track = LocalAudioTrack::create_audio_track("screen-audio", RtcAudioSource::Native(src.clone()));
    let published = room
        .local_participant()
        .publish_track(
            LocalTrack::Audio(track),
            TrackPublishOptions {
                source: TrackSource::ScreenshareAudio,
                dtx: false,
                red: false,
                audio_encoding: Some(AudioEncoding { max_bitrate: 128_000 }),
                ..Default::default()
            },
        )
        .await;
    if let Err(e) = published {
        log::warn!("no sound: publish: {e}");
        loopback.stop();
        return None;
    }
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
    Some(loopback)
}
