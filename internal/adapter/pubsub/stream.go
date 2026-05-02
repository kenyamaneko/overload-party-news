// Package pubsub は Cloud Pub/Sub subscription への adapter。
// port / usecase 層が Pub/Sub のライブラリに直接依存しないよう、
// 「バイトを受けて handler に委譲する Stream」として抽象化する。
package pubsub

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/pubsub/v2"

	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// Stream は Cloud Pub/Sub subscription を port.MessageStream として
// 露出する adapter。cloud.google.com/go/pubsub/v2 の SDK 型依存は本 adapter に
// 閉じ、handler 本体 (subscriber 層) は SDK を知らなくて済む。
type Stream struct {
	client     *pubsub.Client
	subscriber *pubsub.Subscriber
}

// NewStream は指定 projectID / subscriptionID に接続した Stream を返す。
// subscription / topic の作成は行わない (infra 側で事前に作成する前提)。
// Close() 呼び出しまで内部 *pubsub.Client が保持されるため、main.go 側で defer Close が必須。
func NewStream(ctx context.Context, projectID, subscriptionID string) (*Stream, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("pubsub: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub: new client: %w", err)
	}
	return &Stream{
		client:     client,
		subscriber: client.Subscriber(subscriptionID),
	}, nil
}

// Consume は ctx がキャンセルされるまでメッセージを受信し続ける blocking call。
// handler が nil を返した場合は ACK、non-nil を返した場合は NACK する。
// ctx キャンセル時は Receive が nil を返すため、Consume も nil で終了する。
func (s *Stream) Consume(ctx context.Context, handler port.MessageHandler) error {
	err := s.subscriber.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
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

// Close は Cloud Pub/Sub client を閉じる。Consume のブロックが解放された後に呼ぶ。
func (s *Stream) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("pubsub client close: %w", err)
	}
	return nil
}
