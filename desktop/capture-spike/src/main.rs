//! Shares a Windows monitor or window, with the sound of one application, to
//! a Zazzzor call: the first piece of the desktop client, on its own.

#[cfg(not(windows))]
compile_error!("the capture spike is Windows-only: it uses Windows Graphics Capture and Media Foundation");

mod audio;
mod capture;
mod join;
mod mf;
mod procs;
mod quality;

fn main() {}
