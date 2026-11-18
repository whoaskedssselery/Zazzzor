# Check draft-only publication without invoking GitHub or creating remote tags.
# Usage: powershell -NoProfile -File tools/desktop/publish-draft.tests.ps1
$ErrorActionPreference = 'Stop'
$publisher = Join-Path $PSScriptRoot 'publish-draft.ps1'
$metadata = [pscustomobject]@{Version='0.2.0';Installer='setup.exe';Signature='setup.exe.sig';Manifest='latest.json';Notes='notes.md'}
function global:gh {
    $global:C20Calls.Add(($args -join ' '))
    $global:LASTEXITCODE = 0
    if ($args[1] -eq 'view') {
        $state = $global:C20States.Dequeue()
        if ($state -eq 'missing') {
            $global:LASTEXITCODE = 1
            Write-Error 'release not found'
            return
        }
        return $state
    }
}
try {
    foreach ($case in @(
        @{Name='create draft'; States=@('missing','true'); Valid=$true; Creates=$true},
        @{Name='rebuild draft'; States=@('true','true'); Valid=$true; Creates=$false},
        @{Name='published'; States=@('false'); Valid=$false; Creates=$false},
        @{Name='published during rebuild'; States=@('true','false'); Valid=$false; Creates=$false}
    )) {
        $global:C20Calls = [Collections.Generic.List[string]]::new()
        $global:C20States = [Collections.Generic.Queue[string]]::new()
        foreach ($state in $case.States) { $global:C20States.Enqueue($state) }
        $failed = $false
        try { & $publisher -Tag desktop-v0.2.0 -Metadata $metadata } catch { $failed = $true }
        if ($failed -eq $case.Valid) { throw "publisher $($case.Name): success=$(!$failed), want $($case.Valid)" }
        $creates = @($global:C20Calls | Where-Object { $_ -match '^release create ' })
        $uploads = @($global:C20Calls | Where-Object { $_ -match '^release upload ' })
        if (($creates.Count -gt 0) -ne $case.Creates -or ($uploads.Count -gt 0) -ne $case.Valid) { throw "wrong publisher actions for $($case.Name)" }
        if ($creates.Count -gt 0 -and ($creates[0] -notmatch '--draft' -or $creates[0] -notmatch '--verify-tag')) { throw 'release creation could publish or create a tag' }
        if ($uploads.Count -gt 0 -and $uploads[0] -notmatch 'setup.exe setup.exe.sig latest.json --clobber') { throw 'wrong uploaded release assets' }
        Write-Output "PASS $($case.Name)"
    }
} finally {
    Remove-Item Function:gh
    Remove-Variable C20Calls,C20States -Scope Global -ErrorAction SilentlyContinue
}
