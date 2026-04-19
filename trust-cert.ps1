$certName = "Windows Monitor TUI Dev"

# Check for Administrator privileges
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "This script MUST be run as Administrator to modify the Trusted Root store."
    Write-Host "Please right-click your terminal (PowerShell) and select 'Run as Administrator', then run this script again." -ForegroundColor Yellow
    return
}

# Find the certificate in your personal store
$cert = Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Subject -match $certName } | Select-Object -First 1

if (-not $cert) {
    Write-Error "Certificate '$certName' not found. Please run 'task build' first to create it."
    return
}

# Export and Import to Trusted Root
Write-Host "Trusting certificate: $certName..."
$tempFile = "$env:TEMP\dev_cert.cer"
try {
    Export-Certificate -Cert $cert -FilePath $tempFile | Out-Null
    Import-Certificate -FilePath $tempFile -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
    Write-Host "SUCCESS: The certificate is now trusted system-wide." -ForegroundColor Green
    Write-Host "Smart App Control should now allow monitor.exe to run."
} catch {
    Write-Error "An error occurred: $_"
} finally {
    if (Test-Path $tempFile) { Remove-Item $tempFile }
}
