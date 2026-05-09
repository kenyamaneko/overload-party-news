package apinewsfake

import (
	"context"
	"testing"
	"time"
)

// Stream は特定 topic に subscribe 済みの observable stream。
// at-most-once 配信 (nack 後の再配信なし)。subscribe は NewStream で eager に行う。
type Stream struct {
	ch      <-chan []byte
	topic   string
	handled chan error
}

// NewStream は Subscriber から指定 topic のメッセージを読む Stream を返す。
func NewStream(sub *Subscriber, topic string) *Stream {
	return &Stream{
		ch:      sub.Messages(topic),
		topic:   topic,
		handled: make(chan error, handledBufferSize),
	}
}

const handledBufferSize = 16

// Consume は ctx キャンセルまで topic のメッセージを handler に渡し、戻り値を handled channel に流す。
// ctx キャンセル / channel クローズ時は nil を返す。
func (s *Stream) Consume(ctx context.Context, handler func(ctx context.Context, data []byte) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case data, ok := <-s.ch:
			if !ok {
				return nil
			}
			s.handled <- handler(ctx, data)
		}
	}
}

// ExpectHandled は 1 メッセージ分の handler 戻り値を timeout 付きで取り出す (timeout 時 t.Fatal)。
func (s *Stream) ExpectHandled(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-s.handled:
		return err
	case <-time.After(timeout):
		t.Fatalf("apinewsfake.Stream[%s]: timeout waiting for handler completion (%s)", s.topic, timeout)
		return nil
	}
}
