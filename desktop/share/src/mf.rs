//! H.264 on the GPU through a Media Foundation hardware encoder: NVENC on
//! NVIDIA, AMF on AMD, QuickSync on Intel, whichever the driver registers.
//! Hardware encoders are asynchronous transforms: they ask for input and
//! announce output through events, and run on their own threads.
//!
//! The encoder is set up for real-time streaming: constant bitrate, low
//! latency, no B-frames, keyframes only when asked (a viewer joining or
//! losing packets), and Annex B output with the SPS and PPS before each
//! keyframe, which is what the WebRTC H.264 packetizer expects.

use anyhow::{Context, Result, bail};
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::CoTaskMemFree;
use windows::Win32::System::Variant::VARIANT;
use windows::core::{Interface, PWSTR};

use crate::capture::Nv12Frame;

/// The baseline profile: every browser decodes it, no B-frames by definition.
const PROFILE_BASELINE: u32 = 66;
const RATE_CONTROL_CBR: u32 = 0;
const MF_VERSION: u32 = 0x0002_0070;

/// Starts Media Foundation for this process; once is enough.
pub fn startup() -> Result<()> {
    unsafe { MFStartup(MF_VERSION, MFSTARTUP_FULL) }.context("mf: start Media Foundation")
}

/// A hardware H.264 encoder that is registered on this machine.
pub struct Candidate {
    pub name: String,
    activate: IMFActivate,
}

/// The hardware H.264 encoders, best first as Media Foundation sorts them.
pub fn hardware_encoders() -> Result<Vec<Candidate>> {
    let input = MFT_REGISTER_TYPE_INFO { guidMajorType: MFMediaType_Video, guidSubtype: MFVideoFormat_NV12 };
    let output = MFT_REGISTER_TYPE_INFO { guidMajorType: MFMediaType_Video, guidSubtype: MFVideoFormat_H264 };
    let mut list: *mut Option<IMFActivate> = std::ptr::null_mut();
    let mut count = 0u32;
    unsafe {
        MFTEnumEx(
            MFT_CATEGORY_VIDEO_ENCODER,
            MFT_ENUM_FLAG_HARDWARE | MFT_ENUM_FLAG_SORTANDFILTER,
            Some(&input),
            Some(&output),
            &mut list,
            &mut count,
        )
    }
    .context("mf: list the H.264 encoders")?;
    let mut found = Vec::new();
    for i in 0..count as usize {
        if let Some(activate) = unsafe { (*list.add(i)).take() } {
            found.push(Candidate { name: friendly_name(&activate), activate });
        }
    }
    unsafe { CoTaskMemFree(Some(list.cast())) };
    Ok(found)
}

fn friendly_name(activate: &IMFActivate) -> String {
    let mut name = PWSTR::null();
    let mut len = 0u32;
    if unsafe { activate.GetAllocatedString(&MFT_FRIENDLY_NAME_Attribute, &mut name, &mut len) }.is_err() {
        return "unknown".into();
    }
    let s = unsafe { name.to_string() }.unwrap_or_default();
    unsafe { CoTaskMemFree(Some(name.0.cast())) };
    s
}

/// One encoded access unit.
pub struct Encoded {
    pub data: Vec<u8>,
    pub keyframe: bool,
    pub timestamp_us: i64,
    pub width: u32,
    pub height: u32,
    /// Time from handing the frame to the encoder to its access unit.
    pub encode_time: std::time::Duration,
}

pub struct Encoder {
    transform: IMFTransform,
    events: IMFMediaEventGenerator,
    codec: ICodecAPI,
    pub width: u32,
    pub height: u32,
    fps: u32,
    sequence_header: Vec<u8>,
    provides_samples: bool,
    /// Frames given to the encoder and not yet come out, oldest first.
    in_flight: std::collections::VecDeque<(i64, std::time::Instant)>,
}

fn pack(hi: u32, lo: u32) -> u64 {
    ((hi as u64) << 32) | lo as u64
}

fn variant_u32(v: u32) -> VARIANT {
    VARIANT::from(v)
}

impl Encoder {
    /// Opens `candidate` for `width`×`height` at up to `fps` and `bitrate` bit/s.
    pub fn open(candidate: &Candidate, width: u32, height: u32, fps: u32, bitrate: u32) -> Result<Encoder> {
        let transform: IMFTransform = unsafe { candidate.activate.ActivateObject() }.context("mf: open the encoder")?;
        let attributes = unsafe { transform.GetAttributes() }?;
        unsafe { attributes.SetUINT32(&MF_TRANSFORM_ASYNC_UNLOCK, 1) }.context("mf: unlock the async encoder")?;
        let _ = unsafe { attributes.SetUINT32(&MF_LOW_LATENCY, 1) };

        let codec: ICodecAPI = transform.cast().context("mf: the encoder has no ICodecAPI")?;
        unsafe {
            codec.SetValue(&CODECAPI_AVEncCommonRateControlMode, &variant_u32(RATE_CONTROL_CBR))?;
            codec.SetValue(&CODECAPI_AVEncCommonMeanBitRate, &variant_u32(bitrate))?;
            codec.SetValue(&CODECAPI_AVLowLatencyMode, &VARIANT::from(true))?;
            // Keyframes come when WebRTC asks; a long GOP keeps the bitrate even.
            let _ = codec.SetValue(&CODECAPI_AVEncMPVGOPSize, &variant_u32(fps * 60));
            let _ = codec.SetValue(&CODECAPI_AVEncMPVDefaultBPictureCount, &variant_u32(0));
        }

        let out = unsafe { MFCreateMediaType() }?;
        unsafe {
            out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            out.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_H264)?;
            out.SetUINT32(&MF_MT_AVG_BITRATE, bitrate)?;
            out.SetUINT64(&MF_MT_FRAME_SIZE, pack(width, height))?;
            out.SetUINT64(&MF_MT_FRAME_RATE, pack(fps, 1))?;
            out.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack(1, 1))?;
            out.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
            out.SetUINT32(&MF_MT_MPEG2_PROFILE, PROFILE_BASELINE)?;
            transform.SetOutputType(0, &out, 0).context("mf: set the H.264 output type")?;
        }
        let input = unsafe { MFCreateMediaType() }?;
        unsafe {
            input.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            input.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_NV12)?;
            input.SetUINT64(&MF_MT_FRAME_SIZE, pack(width, height))?;
            input.SetUINT64(&MF_MT_FRAME_RATE, pack(fps, 1))?;
            input.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack(1, 1))?;
            input.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
            transform.SetInputType(0, &input, 0).context("mf: set the NV12 input type")?;
        }

        let info = unsafe { transform.GetOutputStreamInfo(0) }?;
        let provides_samples = info.dwFlags
            & (MFT_OUTPUT_STREAM_PROVIDES_SAMPLES.0 as u32 | MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES.0 as u32)
            != 0;
        let events: IMFMediaEventGenerator = transform.cast()?;
        unsafe {
            transform.ProcessMessage(MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)?;
            transform.ProcessMessage(MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0)?;
        }
        let mut encoder = Encoder {
            transform,
            events,
            codec,
            width,
            height,
            fps,
            sequence_header: Vec::new(),
            provides_samples,
            in_flight: Default::default(),
        };
        encoder.read_sequence_header();
        Ok(encoder)
    }

    fn read_sequence_header(&mut self) {
        let Ok(current) = (unsafe { self.transform.GetOutputCurrentType(0) }) else { return };
        let Ok(size) = (unsafe { current.GetBlobSize(&MF_MT_MPEG_SEQUENCE_HEADER) }) else { return };
        let mut blob = vec![0u8; size as usize];
        if unsafe { current.GetBlob(&MF_MT_MPEG_SEQUENCE_HEADER, &mut blob, None) }.is_ok() {
            self.sequence_header = blob;
        }
    }

    /// Makes the next frame a keyframe.
    pub fn request_keyframe(&self) {
        let _ = unsafe { self.codec.SetValue(&CODECAPI_AVEncVideoForceKeyFrame, &variant_u32(1)) };
    }

    /// Changes the target bitrate while encoding.
    pub fn set_bitrate(&self, bitrate: u32) {
        let _ = unsafe { self.codec.SetValue(&CODECAPI_AVEncCommonMeanBitRate, &variant_u32(bitrate)) };
    }

    /// Waits for the encoder's next request. `NeedInput` means it takes a
    /// frame through `feed`; `HaveOutput` means `take` gives an access unit.
    pub fn next_event(&self) -> Result<Event> {
        let event = unsafe { self.events.GetEvent(MEDIA_EVENT_GENERATOR_GET_EVENT_FLAGS(0)) }?;
        let kind = unsafe { event.GetType() }?;
        Ok(match kind {
            k if k == METransformNeedInput.0 as u32 => Event::NeedInput,
            k if k == METransformHaveOutput.0 as u32 => Event::HaveOutput,
            _ => Event::Other,
        })
    }

    /// Gives the encoder one frame, which must have the encoder's size.
    pub fn feed(&mut self, frame: &Nv12Frame) -> Result<()> {
        let buffer = unsafe { MFCreateMemoryBuffer(frame.data.len() as u32) }?;
        unsafe {
            let mut ptr = std::ptr::null_mut();
            buffer.Lock(&mut ptr, None, None)?;
            std::ptr::copy_nonoverlapping(frame.data.as_ptr(), ptr, frame.data.len());
            buffer.Unlock()?;
            buffer.SetCurrentLength(frame.data.len() as u32)?;
        }
        let sample = unsafe { MFCreateSample() }?;
        unsafe {
            sample.AddBuffer(&buffer)?;
            sample.SetSampleTime(frame.timestamp_us * 10)?;
            sample.SetSampleDuration(10_000_000 / self.fps as i64)?;
            self.transform.ProcessInput(0, &sample, 0).context("mf: encode a frame")?;
        }
        self.in_flight.push_back((frame.timestamp_us, std::time::Instant::now()));
        Ok(())
    }

    /// Takes the access unit the encoder announced, or None when the
    /// encoder only changed its output format.
    pub fn take(&mut self) -> Result<Option<Encoded>> {
        let mut output = [MFT_OUTPUT_DATA_BUFFER::default()];
        if !self.provides_samples {
            bail!("mf: encoders that need their samples allocated are not supported");
        }
        let mut status = 0u32;
        let result = unsafe { self.transform.ProcessOutput(0, &mut output, &mut status) };
        let sample = unsafe { std::mem::ManuallyDrop::take(&mut output[0].pSample) };
        drop(unsafe { std::mem::ManuallyDrop::take(&mut output[0].pEvents) });
        if let Err(e) = result {
            if e.code() == MF_E_TRANSFORM_STREAM_CHANGE {
                let kind = unsafe { self.transform.GetOutputAvailableType(0, 0) }?;
                unsafe { self.transform.SetOutputType(0, &kind, 0) }?;
                self.read_sequence_header();
                return Ok(None);
            }
            return Err(e).context("mf: take an encoded frame");
        }
        let Some(sample) = sample else { return Ok(None) };
        let buffer = unsafe { sample.ConvertToContiguousBuffer() }?;
        let mut data = Vec::new();
        unsafe {
            let mut ptr = std::ptr::null_mut();
            let mut len = 0u32;
            buffer.Lock(&mut ptr, None, Some(&mut len))?;
            data.extend_from_slice(std::slice::from_raw_parts(ptr, len as usize));
            buffer.Unlock()?;
        }
        let keyframe = unsafe { sample.GetUINT32(&MFSampleExtension_CleanPoint) }.unwrap_or(0) != 0;
        let (fallback, fed) = self.in_flight.front().copied().unwrap_or((0, std::time::Instant::now()));
        let timestamp_us = unsafe { sample.GetSampleTime() }.map(|t| t / 10).unwrap_or(fallback);
        let mut encode_time = fed.elapsed();
        while let Some((t, at)) = self.in_flight.front().copied() {
            if t > timestamp_us {
                break;
            }
            encode_time = at.elapsed();
            self.in_flight.pop_front();
        }
        if keyframe && !has_nal(&data, 7) && !self.sequence_header.is_empty() {
            let mut with_header = self.sequence_header.clone();
            with_header.extend_from_slice(&data);
            data = with_header;
        }
        Ok(Some(Encoded { data, keyframe, timestamp_us, width: self.width, height: self.height, encode_time }))
    }

    /// Frames given and not yet encoded.
    pub fn pending(&self) -> usize {
        self.in_flight.len()
    }
}

impl Drop for Encoder {
    fn drop(&mut self) {
        unsafe {
            let _ = self.transform.ProcessMessage(MFT_MESSAGE_NOTIFY_END_OF_STREAM, 0);
            let _ = self.transform.ProcessMessage(MFT_MESSAGE_NOTIFY_END_STREAMING, 0);
            let _ = self.transform.ProcessMessage(MFT_MESSAGE_COMMAND_FLUSH, 0);
        }
        if let Ok(shutdown) = self.events.cast::<IMFShutdown>() {
            let _ = unsafe { shutdown.Shutdown() };
        }
    }
}

pub enum Event {
    NeedInput,
    HaveOutput,
    Other,
}

/// Whether an Annex B stream holds a NAL unit of type `kind` (7 is the SPS).
fn has_nal(data: &[u8], kind: u8) -> bool {
    data.windows(4).any(|w| w[0] == 0 && w[1] == 0 && w[2] == 1 && w[3] & 0x1f == kind)
}

#[cfg(test)]
mod tests {
    use super::has_nal;

    #[test]
    fn finds_an_sps_after_any_start_code() {
        assert!(has_nal(&[0, 0, 0, 1, 0x67, 1, 2], 7));
        assert!(has_nal(&[0, 0, 1, 0x68, 0, 0, 1, 0x67], 7));
        assert!(!has_nal(&[0, 0, 0, 1, 0x65, 0x67], 7));
    }
}
