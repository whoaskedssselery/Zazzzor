//! A monitor or a window through Windows Graphics Capture, turned into NV12
//! frames of the size the current quality level allows.
//!
//! Each frame is read back from the GPU (BGRA), converted and scaled with
//! libyuv on this thread, and handed on; when the encoder is still busy with
//! the previous frames the new one is dropped, so a slow encoder costs frames,
//! never latency.

use std::sync::Arc;
use std::sync::atomic::{AtomicU8, AtomicU64, Ordering};
use std::sync::mpsc::{SyncSender, TrySendError};
use std::time::Duration;

use anyhow::{Context as _, Result, anyhow};
use livekit::webrtc::native::yuv_helper;
use livekit::webrtc::prelude::NV12Buffer;
use windows_capture::capture::{CaptureControl, Context, GraphicsCaptureApiHandler};
use windows_capture::frame::Frame;
use windows_capture::graphics_capture_api::InternalCaptureControl;
use windows_capture::monitor::Monitor;
use windows_capture::settings::{
    ColorFormat, CursorCaptureSettings, DirtyRegionSettings, DrawBorderSettings, MinimumUpdateIntervalSettings,
    SecondaryWindowSettings, Settings,
};
use windows_capture::window::Window;

use crate::quality::{Level, fit};

/// One captured frame: NV12 with a stride equal to the width, Y plane then UV.
pub struct Nv12Frame {
    pub width: u32,
    pub height: u32,
    pub data: Vec<u8>,
    /// Capture time on the system's monotonic clock, in microseconds.
    pub timestamp_us: i64,
}

/// What to capture.
#[derive(Clone, Debug)]
pub enum Target {
    Monitor(usize),
    Window(String),
}

/// What the capture shares with the rest of the program.
#[derive(Clone, Default)]
pub struct Shared {
    /// The quality level (0 = 1080p, 1 = 720p) frames are scaled to.
    pub level: Arc<AtomicU8>,
    /// Frames the capture delivered.
    pub captured: Arc<AtomicU64>,
    /// Frames dropped because the encoder was behind.
    pub dropped: Arc<AtomicU64>,
    /// Time spent converting and scaling, in microseconds, summed.
    pub convert_us: Arc<AtomicU64>,
}

impl Shared {
    pub fn level(&self) -> Level {
        if self.level.load(Ordering::Relaxed) == 0 { Level::P1080 } else { Level::P720 }
    }

    pub fn set_level(&self, level: Level) {
        self.level.store(if level == Level::P1080 { 0 } else { 1 }, Ordering::Relaxed);
    }
}

struct Flags {
    out: SyncSender<Nv12Frame>,
    shared: Shared,
    period_us: i64,
}

struct Handler {
    out: SyncSender<Nv12Frame>,
    shared: Shared,
    /// The time between frames at the wanted rate, in microseconds.
    period_us: i64,
    /// When the next frame is due, on the capture's clock.
    due_us: i64,
}

impl GraphicsCaptureApiHandler for Handler {
    type Flags = Flags;
    type Error = anyhow::Error;

    fn new(ctx: Context<Self::Flags>) -> Result<Self, Self::Error> {
        Ok(Handler { out: ctx.flags.out, shared: ctx.flags.shared, period_us: ctx.flags.period_us, due_us: 0 })
    }

    fn on_frame_arrived(&mut self, frame: &mut Frame, control: InternalCaptureControl) -> Result<(), Self::Error> {
        // SystemRelativeTime is in 100 ns units.
        let timestamp_us = frame.timestamp()?.Duration / 10;
        // Windows rounds the update interval up (16.7 ms became 20 ms, 50 fps), so the
        // capture is asked for twice the rate and paced here, before the GPU readback.
        // A little slack keeps a 60 Hz source from losing every other frame to jitter.
        if timestamp_us < self.due_us - self.period_us / 8 {
            return Ok(());
        }
        self.due_us = (self.due_us + self.period_us).max(timestamp_us);
        self.shared.captured.fetch_add(1, Ordering::Relaxed);
        let started = std::time::Instant::now();
        let (width, height) = (frame.width(), frame.height());
        let mut buffer = frame.buffer()?;
        let pitch = buffer.row_pitch();
        let bgra = buffer.as_raw_buffer();

        let mut full = NV12Buffer::new(width, height);
        let (sy, suv) = full.strides();
        let (y, uv) = full.data_mut();
        // libyuv's "ARGB" is BGRA in memory, which is what the capture gives.
        yuv_helper::argb_to_nv12(bgra, pitch, y, sy, uv, suv, width as i32, height as i32);

        let (w, h) = fit(width, height, self.shared.level());
        let sized = if (w, h) == (width, height) { full } else { full.scale(w as i32, h as i32) };
        let data = pack(&sized, w, h);
        self.shared.convert_us.fetch_add(started.elapsed().as_micros() as u64, Ordering::Relaxed);

        match self.out.try_send(Nv12Frame { width: w, height: h, data, timestamp_us }) {
            Ok(()) => {}
            Err(TrySendError::Full(_)) => {
                self.shared.dropped.fetch_add(1, Ordering::Relaxed);
            }
            Err(TrySendError::Disconnected(_)) => control.stop(),
        }
        Ok(())
    }

    fn on_closed(&mut self) -> Result<(), Self::Error> {
        log::info!("the captured item closed");
        Ok(())
    }
}

/// Copies an NV12 buffer into one block with no row padding.
fn pack(buf: &NV12Buffer, w: u32, h: u32) -> Vec<u8> {
    let (w, h) = (w as usize, h as usize);
    let (sy, suv) = buf.strides();
    let (y, uv) = buf.data();
    let mut out = Vec::with_capacity(w * h * 3 / 2);
    for row in 0..h {
        let at = row * sy as usize;
        out.extend_from_slice(&y[at..at + w]);
    }
    for row in 0..h / 2 {
        let at = row * suv as usize;
        out.extend_from_slice(&uv[at..at + w]);
    }
    out
}

/// A running capture; dropping it does not stop it, `stop` does.
pub struct Capture {
    control: CaptureControl<Handler, anyhow::Error>,
}

impl Capture {
    pub fn stop(self) {
        let _ = self.control.stop();
    }
}

/// Starts capturing `target` at up to `fps` frames per second, with the cursor
/// and without the yellow border.
pub fn start(target: &Target, fps: u32, out: SyncSender<Nv12Frame>, shared: Shared) -> Result<Capture> {
    let period_us = 1_000_000 / fps.max(1) as i64;
    let interval = MinimumUpdateIntervalSettings::Custom(Duration::from_micros(period_us as u64 / 2));
    let flags = Flags { out, shared, period_us };
    let control = match target {
        Target::Monitor(index) => {
            let monitor = Monitor::from_index(*index).with_context(|| format!("capture: no monitor {index}"))?;
            check(monitor)?;
            Handler::start_free_threaded(settings(monitor, interval, flags))
        }
        Target::Window(title) => {
            let window = Window::from_contains_name(title)
                .with_context(|| format!("capture: no window titled like {title:?}"))?;
            check(window)?;
            Handler::start_free_threaded(settings(window, interval, flags))
        }
    }
    .map_err(|e| anyhow!("capture: start: {e}"))?;
    Ok(Capture { control })
}

/// Makes the capture item once, on a thread of its own, for the reason
/// Windows gives when it cannot: windows-capture reports only that it failed.
fn check<T>(item: T) -> Result<()>
where
    T: TryInto<windows_capture::settings::GraphicsCaptureItemType, Error = windows::core::Error> + Send + 'static,
{
    std::thread::spawn(move || {
        unsafe {
            let _ =
                windows::Win32::System::Com::CoInitializeEx(None, windows::Win32::System::Com::COINIT_MULTITHREADED);
        }
        item.try_into().map(|_| ()).context("capture: Windows Graphics Capture refused the item")
    })
    .join()
    .map_err(|_| anyhow!("capture: the check panicked"))?
}

fn settings<T>(item: T, interval: MinimumUpdateIntervalSettings, flags: Flags) -> Settings<Flags, T>
where
    T: TryInto<windows_capture::settings::GraphicsCaptureItemType>,
{
    Settings::new(
        item,
        CursorCaptureSettings::WithCursor,
        DrawBorderSettings::WithoutBorder,
        SecondaryWindowSettings::Default,
        interval,
        DirtyRegionSettings::Default,
        ColorFormat::Bgra8,
        flags,
    )
}

/// The process that owns the window titled like `title`.
pub fn window_process(title: &str) -> Result<u32> {
    let window =
        Window::from_contains_name(title).with_context(|| format!("capture: no window titled like {title:?}"))?;
    Ok(window.process_id()?)
}

/// Prints the monitors and the windows that can be captured.
pub fn list() -> Result<()> {
    println!("Monitors (--monitor N):");
    for (i, m) in Monitor::enumerate()?.iter().enumerate() {
        let size = format!("{}x{}", m.width().unwrap_or(0), m.height().unwrap_or(0));
        let n = i + 1; // Monitor::from_index counts from one
        let can = match check(*m) {
            Ok(()) => "capturable".to_owned(),
            Err(e) => format!("not capturable: {:#}", e),
        };
        println!("  {n}: {} {size} @ {} Hz, {can}", m.name().unwrap_or_default(), m.refresh_rate().unwrap_or(0));
    }
    println!("Windows (--window \"part of the title\"):");
    for w in Window::enumerate()? {
        let title = w.title().unwrap_or_default();
        if title.trim().is_empty() {
            continue;
        }
        let process = w.process_name().unwrap_or_default();
        println!("  pid {:>6}  {process:<24} {title}", w.process_id().unwrap_or(0));
    }
    Ok(())
}
