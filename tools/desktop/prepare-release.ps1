# Write desktop-only release notes and updater metadata from the checked-out tag.
# Usage: ./tools/desktop/prepare-release.ps1 -Tag desktop-v0.2.0 -BundleDir C:/build/bundle/nsis -OutputDir C:/build/metadata
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$Tag,
    [Parameter(Mandatory=$true)][string]$BundleDir,
    [Parameter(Mandatory=$true)][string]$OutputDir,
    [string]$Root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
)
$ErrorActionPreference = 'Stop'
$version = & (Join-Path $PSScriptRoot 'check-version.ps1') -Tag $Tag -Root $Root
$installer = Join-Path $BundleDir "Zazzzor_${version}_x64-setup.exe"
$signature = $installer + '.sig'
if (!(Test-Path -LiteralPath $installer -PathType Leaf) -or !(Test-Path -LiteralPath $signature -PathType Leaf)) {
    throw "Expected the NSIS installer and its signature for $Tag in $BundleDir."
}
$signed = [IO.File]::ReadAllText($signature).Trim()
if (!$signed -or (Get-Item -LiteralPath $installer).Length -eq 0) { throw 'Empty installer or signature.' }
$commit = & git -C $Root rev-parse --verify "$Tag^{commit}"
if ($LASTEXITCODE) { throw "Tag $Tag does not exist locally." }
$headCommit = & git -C $Root rev-parse HEAD
if ($LASTEXITCODE -or $headCommit -cne $commit) { throw "Checkout must be at tag $Tag before preparing its release." }
$previous = @(& git -C $Root tag --merged $commit --sort=-version:refname --list 'desktop-v*' |
    Where-Object { $_ -cne $Tag -and $_ -cmatch '\Adesktop-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\z' })
if ($LASTEXITCODE) { throw 'Cannot enumerate previous desktop tags.' }
$range = $commit
if ($previous.Count -gt 0) { $range = "$($previous[0])..$commit" }
$notes = @(& git -C $Root log '--format=- %s (%h)' $range -- desktop/) -join "`n"
if ($LASTEXITCODE) { throw 'Cannot read desktop release history.' }
if (!$notes) { $notes = "Desktop release $version." }
$name = [IO.Path]::GetFileName($installer)
$latest = [ordered]@{
    version = $version
    notes = $notes
    pub_date = [datetime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    platforms = [ordered]@{
        'windows-x86_64' = [ordered]@{
            signature = $signed
            url = "https://github.com/whoaskedssselery/Zazzzor/releases/download/$Tag/$([uri]::EscapeDataString($name))"
        }
    }
}
[IO.Directory]::CreateDirectory($OutputDir) | Out-Null
$manifest = Join-Path $OutputDir 'latest.json'
$notesFile = Join-Path $OutputDir 'notes.md'
[IO.File]::WriteAllText($manifest, ($latest | ConvertTo-Json -Depth 5))
[IO.File]::WriteAllText($notesFile, $notes)
[pscustomobject]@{ Version=$version; Installer=$installer; Signature=$signature; Manifest=$manifest; Notes=$notesFile }
