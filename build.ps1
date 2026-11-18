param([string]$OutputDirectory = 'dist')
$ErrorActionPreference = 'Stop'
$oldCgo, $oldOS, $oldArch = $env:CGO_ENABLED, $env:GOOS, $env:GOARCH
Push-Location $PSScriptRoot
try {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'Install Go 1.25 or later and reopen PowerShell.'
    }
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & go test -buildvcs=false ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
    $target = Join-Path $OutputDirectory 'LexiGo.exe'
    & go build -buildvcs=false -trimpath '-ldflags=-s -w -H=windowsgui' -o $target .
    if ($LASTEXITCODE -ne 0) { throw 'Build failed.' }
    Get-FileHash -Algorithm SHA256 $target
}
finally {
    $env:CGO_ENABLED, $env:GOOS, $env:GOARCH = $oldCgo, $oldOS, $oldArch
    Pop-Location
}
