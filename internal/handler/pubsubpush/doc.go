// Package pubsubpush は Cloud Run 上で Pub/Sub push subscription を受ける HTTP delivery 層。
// push envelope を解析・復号し、既存の port.MessageHandler へ委譲する。
package pubsubpush
