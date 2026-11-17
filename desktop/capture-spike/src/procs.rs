//! Finding a process by its executable name, and this process's CPU use.

use anyhow::{Result, bail};
use windows::Win32::Foundation::{CloseHandle, FILETIME};
use windows::Win32::System::Diagnostics::ToolHelp::{
    CreateToolhelp32Snapshot, PROCESSENTRY32W, Process32FirstW, Process32NextW, TH32CS_SNAPPROCESS,
};
use windows::Win32::System::Threading::{GetCurrentProcess, GetProcessTimes};

/// The root process named `exe` (e.g. "chrome.exe"): the one whose parent
/// has another name, so that the loopback of its tree takes every child.
pub fn find(exe: &str) -> Result<u32> {
    let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) }?;
    let mut all = Vec::new();
    let mut entry = PROCESSENTRY32W { dwSize: size_of::<PROCESSENTRY32W>() as u32, ..Default::default() };
    let mut more = unsafe { Process32FirstW(snapshot, &mut entry) }.is_ok();
    while more {
        let len = entry.szExeFile.iter().position(|c| *c == 0).unwrap_or(entry.szExeFile.len());
        let name = String::from_utf16_lossy(&entry.szExeFile[..len]);
        all.push((entry.th32ProcessID, entry.th32ParentProcessID, name));
        more = unsafe { Process32NextW(snapshot, &mut entry) }.is_ok();
    }
    unsafe {
        let _ = CloseHandle(snapshot);
    }
    let named = |pid: u32| all.iter().any(|(p, _, n)| *p == pid && n.eq_ignore_ascii_case(exe));
    let roots: Vec<u32> =
        all.iter().filter(|(p, parent, _)| named(*p) && !named(*parent)).map(|(p, _, _)| *p).collect();
    match roots.first() {
        Some(pid) => Ok(*pid),
        None => bail!("procs: no process named {exe}"),
    }
}

/// This process's CPU time (kernel and user), in 100 ns units.
pub fn cpu_time() -> u64 {
    let (mut created, mut exited, mut kernel, mut user) =
        (FILETIME::default(), FILETIME::default(), FILETIME::default(), FILETIME::default());
    if unsafe { GetProcessTimes(GetCurrentProcess(), &mut created, &mut exited, &mut kernel, &mut user) }.is_err() {
        return 0;
    }
    let ticks = |t: FILETIME| ((t.dwHighDateTime as u64) << 32) | t.dwLowDateTime as u64;
    ticks(kernel) + ticks(user)
}
