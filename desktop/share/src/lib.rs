//! Sharing a Windows monitor or window, with the sound of one application,
//! to a Zazzzor call: Windows Graphics Capture, a hardware H.264 encoder
//! (libwebrtc's VP8 without one), the process loopback for the sound, and a
//! LiveKit connection of its own that publishes them. `examples/share`
//! drives it as a CLI, for measuring.

#[cfg(not(windows))]
compile_error!("the screen share is Windows-only: it uses Windows Graphics Capture and Media Foundation");

pub mod audio;
pub mod capture;
pub mod encode;
pub mod mf;
pub mod procs;
pub mod quality;
pub mod session;
pub mod sources;
