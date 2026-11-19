# Exercise release metadata against an isolated local repository and fake artifacts.
# Usage: powershell -NoProfile -File tools/desktop/prepare-release.tests.ps1
$ErrorActionPreference = 'Stop'
$prepare = Join-Path $PSScriptRoot 'prepare-release.ps1'
$checker = Join-Path $PSScriptRoot 'check-version.ps1'
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('zazzzor release ' + [guid]::NewGuid().ToString('N'))
$app = Join-Path $fixture 'desktop/app'
$bundles = Join-Path $fixture 'bundle'
$output = Join-Path $fixture 'metadata'
foreach ($dir in @($app, $bundles)) { [IO.Directory]::CreateDirectory($dir) | Out-Null }
# Keep all test commits and tags in this temporary repository.
function Invoke-Git {
    & git -C $fixture @args | Out-Null
    if ($LASTEXITCODE) { throw "fixture git failed: $args" }
}
function Write-Version([string]$Version) {
    [IO.File]::WriteAllText((Join-Path $app 'Cargo.toml'), "[package]`nname = `"zazzzor-desktop`"`nversion = `"$Version`"`n")
    [IO.File]::WriteAllText((Join-Path $app 'tauri.conf.json'), (@{version=$Version} | ConvertTo-Json))
    $script:installer = Join-Path $bundles "Zazzzor_${Version}_x64-setup.exe"
    [IO.File]::WriteAllText($script:installer, 'installer fixture')
    [IO.File]::WriteAllText(($script:installer + '.sig'), 'signature-fixture')
}
try {
    Invoke-Git init --quiet
    # Git for Windows turns line endings by default and warns on stderr, which
    # PowerShell with Stop takes for a failure: the fixture keeps them as they are.
    Invoke-Git config core.autocrlf false
    Invoke-Git config user.name 'Release Tests'
    Invoke-Git config user.email 'release-tests@example.invalid'
    Write-Version '0.2.0'
    Invoke-Git add desktop/app/Cargo.toml desktop/app/tauri.conf.json
    Invoke-Git commit --quiet -m 'feat(desktop): create shell'
    Invoke-Git tag desktop-v0.2.0
    $metadata = & $prepare -Tag desktop-v0.2.0 -Root $fixture -BundleDir $bundles -OutputDir $output
    $latest = [IO.File]::ReadAllText($metadata.Manifest) | ConvertFrom-Json
    if ($latest.version -ne '0.2.0' -or $latest.platforms.'windows-x86_64'.signature -ne 'signature-fixture') { throw 'wrong version or signature' }
    if ($latest.platforms.'windows-x86_64'.url -ne 'https://github.com/whoaskedssselery/Zazzzor/releases/download/desktop-v0.2.0/Zazzzor_0.2.0_x64-setup.exe') { throw 'wrong repository artifact URL' }
    if ($latest.notes -notmatch 'create shell') { throw 'initial release lost desktop history' }
    Write-Output 'PASS initial release metadata'

    [IO.File]::WriteAllText((Join-Path $fixture 'server.txt'), 'unrelated')
    Invoke-Git add server.txt
    Invoke-Git commit --quiet -m 'fix(server): unrelated change'
    Write-Version '0.2.1'
    Invoke-Git add desktop/app/Cargo.toml desktop/app/tauri.conf.json
    Invoke-Git commit --quiet -m 'fix(desktop): retain session'
    Invoke-Git tag desktop-v0.2.1
    $metadata = & $prepare -Tag desktop-v0.2.1 -Root $fixture -BundleDir $bundles -OutputDir $output
    $latest = [IO.File]::ReadAllText($metadata.Manifest) | ConvertFrom-Json
    if ($latest.notes -notmatch 'retain session' -or $latest.notes -match 'unrelated|create shell') { throw 'release notes crossed desktop tag/path boundaries' }
    Write-Output 'PASS desktop-only changes since previous desktop tag'
    Remove-Item -LiteralPath ($installer + '.sig')
    $failed = $false
    try { & $prepare -Tag desktop-v0.2.1 -Root $fixture -BundleDir $bundles -OutputDir $output | Out-Null } catch { $failed = $true }
    if (!$failed) { throw 'missing signature was accepted' }
    Write-Output 'PASS missing signature rejected'
} finally {
    $resolved = [IO.Path]::GetFullPath($fixture)
    if (!$resolved.StartsWith([IO.Path]::GetFullPath([IO.Path]::GetTempPath()), [StringComparison]::OrdinalIgnoreCase)) { throw 'unexpected fixture path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
