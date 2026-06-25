# Windows launcher for the Python OCR worker — with auto-restart supervision.
# The worker is a long-running daemon; if it ever crashes it is restarted
# automatically so the job queue never goes unattended.

# Set up environment variables
$env:DATABASE_URL="postgresql://postgres:postgres_secure_db_pass_2026@localhost:5439/university_ocr"
$env:PYTHONPATH="$PSScriptRoot\python-worker"

# Prepend CUDA paths so that torch and paddleocr can find CUDA libraries.
$cudaBinPath = "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3\bin"
$cudaBinX64Path = "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3\bin\x64"
$cudaLibnvvpPath = "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3\libnvvp"
if (Test-Path $cudaBinPath) {
    $env:PATH = "$cudaBinPath;$cudaBinX64Path;$cudaLibnvvpPath;" + $env:PATH
    Write-Host "Injected CUDA v13.3 paths (including bin\x64) into PATH." -ForegroundColor Yellow
}

Set-Location "$PSScriptRoot\python-worker"

# Supervision loop: keep the daemon alive across crashes.
while ($true) {
    Write-Host "Starting Python OCR Worker Daemon (GPU enabled)..." -ForegroundColor Cyan
    & .\venv\Scripts\python -u app/main.py
    $code = $LASTEXITCODE
    Write-Host "Worker exited (code $code). Restarting in 5s..." -ForegroundColor Red
    Start-Sleep -Seconds 5
}
