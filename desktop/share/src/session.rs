//! A screen share in a call: its figures every two seconds and the quality
//! rule they drive.

use std::sync::atomic::Ordering;
use std::time::{Duration, Instant};

use livekit::prelude::*;
use livekit::webrtc::stats::RtcStats;

use crate::audio;
use crate::capture::Shared;
use crate::encode::VideoCounters;
use crate::procs;
use crate::quality::{Quality, Sample, fit};

/// Every two seconds: reads the counters and WebRTC's stats, applies the
/// 1080p/720p rule and logs a line. Returns a summary when told to stop.
pub async fn watch(
    track: &LocalVideoTrack,
    shared: &Shared,
    counters: &VideoCounters,
    loopback: &Option<audio::Loopback>,
    seconds: Option<u64>,
    mut gone: tokio::sync::oneshot::Receiver<()>,
) -> String {
    const EVERY: Duration = Duration::from_secs(2);
    let started = Instant::now();
    let cpus = std::thread::available_parallelism().map(|n| n.get()).unwrap_or(1) as f64;
    let mut quality = Quality::default();
    let mut prev = (0u64, 0u64, 0u64, 0u64, 0u64, procs::cpu_time(), Instant::now(), 0u64);
    let mut lines: Vec<(f64, f64, f64, f64)> = Vec::new();
    let mut tick = tokio::time::interval(EVERY);
    tick.tick().await;
    loop {
        tokio::select! {
            _ = tick.tick() => {}
            _ = tokio::signal::ctrl_c() => break,
            _ = &mut gone => break,
        }
        let now = Instant::now();
        let dt = (now - prev.6).as_secs_f64();
        let captured = shared.captured.load(Ordering::Relaxed);
        let encoded = counters.encoded.load(Ordering::Relaxed);
        let bytes = counters.bytes.load(Ordering::Relaxed);
        let latency = counters.latency_us.load(Ordering::Relaxed);
        let convert = shared.convert_us.load(Ordering::Relaxed);
        let cpu = procs::cpu_time();
        let sound = loopback.as_ref().map(|l| l.frames.load(Ordering::Relaxed)).unwrap_or(0);
        let peak = loopback.as_ref().map(|l| l.peak.swap(0, Ordering::Relaxed)).unwrap_or(0);

        let source_fps = (captured - prev.0) as f64 / dt;
        let encoded_fps = (encoded - prev.1) as f64 / dt;
        let n = (encoded - prev.1).max(1) as f64;
        let encode_ms = (latency - prev.3) as f64 / n / 1000.0;
        let convert_ms = (convert - prev.4) as f64 / ((captured - prev.0).max(1) as f64) / 1000.0;
        let cpu_pct = (cpu - prev.5) as f64 / 1e7 / dt / cpus * 100.0;
        let mbps = (bytes - prev.2) as f64 * 8.0 / dt / 1e6;

        let (mut sent_fps, mut bandwidth, mut size, mut encoder_name) =
            (encoded_fps, None, String::new(), String::new());
        if let Ok(stats) = track.get_stats().await {
            for s in stats {
                match s {
                    RtcStats::OutboundRtp(o) if o.stream.kind == "video" => {
                        sent_fps = o.outbound.frames_per_second.max(0.0);
                        size = format!("{}x{}", o.outbound.frame_width, o.outbound.frame_height);
                        encoder_name = o.outbound.encoder_implementation.clone();
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
        quality = quality.next(Sample { source_fps, sent_fps, bandwidth });
        if quality.level != before {
            log::warn!("quality: {before:?} -> {:?}", quality.level);
            shared.set_level(quality.level);
        }
        let dropped = shared.dropped.load(Ordering::Relaxed);
        log::info!(
            "capture {source_fps:5.1} fps | encoded {encoded_fps:5.1} | sent {sent_fps:5.1} fps {size} ({encoder_name}) | \
             {mbps:4.1} Mbit/s, estimate {} | encode {encode_ms:4.1} ms, convert {convert_ms:4.1} ms | \
             cpu {cpu_pct:4.1}% | dropped {dropped} | sound {} frames, peak {:.0} dBFS | {:?}",
            bandwidth.map(|b| format!("{:.1} Mbit/s", b / 1e6)).unwrap_or("-".into()),
            sound - prev.7,
            20.0 * (peak.max(1) as f64 / 32768.0).log10(),
            quality.level,
        );
        lines.push((source_fps, sent_fps, encode_ms, cpu_pct));
        prev = (captured, encoded, bytes, latency, convert, cpu, now, sound);
        if seconds.is_some_and(|s| started.elapsed() >= Duration::from_secs(s)) {
            break;
        }
    }
    summarize(&lines, shared)
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
