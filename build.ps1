param(
    [string]$OutputDirectory = 'dist',
    [string]$MakeNSIS = 'makensis',
    [switch]$Release,
    [string]$SignTool = 'signtool.exe',
    [string]$SigningCertificateThumbprint = $env:LEXIGO_WINDOWS_SIGNING_THUMBPRINT,
    [string]$TimestampUrl = 'http://timestamp.digicert.com'
)
$ErrorActionPreference = 'Stop'

function Protect-ReleaseExecutable([string]$File) {
    & $SignTool sign /sha1 $thumbprint /fd SHA256 /tr $TimestampUrl /td SHA256 $File | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "Signing failed: $File" }
    Assert-WindowsSignature $File $SignTool $thumbprint
}

$oldCgo, $oldOS, $oldArch = $env:CGO_ENABLED, $env:GOOS, $env:GOARCH
Push-Location $PSScriptRoot
$staging = $null
try {
    if ($Release) {
        . .\scripts\windows-signatures.ps1
        $thumbprint = Get-CodeSigningThumbprint $SigningCertificateThumbprint
        if (-not (Get-Command $SignTool -ErrorAction SilentlyContinue)) {
            throw 'Install Windows SDK SignTool and pass its path with -SignTool.'
        }
        $certificate = Get-Item -LiteralPath "Cert:\CurrentUser\My\$thumbprint" -ErrorAction SilentlyContinue
        if (-not $certificate.HasPrivateKey) {
            throw 'The selected code-signing certificate with its private key must be available in CurrentUser\My.'
        }
    }
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'Install Go 1.25 or later and reopen PowerShell.'
    }
    if (-not (Get-Command $MakeNSIS -ErrorAction SilentlyContinue)) {
        throw 'Install NSIS 3 and pass its makensis.exe path with -MakeNSIS.'
    }
    $output = (New-Item -ItemType Directory -Force -Path $OutputDirectory).FullName
    $staging = Join-Path $output ('.build-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $staging | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & go test -buildvcs=false ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
    $target = Join-Path $staging 'LexiGo.exe'
    & go build -buildvcs=false -trimpath '-ldflags=-s -w -H=windowsgui' -o $target .
    if ($LASTEXITCODE -ne 0) { throw 'Build failed.' }
    if ($Release) { Protect-ReleaseExecutable $target }
    $setup = Join-Path $staging 'LexiGo-Setup-3.5.6.exe'
    & $MakeNSIS "/DPACKAGE_EXE=$target" "/DPACKAGE_SETUP=$setup" .\installer\LexiGo.nsi
    if ($LASTEXITCODE -ne 0) { throw 'Installer build failed.' }
    if ($Release) { Protect-ReleaseExecutable $setup }
    foreach ($file in @($target, $setup)) {
        $destination = Join-Path $output (Split-Path -Leaf $file)
        Move-Item -LiteralPath $file -Destination $destination -Force
        Get-FileHash -Algorithm SHA256 -LiteralPath $destination
    }
}
finally {
    if ($staging -and (Test-Path -LiteralPath $staging)) {
        Remove-Item -LiteralPath $staging -Recurse -Force
    }
    $env:CGO_ENABLED, $env:GOOS, $env:GOARCH = $oldCgo, $oldOS, $oldArch
    Pop-Location
}
