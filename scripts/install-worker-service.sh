#!/usr/bin/env bash
# Installs the OCR worker as an auto-starting systemd service on Linux.
# Run once with sudo:
#     sudo ./scripts/install-worker-service.sh
#
# systemd handles boot-start AND restart-on-crash (Restart=always), and
# run_local_worker.sh adds a second safety loop.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LAUNCHER="$REPO_ROOT/run_local_worker.sh"
SERVICE_NAME="university-ocr-worker"
UNIT_PATH="/etc/systemd/system/${SERVICE_NAME}.service"

# Run the service as the invoking user (so it can see that user's venv/CUDA),
# not root, when launched via sudo.
RUN_USER="${SUDO_USER:-$(id -un)}"

if [ ! -f "$LAUNCHER" ]; then
    echo "Launcher not found at $LAUNCHER" >&2
    exit 1
fi
chmod +x "$LAUNCHER"

cat > "$UNIT_PATH" <<EOF
[Unit]
Description=University OCR Worker (job-queue daemon)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
WorkingDirectory=${REPO_ROOT}
ExecStart=/usr/bin/env bash ${LAUNCHER}
Restart=always
RestartSec=5
# Pass production overrides here or via an EnvironmentFile=
# Environment=DATABASE_URL=postgresql://...

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

echo "Installed and started '$SERVICE_NAME' (running as ${RUN_USER})."
echo "Status: systemctl status ${SERVICE_NAME}"
echo "Logs:   journalctl -u ${SERVICE_NAME} -f"
