// Package apinewsfake は api-news を取り込む consumer / publisher 双方のテスト用 fake。
//
// 本番では news は news-article-collected を購読する側で publish はしないが、本 fake は
// 「publisher (newsfeed 等) が ArticleCollectedEvent を発行する状況」をプロセス内で再現するための
// 在庫を提供する。news 自身の subscriber テストや、ArticleCollectedEvent の wire 形状に依存する
// consumer テストで利用する。
package apinewsfake

import "sync"

// Broker は topic ベースの in-memory pub/sub。
type Broker struct {
	mu      sync.Mutex
	byTopic map[string][]chan []byte
}

func NewBroker() *Broker {
	return &Broker{byTopic: make(map[string][]chan []byte)}
}

// Publish は topic に subscribe 中の全 channel に data を配信する (満杯時はブロック)。
func (b *Broker) Publish(topic string, data []byte) {
	b.mu.Lock()
	subs := append([]chan []byte(nil), b.byTopic[topic]...)
	b.mu.Unlock()
	for _, ch := range subs {
		ch <- data
	}
}

// Subscribe は topic に対する新しい受信 channel を返す。複数呼びで fan-out される。
func (b *Broker) Subscribe(topic string) <-chan []byte {
	ch := make(chan []byte, defaultSubscribeBuffer)
	b.mu.Lock()
	b.byTopic[topic] = append(b.byTopic[topic], ch)
	b.mu.Unlock()
	return ch
}

const defaultSubscribeBuffer = 100
