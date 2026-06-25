# Installs the OCR worker as an auto-starting Windows background task.
# Run this once, from an elevated (Administrator) PowerShell:
#     powershell -ExecutionPolicy Bypass -File scripts\install-worker-service.ps1
#
# It registers a Scheduled Task that launches run_local_worker.ps1 at boot.
# That launcher already auto-restarts the worker if it crashes, so between the
# two the queue is never left unattended.

$ErrorActionPreference = "Stop"

$RepoRoot   = Split-Path -Parent $PSScriptRoot
$Launcher   = Join-Path $RepoRoot "run_local_worker.ps1"
$TaskName   = "UniversityOCRWorker"

if (-not (Test-Path $Launcher)) {
    throw "Launcher not found at $Launcher"
}

# Run hidden, no profile, bypassing execution policy for just this task.
$action  = New-ScheduledTaskAction -Execute "powershell.exe" `
    -Argument "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$Launcher`""

# Start at system boot AND keep running. RestartCount/Interval cover the rare
# case where the whole powershell host dies (the launcher handles worker crashes).
$trigger  = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries -StartWhenAvailable `
    -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit (New-TimeSpan -Seconds 0)

# Run as SYSTEM so it starts without anyone logged in.
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
    -Settings $settings -Principal $principal -Force | Out-Null

Write-Host "Registered scheduled task '$TaskName'." -ForegroundColor Green
Write-Host "Start it now with:  Start-ScheduledTask -TaskName $TaskName"
Write-Host "View status with:   Get-ScheduledTask -TaskName $TaskName | Get-ScheduledTaskInfo"
Write-Host ""
Write-Host "NOTE: SYSTEM-account tasks may not see a per-user CUDA install. If the" -ForegroundColor Yellow
Write-Host "worker falls back to CPU, run it under your own user account instead" -ForegroundColor Yellow
Write-Host "(re-register with -UserId '<DOMAIN\\you>' -LogonType S4U)." -ForegroundColor Yellow
