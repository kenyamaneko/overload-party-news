#!/bin/bash
# news が購読する Pub/Sub topic + subscription を emulator に作成する。
# 実環境では newsfeed が topic を publish するが、news 単体起動では publisher が
# 居ないため、購読対象の topic をここで用意して subscription を張る。
# topic / subscription 名は compose の env から受け取り、名前の二重管理を避ける。
set -euo pipefail

PROJECT="${PUBSUB_PROJECT_ID:?compose 経由で PUBSUB_PROJECT_ID を設定すること}"
HOST="${PUBSUB_EMULATOR_HOST:?compose 経由で PUBSUB_EMULATOR_HOST を設定すること}"
TOPIC="${NEWS_ARTICLE_COLLECTED_TOPIC:?compose 経由で NEWS_ARTICLE_COLLECTED_TOPIC を設定すること}"
SUB="${NEWS_ARTICLE_COLLECTED_SUBSCRIPTION:?compose 経由で NEWS_ARTICLE_COLLECTED_SUBSCRIPTION を設定すること}"

# pull subscription の ack 期限 (秒)。news は起動時に stream を開くだけで消費挙動には効かないため、ローカルでは実環境の既定に合わせる。
readonly ACK_DEADLINE_SECS=30

# 既存 (409) は idempotent 再実行として許容し、それ以外の HTTP 失敗は abort する。
put() {
  local url="$1"
  shift
  local code
  code=$(curl -sS -o /dev/null -w '%{http_code}' -X PUT "$url" "$@")
  case "$code" in
    2*|409) return 0 ;;
    *) echo "PUT $url -> HTTP $code" >&2; return 1 ;;
  esac
}

put "http://${HOST}/v1/projects/${PROJECT}/topics/${TOPIC}"
put "http://${HOST}/v1/projects/${PROJECT}/subscriptions/${SUB}" \
  -H "Content-Type: application/json" \
  -d "{\"topic\":\"projects/${PROJECT}/topics/${TOPIC}\",\"ackDeadlineSeconds\":${ACK_DEADLINE_SECS}}"
echo "ready: ${SUB} -> ${TOPIC}"
