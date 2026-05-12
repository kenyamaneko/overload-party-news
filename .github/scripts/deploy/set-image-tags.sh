#!/usr/bin/env bash
# set-image-tags.sh — デプロイ用の Docker イメージタグを算出する。
# 入力: REGISTRY, PROJECT_ID, REPOSITORY, IMAGE_NAME, ENV_NAME, GITHUB_SHA (env)
# 出力: GITHUB_OUTPUT に image, sha_tag, latest_tag を書き込み。
set -euo pipefail

IMAGE="${REGISTRY}/${PROJECT_ID}/${REPOSITORY}/${IMAGE_NAME}"
echo "image=${IMAGE}" >> "$GITHUB_OUTPUT"
echo "sha_tag=${ENV_NAME}-${GITHUB_SHA}" >> "$GITHUB_OUTPUT"
echo "latest_tag=${ENV_NAME}-latest" >> "$GITHUB_OUTPUT"
