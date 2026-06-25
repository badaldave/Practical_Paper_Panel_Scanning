# GPU Deployment — OCR Worker

How to run the Python OCR worker on GPU. The worker is **GPU-agnostic**: the same
image runs on any NVIDIA card and auto-falls back to CPU if no GPU is visible.
There is no per-GPU calibration — you only match CUDA versions once.

Default target: **CUDA 11.8** (the version `paddlepaddle-gpu 2.6.2` supports; it does
**not** support CUDA 13.x — that mismatch is why the host venv fell back to CPU).

## 1. Host prerequisites (one-time, per GPU machine)

You do **not** install Python or CUDA on the host — the image carries them. You only need:

1. **NVIDIA driver** new enough for CUDA 11.8 (driver >= 520 is safe).
   Verify: `nvidia-smi` shows the GPU.
2. **NVIDIA Container Toolkit** so Docker can pass the GPU into containers:
   - Linux: install `nvidia-container-toolkit`, then `sudo nvidia-ctk runtime configure --runtime=docker && sudo systemctl restart docker`.
   - Windows: Docker Desktop with the **WSL2** backend + NVIDIA driver for WSL.
3. Confirm Docker can see the GPU:
   ```bash
   docker run --rm --gpus all nvidia/cuda:11.8.0-base-ubuntu22.04 nvidia-smi
   ```

## 2. Build & run

From the repo root:
```bash
# DB + Go backend (backend already runs with MOCK_WORKER=false)
docker compose up -d db go-backend

# Build + start the GPU worker (base + GPU override together)
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build python-worker
```

The worker container restarts automatically (`restart: unless-stopped`) and runs the
self-healing reaper, so the job queue is never left unattended.

## 3. Verify it's actually on GPU

```bash
# PyTorch sees CUDA?
docker exec university_ocr_worker python -c "import torch; print('torch cuda:', torch.cuda.is_available(), torch.cuda.get_device_name(0) if torch.cuda.is_available() else '')"

# PaddlePaddle health check (prints 'PaddlePaddle works well on N GPUs')
docker exec university_ocr_worker python -c "import paddle; paddle.utils.run_check()"

# Watch the log — provider init + per-job timings
docker logs -f university_ocr_worker
```
If `torch.cuda.is_available()` is `False` or Paddle reports CPU, see Troubleshooting.

## 4. Switching CUDA version later

All three sources must agree (base image / Paddle index / Torch index). Example for CUDA 12.3:
```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml build \
  --build-arg CUDA_TAG=12.3.2-cudnn9-runtime-ubuntu22.04 \
  --build-arg PADDLE_INDEX=https://www.paddlepaddle.org.cn/packages/stable/cu123/ \
  --build-arg TORCH_INDEX=https://download.pytorch.org/whl/cu121 \
  python-worker
```
(`paddlepaddle-gpu 2.6.2` supports cu118 / cu120 / cu123 only.)

## 5. Troubleshooting

- **`could not select device driver "nvidia"`** → NVIDIA Container Toolkit not installed/configured (step 1.2).
- **`torch.cuda.is_available()` is False** → host driver too old for the image's CUDA, or the toolkit isn't passing the GPU. Re-run the step 1.3 check.
- **Paddle uses CPU but torch sees GPU (or vice-versa)** → a version mismatch; rebuild with a consistent `CUDA_TAG` / `PADDLE_INDEX` / `TORCH_INDEX` triplet.
- **`surya-ocr` install fails / torch version conflict at build** → surya 0.17.1 needs a fairly recent torch; if cu118 can't satisfy it, move to the CUDA 12.x triplet above.
- **Worker runs but processes nothing** → check it dequeues: `docker logs university_ocr_worker`. The Go backend must have `MOCK_WORKER=false` (it does) so it doesn't also process jobs.

## Notes

- Don't run the host venv worker (`run_local_worker.ps1/.sh`) **and** this container at the
  same time against the same DB — both would poll. The `FOR UPDATE SKIP LOCKED` claim
  prevents double-processing a single job, but pick one worker per environment.
- The host launchers remain for bare-metal/dev use; this container path is the
  recommended production deployment.
