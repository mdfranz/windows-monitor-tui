$certName = "Windows Monitor TUI Dev"
$cert = Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Subject -match $certName } | Select-Object -First 1

if (-not $cert) {
    Write-Host "Creating self-signed code-signing certificate: $certName..."
    $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=$certName" -KeySpec Signature -CertStoreLocation Cert:\CurrentUser\My
    Write-Host "Certificate created in CurrentUser\My."
}

if (Test-Path "monitor.exe") {
    Write-Host "Signing monitor.exe..."
    $status = Set-AuthenticodeSignature -FilePath "monitor.exe" -Certificate $cert
    $status | Select-Object Path, Status, StatusMessage | Format-List
} else {
    Write-Error "monitor.exe not found. Build the project first."
}
