package port

import "context"

// MessageHandler は 1 メッセージの処理結果を ack / nack の形で返す。
// nil = ack、非 nil = nack (再配信を要するケース)。
type MessageHandler = func(ctx context.Context, data []byte) error
