package apinewsfake

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ChannelArticleCollected は asyncapi.yaml の論理 channel address。
// 物理 topic 名は env (NEWS_ARTICLE_COLLECTED_TOPIC 等) で解決する想定だが、
// fake では Broker の routing key としてそのまま使う。
const ChannelArticleCollected = "news-article-collected"

// PublishArticleCollected は ChannelArticleCollected へ ArticleCollectedEvent を 1 件発行する。
// ArticleID 未設定なら 26 桁 base32 文字列を補完する (他フィールドは caller の値を尊重)。
func PublishArticleCollected(ctx context.Context, p *Publisher, ev apinews.ArticleCollectedEvent) error {
	ev = fillArticleCollectedDefaults(ev)
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal ArticleCollectedEvent: %w", err)
	}
	return p.Publish(ctx, ChannelArticleCollected, data)
}

// ArticleCollectedExpecter は ChannelArticleCollected に subscribe 済みの待受器。
// publish より前に subscribe を確定する必要があるため (Broker は過去メッセージを配信しない)、
// API は ExpectArticleCollected → publish → Wait の順序を強制する形に分割している。
type ArticleCollectedExpecter struct {
	ch <-chan []byte
}

// ExpectArticleCollected は即時 subscribe して Expecter を返す (publish より前に呼ぶこと)。
func ExpectArticleCollected(s *Subscriber) *ArticleCollectedExpecter {
	return &ArticleCollectedExpecter{ch: s.Messages(ChannelArticleCollected)}
}

// Wait は subscribe 後に publish された最初の event を timeout 付きで取り出す。
func (e *ArticleCollectedExpecter) Wait(timeout time.Duration) (apinews.ArticleCollectedEvent, error) {
	return waitTypedFromChan[apinews.ArticleCollectedEvent](e.ch, ChannelArticleCollected, timeout)
}

// waitTypedFromChan は payload bytes を timeout 付きで受信し型 T にデコードする。
func waitTypedFromChan[T any](ch <-chan []byte, topic string, timeout time.Duration) (T, error) {
	var zero T
	select {
	case data, ok := <-ch:
		if !ok {
			return zero, fmt.Errorf("channel closed for topic %q before receiving message", topic)
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return zero, fmt.Errorf("unmarshal %q payload: %w", topic, err)
		}
		return v, nil
	case <-time.After(timeout):
		return zero, fmt.Errorf("timeout waiting for %q after %s", topic, timeout)
	}
}

// fillArticleCollectedDefaults は ArticleID 未設定時に 26 桁 base32 文字列を補完する。
func fillArticleCollectedDefaults(ev apinews.ArticleCollectedEvent) apinews.ArticleCollectedEvent {
	if ev.ArticleID == "" {
		ev.ArticleID = newArticleID()
	}
	return ev
}
