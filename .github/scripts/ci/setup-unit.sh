#!/usr/bin/env bash
set -euo pipefail

# Testcontainers はテスト中に自動 pull するが、時間がかかりタイムアウトの原因になるため事前に取得する
docker pull postgres:16-alpine
