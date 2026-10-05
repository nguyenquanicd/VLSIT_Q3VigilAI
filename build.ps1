<#
.SYNOPSIS
    Build and test Q3VNLaw.

.EXAMPLE
    .\build.ps1            # run the tests, then build q3vnlaw.exe
    .\build.ps1 -SkipTests # build only
    .\build.ps1 -Live      # also run the tests that reach the real sources
#>
[CmdletBinding()]
param(
    [switch]$SkipTests,
    [switch]$Live
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

# Go may be installed without being on PATH (a zip unpacked under the user's
# local programs folder needs no administrator rights).
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) {
    foreach ($p in "$env:LOCALAPPDATA\Programs\go\bin\go.exe", "$env:ProgramFiles\Go\bin\go.exe") {
        if (Test-Path $p) { $go = $p; break }
    }
}
if (-not $go) { throw 'Go was not found. Install it from https://go.dev/dl/ (version 1.27 or newer).' }
Write-Host "Using $(& $go version)"

& $go vet ./...
if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }

if (-not $SkipTests) {
    if ($Live) { $env:Q3_LIVE = '1' } else { Remove-Item Env:Q3_LIVE -ErrorAction SilentlyContinue }
    & $go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'tests failed' }
}

# -H windowsgui: no console window. -s -w and -trimpath: smaller file, no
# local paths inside it.
& $go build -trimpath -ldflags '-s -w -H windowsgui' -o q3vnlaw.exe ./cmd/q3vnlaw
if ($LASTEXITCODE -ne 0) { throw 'build failed' }

$exe = Get-Item .\q3vnlaw.exe
Write-Host ("Built {0} ({1:N1} MB)" -f $exe.FullName, ($exe.Length / 1MB))
