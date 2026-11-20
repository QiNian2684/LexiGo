param([string]$OutputDirectory = 'dist', [string]$MakeNSIS = 'makensis')
$ErrorActionPreference = 'Stop'
$oldCgo, $oldOS, $oldArch = $env:CGO_ENABLED, $env:GOOS, $env:GOARCH
Push-Location $PSScriptRoot
try {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'Install Go 1.25 or later and reopen PowerShell.'
    }
    if (-not (Get-Command $MakeNSIS -ErrorAction SilentlyContinue)) {
        throw 'Install NSIS 3 and pass its makensis.exe path with -MakeNSIS.'
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
    $setup = Join-Path (Resolve-Path $OutputDirectory).Path 'LexiGo-Setup-3.0.1.exe'
    & $MakeNSIS "/DPACKAGE_EXE=$((Resolve-Path $target).Path)" "/DPACKAGE_SETUP=$setup" .\installer\LexiGo.nsi
    if ($LASTEXITCODE -ne 0) { throw 'Installer build failed.' }
    Get-FileHash -Algorithm SHA256 $target
    Get-FileHash -Algorithm SHA256 $setup
}
finally {
    $env:CGO_ENABLED, $env:GOOS, $env:GOARCH = $oldCgo, $oldOS, $oldArch
    Pop-Location
}
