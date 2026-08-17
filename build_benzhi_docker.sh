#!/usr/bin/env bash
set -euo pipefail

NAME="${1:-solar-ops}"
PLATFORM="${2:-linux/amd64}"

IMAGE="benzhi/${NAME}:latest"

echo "[build] ${IMAGE} for ${PLATFORM}"
docker build --platform "${PLATFORM}" -t "${IMAGE}" -f benzhi.Dockerfile .
echo "[done] ${IMAGE}"
