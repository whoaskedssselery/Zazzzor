//! Turning captured frames into the video WebRTC sends: a hardware H.264
//! encoder through Media Foundation, or libwebrtc's software VP8.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::mpsc::{Receiver, RecvTimeoutError};
use std::time::Duration;

use anyhow::{Result, bail};
use livekit::webrtc::prelude::{NV12Buffer, VideoFrame, VideoRotation};
use livekit::webrtc::video_frame::{EncodedFrameType, EncodedVideoCodec, EncodedVideoFrame};
use livekit::webrtc::video_source::VideoResolution;
use livekit::webrtc::video_source::native::NativeVideoSource;

use crate::capture::Nv12Frame;
use crate::mf;

/// What the video thread counts, read by the stats loop.
#[derive(Default)]
pub struct VideoCounters {
    pub encoded: AtomicU64,
    pub bytes: AtomicU64,
    /// Time from handing a frame to the encoder to its access unit, summed, in microseconds.
    pub latency_us: AtomicU64,
    pub keyframes: AtomicU64,
}

/// Encodes on the GPU and hands the access units to WebRTC as they are.
pub fn encode_hardware(
    name: &str,
    frames: Receiver<Nv12Frame>,
    source: &NativeVideoSource,
    counters: &VideoCounters,
    fps: u32,
    max_bitrate: u32,
) -> Result<()> {
    unsafe {
        let _ = windows::Win32::System::Com::CoInitializeEx(None, windows::Win32::System::Com::COINIT_MULTITHREADED);
    }
    let Some(candidate) = mf::hardware_encoders()?.into_iter().find(|c| c.name == name) else {
        bail!("the encoder {name} is gone");
    };
    let hw = Hardware { candidate: &candidate, frames: &frames, source, counters, fps, max_bitrate };
    log::info!("encoding with {}", candidate.name);
    let mut bitrate = max_bitrate;
    let mut encoder: Option<mf::Encoder> = None;
    // A still screen gives no frames: the last one is sent again now and then,
    // so a viewer who joins or loses a packet still gets a keyframe.
    let mut last: Option<Nv12Frame> = None;
    let mut waiting: Option<Nv12Frame> = None;
    // Errors in a row. One is met with a fresh encoder: a window that changes
    // size several times at once (a video going full screen) can fail a
    // reopen. Many in a row mean the encoder cannot go on.
    let mut failures = 0u32;
    loop {
        let frame = match waiting.take() {
            Some(f) => f,
            None => match frames.recv_timeout(Duration::from_millis(500)) {
                Ok(f) => f,
                Err(RecvTimeoutError::Timeout) => match &last {
                    // On the capture's clock: half a second after the frame it repeats.
                    Some(l) => Nv12Frame { data: l.data.clone(), timestamp_us: l.timestamp_us + 500_000, ..*l },
                    None => continue,
                },
                Err(RecvTimeoutError::Disconnected) => return Ok(()),
            },
        };
        match hw.feed(&frame, &mut encoder, &mut waiting, &mut bitrate) {
            Ok(Fed::Disconnected) => return Ok(()),
            Ok(Fed::Done(newer)) => {
                failures = 0;
                last = newer.or(Some(frame));
            }
            Err(e) => {
                failures += 1;
                if failures >= MAX_FAILURES {
                    return Err(e.context("the encoder keeps failing"));
                }
                log::warn!("encoder: {e:#}; starting it again");
                encoder = None;
                std::thread::sleep(Duration::from_millis(100));
            }
        }
    }
}

/// Encoder errors in a row after which the share's video gives up.
const MAX_FAILURES: u32 = 20;

/// What became of one frame: fed (with a newer one fed after it, if any),
/// or the capture is gone.
enum Fed {
    Done(Option<Nv12Frame>),
    Disconnected,
}

/// What the hardware encoding loop works with.
struct Hardware<'a> {
    candidate: &'a mf::Candidate,
    frames: &'a Receiver<Nv12Frame>,
    source: &'a NativeVideoSource,
    counters: &'a VideoCounters,
    fps: u32,
    max_bitrate: u32,
}

impl Hardware<'_> {
    /// Feeds `frame` to the encoder (opened anew for a new size) and sends
    /// whatever comes out meanwhile.
    fn feed(
        &self,
        frame: &Nv12Frame,
        encoder: &mut Option<mf::Encoder>,
        waiting: &mut Option<Nv12Frame>,
        bitrate: &mut u32,
    ) -> Result<Fed> {
        let size_changed = encoder.as_ref().is_none_or(|e| (e.width, e.height) != (frame.width, frame.height));
        if size_changed {
            drop(encoder.take());
            log::info!("encoder at {}x{}", frame.width, frame.height);
            *encoder = Some(mf::Encoder::open(self.candidate, frame.width, frame.height, self.fps, *bitrate)?);
        }
        let Some(enc) = encoder.as_mut() else { return Ok(Fed::Done(None)) };
        // Feed this frame at the encoder's next request, and send whatever comes out meanwhile.
        loop {
            if self.source.take_keyframe_request() {
                enc.request_keyframe();
            }
            if let Some(rc) = self.source.take_rate_control_request() {
                let wanted = (rc.target_bitrate_bps as u32).clamp(500_000, self.max_bitrate);
                if wanted != *bitrate {
                    *bitrate = wanted;
                    enc.set_bitrate(wanted);
                }
            }
            match enc.next_event()? {
                mf::Event::NeedInput => {
                    enc.feed(frame)?;
                    break;
                }
                mf::Event::HaveOutput => send(enc, self.source, self.counters)?,
                mf::Event::Other => {}
            }
        }
        // Take what is ready without waiting for the next frame.
        let mut newer = None;
        while enc.pending() > 0 {
            match enc.next_event()? {
                mf::Event::HaveOutput => send(enc, self.source, self.counters)?,
                mf::Event::NeedInput => {
                    // It wants the next frame before finishing this one: give it when it comes.
                    match self.frames.recv_timeout(Duration::from_millis(500)) {
                        Ok(f) if (f.width, f.height) == (enc.width, enc.height) => {
                            enc.feed(&f)?;
                            newer = Some(f);
                        }
                        Ok(f) => {
                            *waiting = Some(f);
                            break;
                        }
                        Err(RecvTimeoutError::Timeout) => break,
                        Err(RecvTimeoutError::Disconnected) => return Ok(Fed::Disconnected),
                    }
                }
                mf::Event::Other => {}
            }
        }
        Ok(Fed::Done(newer))
    }
}

fn send(enc: &mut mf::Encoder, source: &NativeVideoSource, counters: &VideoCounters) -> Result<()> {
    let Some(unit) = enc.take()? else { return Ok(()) };
    counters.encoded.fetch_add(1, Ordering::Relaxed);
    counters.bytes.fetch_add(unit.data.len() as u64, Ordering::Relaxed);
    counters.latency_us.fetch_add(unit.encode_time.as_micros() as u64, Ordering::Relaxed);
    if unit.keyframe {
        counters.keyframes.fetch_add(1, Ordering::Relaxed);
    }
    source.capture_encoded_frame(&EncodedVideoFrame {
        codec: EncodedVideoCodec::H264,
        payload: &unit.data,
        timestamp_us: unit.timestamp_us,
        frame_type: if unit.keyframe { EncodedFrameType::Key } else { EncodedFrameType::Delta },
        resolution: VideoResolution { width: unit.width, height: unit.height },
        frame_metadata: None,
    });
    Ok(())
}

/// Hands raw frames to WebRTC, whose software VP8 encoder sends them.
pub fn encode_software(
    frames: Receiver<Nv12Frame>,
    source: &NativeVideoSource,
    counters: &VideoCounters,
) -> Result<()> {
    log::info!("encoding with libwebrtc's software VP8");
    while let Ok(frame) = frames.recv() {
        let mut buffer = NV12Buffer::new(frame.width, frame.height);
        let (sy, suv) = buffer.strides();
        let (y, uv) = buffer.data_mut();
        let (w, h) = (frame.width as usize, frame.height as usize);
        for row in 0..h {
            y[row * sy as usize..][..w].copy_from_slice(&frame.data[row * w..][..w]);
        }
        for row in 0..h / 2 {
            uv[row * suv as usize..][..w].copy_from_slice(&frame.data[w * h + row * w..][..w]);
        }
        source.capture_frame(&VideoFrame {
            rotation: VideoRotation::VideoRotation0,
            timestamp_us: frame.timestamp_us,
            frame_metadata: None,
            buffer,
        });
        counters.encoded.fetch_add(1, Ordering::Relaxed);
    }
    Ok(())
}
