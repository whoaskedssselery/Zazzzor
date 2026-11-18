# A cached archive must pass its pinned digest before it can provide an executable.
# Usage: powershell -NoProfile -File tools/desktop/get-tauri.tests.ps1
$ErrorActionPreference = 'Stop'
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('zazzzor cli ' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($fixture) | Out-Null
try {
    $fake = Join-Path $fixture 'cargo-tauri.exe'
    [IO.File]::WriteAllText($fake, 'untrusted executable fixture')
    Compress-Archive -LiteralPath $fake -DestinationPath (Join-Path $fixture 'cargo-tauri-2.12.4.zip')
    Remove-Item -LiteralPath $fake
    $message = ''
    try { & (Join-Path $PSScriptRoot 'get-tauri.ps1') -ToolDir $fixture | Out-Null } catch { $message = $_.Exception.Message }
    if ($message -notmatch 'checksum' -or (Test-Path -LiteralPath $fake)) { throw 'an untrusted archive was not rejected by its checksum before extraction' }
    Write-Output 'PASS cached CLI checksum checked before extraction'
} finally {
    $resolved = [IO.Path]::GetFullPath($fixture)
    if (!$resolved.StartsWith([IO.Path]::GetFullPath([IO.Path]::GetTempPath()), [StringComparison]::OrdinalIgnoreCase)) { throw 'unexpected fixture path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
