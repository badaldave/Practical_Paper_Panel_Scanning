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

# Start at user logon AND keep running. RestartCount/Interval cover the rare
# case where the whole powershell host dies (the launcher handles worker crashes).
$trigger  = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries -StartWhenAvailable `
    -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit (New-TimeSpan -Seconds 0)

# Run as the CURRENT user, in their session, so the worker sees the same venv,
# PaddleOCR model cache (%USERPROFILE%\.paddleocr) and per-user CUDA install it
# gets when launched by hand. Interactive logon = starts when you log in; needs
# no stored password and no admin/elevation to register.
# RunLevel Limited: the worker needs no admin rights (only the user venv/CUDA/
# models), and Limited lets a standard, non-elevated user register their own
# logon task. Use -RunLevel Highest only if you install from an elevated shell.
$CurrentUser = "$env:USERDOMAIN\$env:USERNAME"
$principal = New-ScheduledTaskPrincipal -UserId $CurrentUser -LogonType Interactive -RunLevel Limited

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
    -Settings $settings -Principal $principal -Force | Out-Null

Write-Host "Registered scheduled task '$TaskName' to run as $CurrentUser at logon." -ForegroundColor Green
Write-Host "Start it now with:  Start-ScheduledTask -TaskName $TaskName"
Write-Host "View status with:   Get-ScheduledTask -TaskName $TaskName | Get-ScheduledTaskInfo"
