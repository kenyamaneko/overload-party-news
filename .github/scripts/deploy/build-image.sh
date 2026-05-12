#!/usr/bin/env bash
# build-image.sh — Docker イメージをビルドする。
# 入力: IMAGE, SHA_TAG, LATEST_TAG, GO_MODULES_TOKEN (env)
set -euo pipefail

docker build \
  --secret id=GO_MODULES_TOKEN,env=GO_MODULES_TOKEN \
  -t "${IMAGE}:${SHA_TAG}" \
  -t "${IMAGE}:${LATEST_TAG}" \
  .
