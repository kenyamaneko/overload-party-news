package port

import "context"

// MessageHandler は 1 メッセージの処理結果を ack / nack の形で返す。
// nil = ack、非 nil = nack (再配信を要するケース)。
type MessageHandler = func(ctx context.Context, data []byte) error

// MessageStream は Pub/Sub subscription の抽象境界。Cloud Pub/Sub SDK 型に依存しないために挟む。
type MessageStream interface {
	// Consume は ctx がキャンセルされるまで handler をメッセージ毎に呼び、戻り値で ack / nack を制御する。
	Consume(ctx context.Context, handler MessageHandler) error
}
