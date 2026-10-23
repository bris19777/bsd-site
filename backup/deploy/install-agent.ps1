# Installs the BSD Backup Agent on a Windows PC. Run from an elevated
# PowerShell prompt in the folder that contains bsd-backup-agent.exe:
#
#   .\install-agent.ps1 -Server https://backup.example.com -Username acme
#
# The account password is prompted for (never stored); the device receives
# its own token. A recovery key is generated unless -Passphrase is given.
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [Parameter(Mandatory = $true)][string]$Username,
    [string]$Passphrase = ""
)
$ErrorActionPreference = "Stop"

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this script as Administrator."
}

$dest = Join-Path $env:ProgramFiles "BSDBackup"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item -Force (Join-Path $PSScriptRoot "bsd-backup-agent.exe") $dest
$exe = Join-Path $dest "bsd-backup-agent.exe"

$secure = Read-Host "Password for $Username" -AsSecureString
$env:BSD_BACKUP_PASSWORD = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
    [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
if ($Passphrase) { $env:BSD_BACKUP_PASSPHRASE = $Passphrase }
try {
    & $exe enroll -server $Server -username $Username
    if ($LASTEXITCODE -ne 0) { throw "enrollment failed" }
} finally {
    Remove-Item Env:\BSD_BACKUP_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:\BSD_BACKUP_PASSPHRASE -ErrorAction SilentlyContinue
}

& $exe install
if ($LASTEXITCODE -ne 0) { throw "service install failed" }
& $exe start
if ($LASTEXITCODE -ne 0) { throw "service start failed" }
Write-Host "BSD Backup Agent installed and running. Check with: & '$exe' status"
