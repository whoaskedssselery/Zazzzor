# Refuse a desktop build unless the stable tag matches both desktop versions.
# Usage: powershell -NoProfile -File tools/desktop/check-version.ps1 -Tag desktop-v0.2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$Tag,
    [string]$Root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..')),
    [switch]$RequireTag
)
$ErrorActionPreference = 'Stop'
$matched = [regex]::Match($Tag, '\Adesktop-v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))\z')
if (!$matched.Success) { throw 'Expected a stable desktop-vX.Y.Z tag without prefixes or suffixes.' }
$version = $matched.Groups[1].Value
$app = Join-Path $Root 'desktop/app'
$manifest = [IO.File]::ReadAllText((Join-Path $app 'Cargo.toml'))
$package = [regex]::Match($manifest, '(?ms)^\[package\]\s*(.*?)(?=^\[|\z)')
$cargo = [regex]::Match($package.Groups[1].Value, '(?m)^version\s*=\s*"([^"]+)"\s*(?:#.*)?$')
$config = [IO.File]::ReadAllText((Join-Path $app 'tauri.conf.json')) | ConvertFrom-Json
if (!$cargo.Success -or $cargo.Groups[1].Value -cne $version -or $config.version -cne $version) {
    throw "Tag $Tag must match desktop/app/Cargo.toml ($($cargo.Groups[1].Value)) and tauri.conf.json ($($config.version)). Bump both versions."
}
if ($RequireTag) {
    $commit = & git -C $Root rev-parse --verify "refs/tags/$Tag^{commit}"
    if ($LASTEXITCODE) { throw "Tag $Tag does not exist locally." }
    $headCommit = & git -C $Root rev-parse HEAD
    if ($LASTEXITCODE -or $headCommit -cne $commit) { throw "Checkout must be at tag $Tag before building its release." }
}
Write-Output $version
