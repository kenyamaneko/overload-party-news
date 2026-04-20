// Package pubsub は Google Cloud Pub/Sub への subscriber アダプタ。
// port / service 層が Pub/Sub のライブラリに直接依存しないよう、
// 「バイトを受けて handler に委譲する Receiver」として抽象化する。
package pubsub

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
)

// MessageHandler はイベントメッセージ 1 件の処理を行う関数型。
// nil を返せば ACK、non-nil を返せば NACK する (Pub/Sub 側でリトライまたは DLQ 行き)。
// deterministic error (再送無意味) を nil として返す判断は呼び出し側 (handler 層) が行う。
type MessageHandler func(ctx context.Context, data []byte) error

// Subscriber は subscription ID を 1 つ持ち、Receive を呼ぶと handler にメッセージを流し込む。
type Subscriber struct {
	client *pubsub.Client
	subID  string
}

// New は指定プロジェクト / subscription に接続する Subscriber を生成する。
// subscription / topic の作成は行わない (infra 側で事前に作成する前提)。
func New(ctx context.Context, projectID, subscriptionID string) (*Subscriber, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub client: %w", err)
	}
	return &Subscriber{client: client, subID: subscriptionID}, nil
}

// Receive は ctx がキャンセルされるまでメッセージを受信し続ける blocking call。
// handler が nil を返した場合は ACK、non-nil を返した場合は NACK する。
func (s *Subscriber) Receive(ctx context.Context, handler MessageHandler) error {
	sub := s.client.Subscriber(s.subID)
	err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		if err := handler(ctx, msg.Data); err != nil {
			msg.Nack()
			return
		}
		msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("pubsub receive: %w", err)
	}
	return nil
}

// Close は背後の client を閉じる。Receive のブロックが解放された後に呼ぶ。
func (s *Subscriber) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("pubsub client close: %w", err)
	}
	return nil
}
