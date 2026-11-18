# Resolve the pinned official Windows x64 Tauri CLI after verifying its archive.
# Usage: $cli = ./tools/desktop/get-tauri.ps1; & $cli tauri --version
[CmdletBinding()]
param([string]$ToolDir = (Join-Path $env:USERPROFILE 'zz-build/tools/tauri-2.12.4'))
$ErrorActionPreference = 'Stop'
[IO.Directory]::CreateDirectory($ToolDir) | Out-Null
$archive = Join-Path $ToolDir 'cargo-tauri-2.12.4.zip'
$expected = '3e8687fb184bd7f10ebd8642f7715f79f4a605c0694c142a21507e8a24dc0f8b'
if (!(Test-Path -LiteralPath $archive -PathType Leaf)) {
    Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/tauri-apps/tauri/releases/download/tauri-cli-v2.12.4/cargo-tauri-x86_64-pc-windows-msvc.zip' -OutFile $archive
}
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -cne $expected) {
    throw "Tauri CLI checksum mismatch. Remove $archive and retry; no executable was extracted."
}
Expand-Archive -LiteralPath $archive -DestinationPath $ToolDir -Force
$cli = Join-Path $ToolDir 'cargo-tauri.exe'
if (!(Test-Path -LiteralPath $cli -PathType Leaf)) { throw 'Verified archive is missing cargo-tauri.exe.' }
Write-Output $cli
