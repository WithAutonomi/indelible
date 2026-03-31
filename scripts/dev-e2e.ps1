## Start a full indelible + antd + devnet environment for end-to-end testing.
##
## Starts the ant devnet, antd, and indelible, then registers an admin user
## and configures the devnet wallet so uploads actually pay on-chain.
##
## Prerequisites:
##   - Go toolchain
##   - Rust toolchain (cargo)
##   - ant-node repo cloned as sibling: ../../ant-node (or set $env:ANT_NODE_DIR)
##   - ant-sdk repo cloned as sibling: ../../ant-sdk  (or set $env:ANT_SDK_DIR)
##   - curl + jq on PATH
##
## Usage:
##   .\scripts\dev-e2e.ps1
##
## Tear down:
##   .\scripts\dev-e2e-stop.ps1

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$indelibleRoot = (Resolve-Path "$scriptDir\..").Path
$pidFile = "$env:TEMP\indelible-e2e-pids.json"
$manifestFile = "$env:TEMP\devnet-manifest.json"
$devnetLog = "$env:TEMP\indelible-e2e-devnet.log"
$antdLog = "$env:TEMP\indelible-e2e-antd.log"
$indelibleLog = "$env:TEMP\indelible-e2e-indelible.log"

# ── Resolve sibling repos ──

if ($env:ANT_SDK_DIR) {
    $sdkRoot = $env:ANT_SDK_DIR
} else {
    $candidate = "$indelibleRoot\..\ant-sdk"
    if (Test-Path "$candidate\antd\Cargo.toml") {
        $sdkRoot = (Resolve-Path $candidate).Path
    } else {
        Write-Host "ERROR: Cannot find ant-sdk repo. Clone as sibling or set `$env:ANT_SDK_DIR." -ForegroundColor Red
        exit 1
    }
}

if ($env:ANT_NODE_DIR) {
    $antNodeDir = $env:ANT_NODE_DIR
} else {
    $candidate = "$indelibleRoot\..\ant-node"
    if (Test-Path "$candidate\Cargo.toml") {
        $antNodeDir = (Resolve-Path $candidate).Path
    } else {
        Write-Host "ERROR: Cannot find ant-node repo. Clone as sibling or set `$env:ANT_NODE_DIR." -ForegroundColor Red
        exit 1
    }
}

# ── Clean up old files (best-effort — previous run may still hold locks) ──
foreach ($f in @($pidFile, $manifestFile, $devnetLog, "$devnetLog.err", $antdLog, $indelibleLog)) {
    Remove-Item $f -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "=== Indelible E2E Test Environment ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Indelible:  $indelibleRoot" -ForegroundColor Gray
Write-Host "  ant-sdk:    $sdkRoot" -ForegroundColor Gray
Write-Host "  ant-node:   $antNodeDir" -ForegroundColor Gray
Write-Host ""

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 1. Start ant devnet (local network + EVM)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Write-Host "[1/5] Starting ant devnet (25 nodes + EVM)..." -ForegroundColor Yellow
$devnetProc = Start-Process -PassThru -FilePath "cargo" `
    -ArgumentList "run", "--release", "--bin", "ant-devnet", "--", "--preset", "default", "--enable-evm", "--manifest", $manifestFile `
    -WorkingDirectory $antNodeDir `
    -RedirectStandardOutput $devnetLog `
    -RedirectStandardError "$devnetLog.err" `
    -WindowStyle Hidden
Write-Host "       PID $($devnetProc.Id)" -ForegroundColor Gray

# Wait for manifest
Write-Host "       Waiting for devnet (first build may take several minutes)..." -ForegroundColor Gray
$manifest = $null
for ($i = 0; $i -lt 180; $i++) {
    Start-Sleep -Seconds 2
    if (Test-Path $manifestFile) {
        try {
            $manifest = Get-Content $manifestFile -Raw | ConvertFrom-Json
            if ($manifest.bootstrap.Count -gt 0 -and $manifest.evm) { break }
        } catch {}
        $manifest = $null
    }
}

if (-not $manifest) {
    Write-Host "       Timed out waiting for devnet!" -ForegroundColor Red
    Write-Host "       Check: $devnetLog" -ForegroundColor Gray
    exit 1
}

$bootstrapPeers = ($manifest.bootstrap -join ",")
$walletKey = $manifest.evm.wallet_private_key -replace '^0x', ''
$walletAddress = $manifest.evm.wallet_address
$evmRpcUrl = $manifest.evm.rpc_url
$evmTokenAddr = $manifest.evm.payment_token_address
$evmPaymentsAddr = $manifest.evm.data_payments_address

Write-Host "       Devnet ready: $($manifest.node_count) nodes" -ForegroundColor Green
Write-Host "       EVM: $evmRpcUrl" -ForegroundColor Green

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 2. Start antd (no wallet key — external signer only)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Write-Host "[2/5] Starting antd (external signer mode)..." -ForegroundColor Yellow

$antdEnv = @{
    ANTD_PEERS                = $bootstrapPeers
    EVM_RPC_URL               = $evmRpcUrl
    EVM_PAYMENT_TOKEN_ADDRESS = $evmTokenAddr
    EVM_DATA_PAYMENTS_ADDRESS = $evmPaymentsAddr
    # No AUTONOMI_WALLET_KEY — indelible signs payments externally
}

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = "cargo"
$psi.Arguments = "run -- --network local"
$psi.WorkingDirectory = "$sdkRoot\antd"
$psi.UseShellExecute = $false
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.CreateNoWindow = $true
foreach ($entry in [System.Environment]::GetEnvironmentVariables().GetEnumerator()) {
    $psi.Environment[$entry.Key] = $entry.Value
}
foreach ($entry in $antdEnv.GetEnumerator()) {
    $psi.Environment[$entry.Key] = $entry.Value
}

$antdProcess = [System.Diagnostics.Process]::Start($psi)
$antdProcess.BeginOutputReadLine()
$antdProcess.BeginErrorReadLine()
Register-ObjectEvent -InputObject $antdProcess -EventName OutputDataReceived -Action {
    if ($EventArgs.Data) { $EventArgs.Data | Out-File -Append -FilePath "$env:TEMP\indelible-e2e-antd.log" }
} | Out-Null
Register-ObjectEvent -InputObject $antdProcess -EventName ErrorDataReceived -Action {
    if ($EventArgs.Data) { $EventArgs.Data | Out-File -Append -FilePath "$env:TEMP\indelible-e2e-antd.log" }
} | Out-Null

Write-Host "       PID $($antdProcess.Id)" -ForegroundColor Gray

# Wait for antd health (antd may need to compile first -- allow up to 15 minutes)
Write-Host "       Waiting for antd (may need to compile, up to 15 min)..." -ForegroundColor Gray
$antdReady = $false
for ($i = 0; $i -lt 300; $i++) {
    Start-Sleep -Seconds 3
    # Show progress every 30 seconds
    if ($i % 10 -eq 0 -and $i -gt 0) {
        $elapsed = [math]::Round($i * 3 / 60, 1)
        Write-Host ("       Still waiting... ({0}m)" -f $elapsed) -ForegroundColor Gray
    }
    try {
        $health = Invoke-RestMethod http://localhost:8082/health -ErrorAction SilentlyContinue
        if ($health.status -eq "ok") { $antdReady = $true; break }
    } catch {}
    # Check if antd process died
    if ($antdProcess.HasExited) {
        Write-Host "       antd process exited with code $($antdProcess.ExitCode)!" -ForegroundColor Red
        Write-Host "       Check: $antdLog" -ForegroundColor Gray
        exit 1
    }
}
if (-not $antdReady) {
    Write-Host "       antd did not respond within 15 minutes!" -ForegroundColor Red
    Write-Host "       Check: $antdLog" -ForegroundColor Gray
    exit 1
}
Write-Host "       antd ready at http://localhost:8082" -ForegroundColor Green

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 3. Build + start indelible
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Write-Host "[3/5] Building indelible..." -ForegroundColor Yellow
Push-Location $indelibleRoot
go build -buildvcs=false -o "$env:TEMP\indelible-e2e.exe" ./cmd/indelible
if ($LASTEXITCODE -ne 0) {
    Write-Host "       Build failed!" -ForegroundColor Red
    Pop-Location
    exit 1
}
Pop-Location
Write-Host "       Built" -ForegroundColor Green

Write-Host "[4/5] Starting indelible..." -ForegroundColor Yellow

# Use a fresh SQLite DB for each test run
$testDataDir = "$env:TEMP\indelible-e2e-data"
if (Test-Path $testDataDir) { Remove-Item $testDataDir -Recurse -Force }
New-Item -ItemType Directory -Path $testDataDir -Force | Out-Null

$indelibleEnv = @{
    INDELIBLE_JWT_SECRET = "e2e-test-secret-key-at-least-32-characters-long"
    INDELIBLE_ANTD_URL   = "http://localhost:8082"
    INDELIBLE_DB_URL     = "sqlite://$testDataDir/e2e.db"
    INDELIBLE_DATA_DIR   = $testDataDir
    INDELIBLE_PORT       = "8080"
    INDELIBLE_DEBUG      = "true"
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
Write-Host "       indelible ready at http://localhost:8080" -ForegroundColor Green

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 5. Register admin + configure wallet
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Write-Host "[5/5] Registering admin and configuring wallet..." -ForegroundColor Yellow

# Register first user (becomes admin)
$regBody = @{
    email      = "admin@e2e-test.local"
    password   = "e2e-test-password-123"
    first_name = "E2E"
    last_name  = "Admin"
} | ConvertTo-Json

$regResult = Invoke-RestMethod -Uri "http://localhost:8080/api/v2/auth/register" `
    -Method Post -ContentType "application/json" -Body $regBody
$token = $regResult.token

if (-not $token) {
    Write-Host "       Registration failed!" -ForegroundColor Red
    exit 1
}

# Add the devnet wallet (address derived from private key automatically)
$walletBody = @{
    name        = "devnet-e2e"
    private_key = $walletKey
} | ConvertTo-Json

$null = Invoke-RestMethod -Uri "http://localhost:8080/api/v2/admin/wallets" `
    -Method Post -ContentType "application/json" -Body $walletBody `
    -Headers @{ Authorization = "Bearer $token" }

Write-Host "       Admin registered, wallet configured" -ForegroundColor Green

# ── Save state ──
@{
    devnet_pid    = $devnetProc.Id
    antd_pid      = $antdProcess.Id
    indelible_pid = $indelibleProcess.Id
    token         = $token
    data_dir      = $testDataDir
} | ConvertTo-Json | Set-Content $pidFile

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Write-Host ""
Write-Host "=== E2E Environment Ready ===" -ForegroundColor Green
Write-Host ""
Write-Host "  Indelible:  http://localhost:8080" -ForegroundColor White
Write-Host "  antd:       http://localhost:8082" -ForegroundColor White
Write-Host "  Swagger:    http://localhost:8080/api/docs/" -ForegroundColor White
Write-Host ""
Write-Host "  Admin token:" -ForegroundColor White
Write-Host "  $token" -ForegroundColor Gray
Write-Host ""
Write-Host "  Test upload:" -ForegroundColor White
Write-Host "  curl -X POST http://localhost:8080/api/v2/uploads \" -ForegroundColor Gray
Write-Host "    -H 'Authorization: Bearer $($token.Substring(0, 20))...' \" -ForegroundColor Gray
Write-Host "    -F file=@README.md -F visibility=private" -ForegroundColor Gray
Write-Host ""
Write-Host "  Logs:" -ForegroundColor White
Write-Host "  Get-Content $indelibleLog -Tail 20 -Wait" -ForegroundColor Gray
Write-Host "  Get-Content $antdLog -Tail 20 -Wait" -ForegroundColor Gray
Write-Host ""
Write-Host "  Tear down:" -ForegroundColor White
Write-Host "  .\scripts\dev-e2e-stop.ps1" -ForegroundColor Gray
Write-Host ""
