param(
    [Parameter(Mandatory)][ValidatePattern('\Av[0-9]+\.[0-9]+\.[0-9]+\z')][string]$Tag,
    [Parameter(Mandatory)][string]$AndroidBuildTools,
    [string]$Artifacts = 'dist',
    [string]$SignTool = 'signtool.exe',
    [string]$SigningCertificateThumbprint = $env:LEXIGO_WINDOWS_SIGNING_THUMBPRINT,
    [string]$Java = 'java.exe'
)
$ErrorActionPreference = 'Stop'
$repo = 'QiNian2684/LexiGo'
$project = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'windows-signatures.ps1')

$version = $Tag.Substring(1)
$thumbprint = Get-CodeSigningThumbprint $SigningCertificateThumbprint
$root = (Resolve-Path -LiteralPath $Artifacts).Path
$names = @('LexiGo.exe', "LexiGo-Setup-$version.exe", "LexiGo-Android-$version.apk", "LexiGo-Android-$version.apk.idsig")
$files = @($names | ForEach-Object { Join-Path $root $_ })
foreach ($file in $files) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Missing release asset: $file" }
}

foreach ($file in $files[0..1]) {
    Assert-WindowsSignature $file $SignTool $thumbprint
    $fileVersion = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($file).ProductVersion
    if ($fileVersion -notin @($version, "$version.0")) { throw "Incorrect product version in ${file}: $fileVersion" }
}

$apk = $files[2]
& (Join-Path $AndroidBuildTools 'zipalign.exe') -c -P 16 4 $apk
if ($LASTEXITCODE -ne 0) { throw 'APK alignment verification failed.' }
& $Java -cp (Join-Path $AndroidBuildTools 'lib\apksigner.jar') (Join-Path $project 'android\VerifyApkSignatures.java') $apk --release
if ($LASTEXITCODE -ne 0) { throw 'APK signature verification failed.' }

$manifest = [xml](Get-Content -LiteralPath (Join-Path $project 'android\src\main\AndroidManifest.xml') -Raw)
$androidNamespace = 'http://schemas.android.com/apk/res/android'
$versionCode = $manifest.DocumentElement.GetAttribute('versionCode', $androidNamespace)
$badging = & (Join-Path $AndroidBuildTools 'aapt.exe') dump badging $apk
$packageLine = "package: name='cn.wordreview.app.lexigo.v213' versionCode='$versionCode' versionName='$version'"
if ($LASTEXITCODE -ne 0 -or -not ($badging -like "$packageLine *")) {
    throw 'APK package name or version does not match the LexiGo release.'
}

$hashes = @{}
foreach ($file in $files) {
    $hashes[(Split-Path -Leaf $file)] = (Get-FileHash -Algorithm SHA256 -LiteralPath $file).Hash
}

$sourceCommit = git -C $project rev-parse HEAD
if ($LASTEXITCODE -ne 0) { throw 'Unable to read the source checkout commit.' }
$tagCommit = gh api "repos/$repo/commits/$Tag" --jq '.sha'
if ($LASTEXITCODE -ne 0 -or $tagCommit -ne $sourceCommit) {
    throw "$Tag must point to the source checkout commit on GitHub."
}

# Older PowerShell treats redirected stderr as a terminating error with Stop.
$previousErrorAction = $ErrorActionPreference
try {
    $ErrorActionPreference = 'Continue'
    $existing = gh release view $Tag --repo $repo --json isDraft,assets 2>$null
    $found = $LASTEXITCODE -eq 0
} finally {
    $ErrorActionPreference = $previousErrorAction
}
if ($found) {
    $draft = $existing | ConvertFrom-Json
    if (-not $draft.isDraft) { throw "$Tag is already published." }
    $attached = @($draft.assets | ForEach-Object { $_.name })
    if ($attached | Where-Object { $_ -notin $names }) {
        throw 'Release draft contains unexpected assets. Review them before publishing.'
    }
} else {
    gh release create $Tag --repo $repo --verify-tag --draft --title "LexiGo $version" --generate-notes
    if ($LASTEXITCODE -ne 0) { throw 'Unable to create a draft release for the existing tag.' }
    $attached = @()
}

$missing = @($files | Where-Object { (Split-Path -Leaf $_) -notin $attached })
if ($missing.Count -gt 0) {
    gh release upload $Tag @missing --repo $repo
    if ($LASTEXITCODE -ne 0) { throw 'Asset upload failed. The release is still a draft.' }
}

$download = Join-Path ([System.IO.Path]::GetTempPath()) ('lexigo-release-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $download | Out-Null
try {
    gh release download $Tag --repo $repo --pattern 'LexiGo*' --dir $download
    if ($LASTEXITCODE -ne 0) { throw 'Unable to download the draft release assets for verification.' }
    $downloaded = @(Get-ChildItem -LiteralPath $download -File)
    if ($downloaded.Count -ne $files.Count) { throw 'The draft release asset count differs from the verified build.' }
    foreach ($file in $downloaded) {
        if ((Get-FileHash -Algorithm SHA256 -LiteralPath $file.FullName).Hash -ne $hashes[$file.Name]) {
            throw "Downloaded asset differs from the verified build: $($file.Name)"
        }
    }
    gh release edit $Tag --repo $repo --draft=false
    if ($LASTEXITCODE -ne 0) { throw 'Unable to publish the verified GitHub release.' }
} finally {
    Remove-Item -LiteralPath $download -Recurse -Force
}
