#!/usr/bin/env bash
# push-image.sh — ビルド済み Docker イメージを Artifact Registry に push する。
# 入力: IMAGE, SHA_TAG, LATEST_TAG (env)
set -euo pipefail

docker push "${IMAGE}:${SHA_TAG}"
docker push "${IMAGE}:${LATEST_TAG}"
