## Tear down the indelible E2E test environment.
##
## Stops indelible, antd, and ant-devnet processes started by dev-e2e.ps1.

$ErrorActionPreference = "SilentlyContinue"

Write-Host ""
Write-Host "=== Tearing down E2E environment ===" -ForegroundColor Cyan
Write-Host ""

$pidFile = "$env:TEMP\indelible-e2e-pids.json"

if (Test-Path $pidFile) {
    $pids = Get-Content $pidFile -Raw | ConvertFrom-Json

    Write-Host "[1/3] Stopping indelible (PID $($pids.indelible_pid))..." -ForegroundColor Yellow
    if ($pids.indelible_pid) { taskkill /F /T /PID $pids.indelible_pid 2>$null | Out-Null }
    Write-Host "       Done" -ForegroundColor Green

    Write-Host "[2/3] Stopping antd (PID $($pids.antd_pid))..." -ForegroundColor Yellow
    if ($pids.antd_pid) { taskkill /F /T /PID $pids.antd_pid 2>$null | Out-Null }
    Write-Host "       Done" -ForegroundColor Green

    Write-Host "[3/3] Stopping ant devnet (PID $($pids.devnet_pid))..." -ForegroundColor Yellow
    if ($pids.devnet_pid) { taskkill /F /T /PID $pids.devnet_pid 2>$null | Out-Null }
    Write-Host "       Done" -ForegroundColor Green

    Remove-Item $pidFile -Force
} else {
    Write-Host "No PID file found -- killing by process name..." -ForegroundColor Yellow
    Get-Process -Name "indelible-e2e" -ErrorAction SilentlyContinue | Stop-Process -Force
    Get-Process -Name antd -ErrorAction SilentlyContinue | Stop-Process -Force
    Get-Process -Name "ant-devnet" -ErrorAction SilentlyContinue | Stop-Process -Force
    Write-Host "       Done" -ForegroundColor Green
}

# Always kill Anvil (EVM testnet) -- ant-devnet spawns it as a child process
# and taskkill /T doesn't always catch it
Get-Process -Name anvil -ErrorAction SilentlyContinue | Stop-Process -Force

# Clean up temp files
foreach ($f in @(
    "$env:TEMP\devnet-manifest.json",
    "$env:TEMP\indelible-e2e-devnet.log",
    "$env:TEMP\indelible-e2e-devnet.log.err",
    "$env:TEMP\indelible-e2e-antd.log",
    "$env:TEMP\indelible-e2e-indelible.log",
    "$env:TEMP\indelible-e2e.exe"
)) {
    Remove-Item $f -Force -ErrorAction SilentlyContinue
}

# Clean up test data
$pidData = "$env:TEMP\indelible-e2e-data"
if (Test-Path $pidData) { Remove-Item $pidData -Recurse -Force -ErrorAction SilentlyContinue }

Write-Host ""
Write-Host "=== E2E environment torn down ===" -ForegroundColor Cyan
Write-Host ""
