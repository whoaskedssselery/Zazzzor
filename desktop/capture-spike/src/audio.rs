//! The sound of one application through the process loopback of Windows 10
//! 2004 and later: what the process tree of a PID plays, and nothing else.
//! The browser has no way to do this, and it is the main reason for a native
//! client. The other mode takes everything except a process tree (the system
//! sound without this program, or without the voice chat).

use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc;
use std::thread::JoinHandle;

use anyhow::{Context, Result, bail};
use tokio::sync::mpsc::Sender;
use windows::Win32::Foundation::{CloseHandle, WAIT_OBJECT_0};
use windows::Win32::Media::Audio::{
    AUDCLNT_BUFFERFLAGS_SILENT, AUDCLNT_SHAREMODE_SHARED, AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM,
    AUDCLNT_STREAMFLAGS_EVENTCALLBACK, AUDCLNT_STREAMFLAGS_LOOPBACK, AUDIOCLIENT_ACTIVATION_PARAMS,
    AUDIOCLIENT_ACTIVATION_PARAMS_0, AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK, AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS,
    ActivateAudioInterfaceAsync, IActivateAudioInterfaceAsyncOperation, IActivateAudioInterfaceCompletionHandler,
    IActivateAudioInterfaceCompletionHandler_Impl, IAudioCaptureClient, IAudioClient,
    PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE, PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE,
    VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK, WAVEFORMATEX,
};
use windows::Win32::System::Com::{COINIT_MULTITHREADED, CoInitializeEx, CoUninitialize};
use windows::Win32::System::Threading::{CreateEventW, WaitForSingleObject};
use windows::core::{HRESULT, IUnknown, Interface, Ref, implement};

pub const SAMPLE_RATE: u32 = 48_000;
pub const CHANNELS: u32 = 2;
/// One chunk handed to WebRTC: 10 ms, what its audio pipeline works in.
pub const CHUNK_FRAMES: usize = (SAMPLE_RATE / 100) as usize;

/// Whose sound to take.
#[derive(Clone, Copy, Debug)]
pub enum Scope {
    /// What this process and its children play.
    Only(u32),
    /// Everything except what this process and its children play.
    AllBut(u32),
}

/// A running capture; `stop` ends it.
pub struct Loopback {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
    /// Frames (per channel) captured so far.
    pub frames: Arc<AtomicU64>,
    /// The loudest sample since it was last taken (0–32767).
    pub peak: Arc<AtomicU64>,
}

impl Loopback {
    pub fn stop(mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

/// Starts taking the sound of `scope` as 48 kHz stereo 16-bit chunks of
/// 10 ms, sent to `out`. Fails when the process loopback is not available.
pub fn start(scope: Scope, out: Sender<Vec<i16>>) -> Result<Loopback> {
    let stop = Arc::new(AtomicBool::new(false));
    let frames = Arc::new(AtomicU64::new(0));
    let peak = Arc::new(AtomicU64::new(0));
    let (ready_tx, ready_rx) = mpsc::channel::<Result<()>>();
    let thread = {
        let stop = stop.clone();
        let frames = frames.clone();
        let peak = peak.clone();
        std::thread::Builder::new().name("audio-loopback".into()).spawn(move || {
            unsafe {
                let _ = CoInitializeEx(None, COINIT_MULTITHREADED);
            }
            if let Err(e) = run(scope, &out, &stop, &frames, &peak, &ready_tx) {
                let _ = ready_tx.send(Err(e));
            }
            unsafe { CoUninitialize() };
        })?
    };
    ready_rx.recv().context("audio: the capture thread ended")??;
    Ok(Loopback { stop, thread: Some(thread), frames, peak })
}

#[implement(IActivateAudioInterfaceCompletionHandler)]
struct Activated(mpsc::Sender<()>);

impl IActivateAudioInterfaceCompletionHandler_Impl for Activated_Impl {
    fn ActivateCompleted(&self, _op: Ref<IActivateAudioInterfaceAsyncOperation>) -> windows::core::Result<()> {
        let _ = self.0.send(());
        Ok(())
    }
}

/// A PROPVARIANT holding a VT_BLOB, laid out as the real one on 64-bit Windows.
#[repr(C)]
struct BlobVariant {
    vt: u16,
    reserved: [u16; 3],
    size: u32,
    data: *const u8,
}
const VT_BLOB: u16 = 65;

fn activate(scope: Scope) -> Result<IAudioClient> {
    let (pid, mode) = match scope {
        Scope::Only(pid) => (pid, PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE),
        Scope::AllBut(pid) => (pid, PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE),
    };
    let params = AUDIOCLIENT_ACTIVATION_PARAMS {
        ActivationType: AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK,
        Anonymous: AUDIOCLIENT_ACTIVATION_PARAMS_0 {
            ProcessLoopbackParams: AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS {
                TargetProcessId: pid,
                ProcessLoopbackMode: mode,
            },
        },
    };
    let variant = BlobVariant {
        vt: VT_BLOB,
        reserved: [0; 3],
        size: size_of::<AUDIOCLIENT_ACTIVATION_PARAMS>() as u32,
        data: (&raw const params).cast(),
    };
    let (done_tx, done_rx) = mpsc::channel();
    let handler: IActivateAudioInterfaceCompletionHandler = Activated(done_tx).into();
    let op = unsafe {
        ActivateAudioInterfaceAsync(
            VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK,
            &IAudioClient::IID,
            Some((&raw const variant).cast()),
            &handler,
        )
    }
    .context("audio: activate the process loopback (Windows 10 2004 or later)")?;
    done_rx.recv().context("audio: the activation never completed")?;
    let mut hr = HRESULT(0);
    let mut unknown: Option<IUnknown> = None;
    unsafe { op.GetActivateResult(&mut hr, &mut unknown) }?;
    hr.ok().with_context(|| format!("audio: the process loopback of pid {pid} refused"))?;
    let Some(unknown) = unknown else { bail!("audio: the activation gave no client") };
    Ok(unknown.cast()?)
}

fn run(
    scope: Scope,
    out: &Sender<Vec<i16>>,
    stop: &AtomicBool,
    frames: &AtomicU64,
    peak: &AtomicU64,
    ready: &mpsc::Sender<Result<()>>,
) -> Result<()> {
    let client = activate(scope)?;
    // The process loopback has no mix format of its own: it converts to the one asked for.
    let block = (CHANNELS * 2) as u16;
    let format = WAVEFORMATEX {
        wFormatTag: 1, // WAVE_FORMAT_PCM
        nChannels: CHANNELS as u16,
        nSamplesPerSec: SAMPLE_RATE,
        nAvgBytesPerSec: SAMPLE_RATE * block as u32,
        nBlockAlign: block,
        wBitsPerSample: 16,
        cbSize: 0,
    };
    unsafe {
        client.Initialize(
            AUDCLNT_SHAREMODE_SHARED,
            AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM,
            200_000, // 20 ms, in 100 ns units
            0,
            &format,
            None,
        )
    }
    .context("audio: initialize the loopback client")?;
    let event = unsafe { CreateEventW(None, false, false, None) }?;
    unsafe { client.SetEventHandle(event) }?;
    let capture: IAudioCaptureClient = unsafe { client.GetService() }?;
    unsafe { client.Start() }?;
    let _ = ready.send(Ok(()));

    let per_chunk = CHUNK_FRAMES * CHANNELS as usize;
    let mut pending: Vec<i16> = Vec::with_capacity(per_chunk * 4);
    while !stop.load(Ordering::Relaxed) {
        // Nothing playing means no packets: wake up now and then to see the stop flag.
        if unsafe { WaitForSingleObject(event, 100) } != WAIT_OBJECT_0 {
            continue;
        }
        loop {
            let size = unsafe { capture.GetNextPacketSize() }?;
            if size == 0 {
                break;
            }
            let mut data = std::ptr::null_mut();
            let mut count = 0u32;
            let mut flags = 0u32;
            unsafe { capture.GetBuffer(&mut data, &mut count, &mut flags, None, None) }?;
            let samples = count as usize * CHANNELS as usize;
            if flags & AUDCLNT_BUFFERFLAGS_SILENT.0 as u32 != 0 || data.is_null() {
                pending.resize(pending.len() + samples, 0);
            } else {
                let got = unsafe { std::slice::from_raw_parts(data as *const i16, samples) };
                pending.extend_from_slice(got);
                let loudest = got.iter().map(|s| s.unsigned_abs() as u64).max().unwrap_or(0);
                peak.fetch_max(loudest, Ordering::Relaxed);
            }
            unsafe { capture.ReleaseBuffer(count) }?;
            frames.fetch_add(count as u64, Ordering::Relaxed);
        }
        while pending.len() >= per_chunk {
            let rest = pending.split_off(per_chunk);
            let chunk = std::mem::replace(&mut pending, rest);
            // A full queue means the sender is gone or far behind: drop rather than block capture.
            if out.try_send(chunk).is_err() && out.is_closed() {
                stop.store(true, Ordering::Relaxed);
            }
        }
    }
    unsafe {
        let _ = client.Stop();
        let _ = CloseHandle(event);
    }
    Ok(())
}
