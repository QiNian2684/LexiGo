function Get-CodeSigningThumbprint([string]$Thumbprint) {
    $Thumbprint = ($Thumbprint -replace '\s', '').ToUpperInvariant()
    if ($Thumbprint -notmatch '\A[0-9A-F]{40}\z') {
        throw 'Set LEXIGO_WINDOWS_SIGNING_THUMBPRINT to the Windows code-signing certificate thumbprint.'
    }
    return $Thumbprint
}

function Assert-WindowsSignature([string]$File, [string]$SignTool, [string]$Thumbprint) {
    & $SignTool verify /pa /all /tw /v $File | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "Authenticode or timestamp verification failed: $File" }
    $signature = Get-AuthenticodeSignature -LiteralPath $File
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Thumbprint -ne $Thumbprint) {
        throw "Unexpected Windows signing certificate or invalid Authenticode signature: $File"
    }
}
