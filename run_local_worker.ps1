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

Write-Host "Activating Python Virtual Environment..." -ForegroundColor Green
cd "$PSScriptRoot\python-worker"

Write-Host "Starting Python OCR Worker Daemon (GPU enabled)..." -ForegroundColor Cyan
.\venv\Scripts\python -u app/main.py
