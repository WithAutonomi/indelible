## Restart indelible only, preserving the devnet, antd, and database.
##
## Rebuilds and restarts indelible against the same DB, devnet, and antd.
## Uploads, users, wallets, and all data persist across restarts.
##
## Usage:
##   .\scripts\dev-e2e-restart.ps1
##
## First run:
##   .\scripts\dev-e2e.ps1    (sets up everything from scratch)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$indelibleRoot = (Resolve-Path "$scriptDir\..").Path
$pidFile = "$env:TEMP\indelible-e2e-pids.json"
$indelibleLog = "$env:TEMP\indelible-e2e-indelible.log"

if (-not (Test-Path $pidFile)) {
    Write-Host "ERROR: No E2E environment found. Run .\scripts\dev-e2e.ps1 first." -ForegroundColor Red
    exit 1
}

$state = Get-Content $pidFile -Raw | ConvertFrom-Json

# Check devnet + antd are still running
$devnetAlive = $false
$antdAlive = $false
try { $devnetAlive = -not (Get-Process -Id $state.devnet_pid -ErrorAction Stop).HasExited } catch {}
try { $antdAlive = -not (Get-Process -Id $state.antd_pid -ErrorAction Stop).HasExited } catch {}

if (-not $devnetAlive -or -not $antdAlive) {
    Write-Host "ERROR: devnet or antd is not running. Run .\scripts\dev-e2e.ps1 for a full restart." -ForegroundColor Red
    if (-not $devnetAlive) { Write-Host "  devnet (PID $($state.devnet_pid)): not running" -ForegroundColor Gray }
    if (-not $antdAlive) { Write-Host "  antd (PID $($state.antd_pid)): not running" -ForegroundColor Gray }
    exit 1
}

Write-Host ""
Write-Host "=== Restarting indelible (data preserved) ===" -ForegroundColor Cyan
Write-Host ""

# ── 1. Stop indelible ──

Write-Host "[1/3] Stopping indelible (PID $($state.indelible_pid))..." -ForegroundColor Yellow
try { taskkill /F /T /PID $state.indelible_pid 2>$null | Out-Null } catch {}
# Clear old log but keep DB
Remove-Item $indelibleLog -Force -ErrorAction SilentlyContinue
Write-Host "       Stopped" -ForegroundColor Green

# ── 2. Rebuild ──

Write-Host "[2/3] Rebuilding frontend + indelible..." -ForegroundColor Yellow
Push-Location "$indelibleRoot\web"
npm run build 2>&1 | Out-Null
Pop-Location
Push-Location $indelibleRoot
go build -buildvcs=false -o "$env:TEMP\indelible-e2e.exe" ./cmd/indelible
if ($LASTEXITCODE -ne 0) {
    Write-Host "       Build failed!" -ForegroundColor Red
    Pop-Location
    exit 1
}
Pop-Location
Write-Host "       Built" -ForegroundColor Green

# ── 3. Start indelible (same DB) ──

Write-Host "[3/3] Starting indelible..." -ForegroundColor Yellow

$testDataDir = $state.data_dir
$indelibleEnv = @{
    INDELIBLE_JWT_SECRET           = "e2e-test-secret-key-at-least-32-characters-long"
    INDELIBLE_WALLET_ENCRYPTION_KEY = "e2e0test0key0000e2e0test0key0000e2e0test0key0000e2e0test0key00000"
    INDELIBLE_ANTD_URL             = "http://localhost:8082"
    INDELIBLE_DB_URL               = "sqlite://$testDataDir/e2e.db"
    INDELIBLE_DATA_DIR             = $testDataDir
    INDELIBLE_PORT                 = "8080"
    INDELIBLE_DEBUG                = "true"
}

$ipsi = New-Object System.Diagnostics.ProcessStartInfo
$ipsi.FileName = "$env:TEMP\indelible-e2e.exe"
$ipsi.UseShellExecute = $false
$ipsi.RedirectStandardOutput = $true
$ipsi.RedirectStandardError = $true
$ipsi.CreateNoWindow = $true
foreach ($entry in [System.Environment]::GetEnvironmentVariables().GetEnumerator()) {
    $ipsi.Environment[$entry.Key] = $entry.Value
}
foreach ($entry in $indelibleEnv.GetEnumerator()) {
    $ipsi.Environment[$entry.Key] = $entry.Value
}

$indelibleProcess = [System.Diagnostics.Process]::Start($ipsi)
$indelibleProcess.BeginOutputReadLine()
$indelibleProcess.BeginErrorReadLine()
Register-ObjectEvent -InputObject $indelibleProcess -EventName OutputDataReceived -Action {
    if ($EventArgs.Data) { $EventArgs.Data | Out-File -Append -FilePath "$env:TEMP\indelible-e2e-indelible.log" }
} | Out-Null
Register-ObjectEvent -InputObject $indelibleProcess -EventName ErrorDataReceived -Action {
    if ($EventArgs.Data) { $EventArgs.Data | Out-File -Append -FilePath "$env:TEMP\indelible-e2e-indelible.log" }
} | Out-Null

Write-Host "       PID $($indelibleProcess.Id)" -ForegroundColor Gray

# Wait for indelible
$indelibleReady = $false
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Seconds 1
    try {
        $null = Invoke-WebRequest http://localhost:8080/api/docs/ -ErrorAction SilentlyContinue
        $indelibleReady = $true; break
    } catch {}
}
if (-not $indelibleReady) {
    Write-Host "       indelible did not respond!" -ForegroundColor Red
    Write-Host "       Check: $indelibleLog" -ForegroundColor Gray
    exit 1
}

# Update PID file (keep devnet/antd PIDs, update indelible PID)
@{
    devnet_pid    = $state.devnet_pid
    antd_pid      = $state.antd_pid
    indelible_pid = $indelibleProcess.Id
    token         = $state.token
    data_dir      = $testDataDir
} | ConvertTo-Json | Set-Content $pidFile

Write-Host ""
Write-Host "=== indelible restarted ===" -ForegroundColor Green
Write-Host ""
Write-Host "  Indelible:  http://localhost:8080" -ForegroundColor White
Write-Host "  Data:       $testDataDir" -ForegroundColor Gray
Write-Host "  Log:        Get-Content $indelibleLog -Tail 20 -Wait" -ForegroundColor Gray
Write-Host ""
Write-Host "  Login:      admin@e2e-test.local / e2e-test-password-123" -ForegroundColor White
Write-Host "  Token:      $($state.token)" -ForegroundColor Gray
Write-Host ""
