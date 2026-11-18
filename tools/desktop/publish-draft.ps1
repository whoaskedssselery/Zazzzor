# Create or rebuild a draft; never publish, create a tag, or modify a published release.
# Usage: $metadata = ./tools/desktop/prepare-release.ps1 ...; ./tools/desktop/publish-draft.ps1 -Tag desktop-v0.2.0 -Metadata $metadata
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$Tag,
    [Parameter(Mandatory=$true)][psobject]$Metadata
)
$ErrorActionPreference = 'Stop'
if ($Tag -cne "desktop-v$($Metadata.Version)") { throw 'Release tag and prepared metadata disagree.' }
# A missing first release is expected. Windows PowerShell otherwise turns
# gh's stderr into a terminating error before LASTEXITCODE can be checked.
$originalPreference = $ErrorActionPreference
try {
    $ErrorActionPreference = 'Continue'
    $draft = & gh release view $Tag --json isDraft --jq '.isDraft' 2>$null
    $found = $LASTEXITCODE -eq 0
} finally { $ErrorActionPreference = $originalPreference }
# Defence in depth: the fresh check before upload also rejects published releases.
if ($found -and $draft -cne 'true') { throw "Release $Tag is already published; refusing to change it." }
if (!$found) {
    & gh release create $Tag --draft --verify-tag --title "Zazzzor desktop $($Metadata.Version)" --notes-file $Metadata.Notes
    if ($LASTEXITCODE) { throw "Cannot create draft $Tag." }
}
# The owner may have published a draft while this rebuild was running.
$draft = & gh release view $Tag --json isDraft --jq '.isDraft'
if ($LASTEXITCODE -or $draft -cne 'true') { throw "Release $Tag must still be a draft before uploading." }
& gh release upload $Tag $Metadata.Installer $Metadata.Signature $Metadata.Manifest --clobber
if ($LASTEXITCODE) { throw "Cannot upload assets to draft $Tag." }
