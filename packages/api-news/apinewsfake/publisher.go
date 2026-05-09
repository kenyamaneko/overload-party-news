package apinewsfake

import (
	"context"
	"sync"
)

// PublishedMessage は Publisher.Publish 1 回分の記録。
type PublishedMessage struct {
	Topic string
	Data  []byte
}

// Publisher は送信側のテスト用 fake。Broker 配信に加え発行記録を残す。
type Publisher struct {
	broker    *Broker
	mu        sync.Mutex
	published []PublishedMessage
}

func NewPublisher(broker *Broker) *Publisher {
	return &Publisher{broker: broker}
}

// Publish は data を topic に発行し、Published() で取り出せる記録を残す。
func (p *Publisher) Publish(_ context.Context, topic string, data []byte) error {
	buf := append([]byte(nil), data...)
	p.mu.Lock()
	p.published = append(p.published, PublishedMessage{Topic: topic, Data: buf})
	p.mu.Unlock()
	p.broker.Publish(topic, buf)
	return nil
}

// Published は Publisher が発行したメッセージの snapshot (caller mutation 独立) を返す。
func (p *Publisher) Published() []PublishedMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PublishedMessage, len(p.published))
	for i, m := range p.published {
		out[i] = PublishedMessage{
			Topic: m.Topic,
			Data:  append([]byte(nil), m.Data...),
		}
	}
	return out
}
