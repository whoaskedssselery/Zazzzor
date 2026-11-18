# Check that wrong desktop tags or versions cannot reach the build.
# Usage: powershell -NoProfile -File tools/desktop/check-version.tests.ps1
$ErrorActionPreference = 'Stop'
$checker = Join-Path $PSScriptRoot 'check-version.ps1'
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('zazzzor desktop version ' + [guid]::NewGuid().ToString('N'))
$app = Join-Path $fixture 'desktop/app'
[IO.Directory]::CreateDirectory($app) | Out-Null
try {
    foreach ($case in @(
        @{Name='matching'; Cargo='0.2.0'; Config='0.2.0'; Tag='desktop-v0.2.0'; Valid=$true},
        @{Name='cargo mismatch'; Cargo='0.2.1'; Config='0.2.0'; Tag='desktop-v0.2.0'; Valid=$false},
        @{Name='config mismatch'; Cargo='0.2.0'; Config='0.2.1'; Tag='desktop-v0.2.0'; Valid=$false},
        @{Name='tag mismatch'; Cargo='0.2.0'; Config='0.2.0'; Tag='desktop-v0.2.2'; Valid=$false},
        @{Name='server tag'; Cargo='0.2.0'; Config='0.2.0'; Tag='v0.2.0'; Valid=$false},
        @{Name='leading zero'; Cargo='0.2.0'; Config='0.2.0'; Tag='desktop-v00.2.0'; Valid=$false},
        @{Name='prerelease'; Cargo='0.2.0'; Config='0.2.0'; Tag='desktop-v0.2.0-beta'; Valid=$false},
        @{Name='command text'; Cargo='0.2.0'; Config='0.2.0'; Tag='desktop-v0.2.0; exit 0'; Valid=$false}
    )) {
        [IO.File]::WriteAllText((Join-Path $app 'Cargo.toml'), "[package]`nname = `"zazzzor-desktop`"`nversion = `"$($case.Cargo)`"`n")
        [IO.File]::WriteAllText((Join-Path $app 'tauri.conf.json'), (@{version=$case.Config} | ConvertTo-Json))
        $failed = $false
        try { $version = & $checker -Tag $case.Tag -Root $fixture } catch { $failed = $true }
        if ($failed -eq $case.Valid) { throw "check-version $($case.Name): success=$(!$failed), want $($case.Valid)" }
        if (!$failed -and $version -ne $case.Cargo) { throw "validated version=$version, want $($case.Cargo)" }
        Write-Output "PASS $($case.Name)"
    }
} finally {
    $resolved = [IO.Path]::GetFullPath($fixture)
    $temporary = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if (!$resolved.StartsWith($temporary, [StringComparison]::OrdinalIgnoreCase)) { throw 'unexpected fixture path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
