#!/usr/bin/env bash
# resolve-env.sh — ブランチ名から対応する環境名を判定する。
# 入力: GITHUB_REF (env), 出力: GITHUB_OUTPUT に "env_name=..." を書き込み。
set -euo pipefail

case "${GITHUB_REF}" in
  refs/heads/main)
    echo "env_name=prod" >> "$GITHUB_OUTPUT"
    ;;
  refs/heads/release/*)
    echo "env_name=stg" >> "$GITHUB_OUTPUT"
    ;;
  refs/heads/develop)
    echo "env_name=dev" >> "$GITHUB_OUTPUT"
    ;;
  *)
    echo "::error::Unsupported branch: ${GITHUB_REF}"
    exit 1
    ;;
esac
