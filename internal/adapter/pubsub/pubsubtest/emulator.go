// Package pubsubtest は gcloud Pub/Sub emulator を testcontainers で起動し、
// publisher / subscriber 両方を含むテストから再利用できるヘルパを提供する。
// `net/http/httptest` の pubsub 版として位置付け、リポ固有のロジックは持たない。
//
// 設計方針:
//   - production コードは `PUBSUB_EMULATOR_HOST` を見る gcloud SDK の仕様に依存。
//     StartEmulator 時に global に os.Setenv する (TestMain scope なので
//     テスト並列化の影響を受けない)。
//   - topic 名はテスト呼び出しごとに UUID suffix を付けて隔離。並列テストでの
//     メッセージ混線を構造的に防ぐ。
//   - Subscription は内部で Subscriber.Receive を goroutine で回し、buffered
//     channel 経由で受信。テスト側は context timeout で待機するか Ch() を直接
//     select で消費する。API は timeout を error 返しで表現し、negative test
//     (「publish されないことを検証」) を書けるようにする。
//   - auto-ack がデフォルト。WithManualAck オプションで consumer 側の ack
//     タイミング検証も可能にする。
package pubsubtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gpubsub "cloud.google.com/go/pubsub/v2"
	pubsubpb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ErrTimeout は WaitForMessage / WaitForN が timeout に達した場合に返る。
// negative test (「publish されないこと」) では assert.ErrorIs で明示的に受ける。
var ErrTimeout = errors.New("pubsubtest: timeout waiting for message")

const (
	emulatorImage    = "gcr.io/google.com/cloudsdktool/cloud-sdk:emulators"
	emulatorPort     = "8085/tcp"
	emulatorLogReady = "Server started, listening on 8085"
)

// Emulator は起動済み Pub/Sub emulator container のハンドル。
type Emulator struct {
	container testcontainers.Container
	projectID string
	host      string
	client    *gpubsub.Client
}

// StartEmulator は gcloud pubsub emulator container を起動し、接続可能な
// Client を準備する。PUBSUB_EMULATOR_HOST を process global に設定するため、
// production コード側の gpubsub.NewClient(projectID) がそのまま emulator に向く。
func StartEmulator(ctx context.Context, projectID string) (*Emulator, error) {
	if projectID == "" {
		return nil, errors.New("pubsubtest: projectID is required")
	}

	req := testcontainers.ContainerRequest{
		Image:        emulatorImage,
		ExposedPorts: []string{emulatorPort},
		Cmd: []string{
			"gcloud", "beta", "emulators", "pubsub", "start",
			"--host-port=0.0.0.0:8085",
			"--project=" + projectID,
		},
		WaitingFor: wait.ForLog(emulatorLogReady).WithStartupTimeout(90 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("start pubsub emulator container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("container host: %w", err), container.Terminate(ctx))
	}
	mapped, err := container.MappedPort(ctx, "8085")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("mapped port: %w", err), container.Terminate(ctx))
	}
	endpoint := fmt.Sprintf("%s:%s", host, mapped.Port())

	// production コードの gpubsub.NewClient(projectID) を何も変えずに emulator へ
	// 向けるため global に設定する。TestMain scope なので他テストへの漏れは
	// 実質起きない (パッケージ横断では各 TestMain が自分の emulator を起動する)。
	if err := os.Setenv("PUBSUB_EMULATOR_HOST", endpoint); err != nil {
		return nil, fmt.Errorf("setenv PUBSUB_EMULATOR_HOST: %w", err)
	}

	client, err := newEmulatorClient(ctx, projectID, endpoint)
	if err != nil {
		return nil, errors.Join(err, container.Terminate(ctx))
	}

	return &Emulator{
		container: container,
		projectID: projectID,
		host:      endpoint,
		client:    client,
	}, nil
}

// newEmulatorClient は emulator 用の認証なし gRPC 接続を持つ Client を構築する。
// production コードと挙動を揃えたい場合は PUBSUB_EMULATOR_HOST 経由で
// gpubsub.NewClient(ctx, projectID) を使えばよい。この関数は testutil 内で
// topic/subscription 管理にのみ使う。
func newEmulatorClient(ctx context.Context, projectID, endpoint string) (*gpubsub.Client, error) {
	opts := []option.ClientOption{
		option.WithEndpoint(endpoint),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	}
	client, err := gpubsub.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("new pubsub client: %w", err)
	}
	return client, nil
}

// Close は client と container を停止する。集約したエラーを返す。
func (e *Emulator) Close(ctx context.Context) error {
	var errs []error
	if err := e.client.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close client: %w", err))
	}
	if err := e.container.Terminate(ctx); err != nil {
		errs = append(errs, fmt.Errorf("terminate container: %w", err))
	}
	if err := os.Unsetenv("PUBSUB_EMULATOR_HOST"); err != nil {
		errs = append(errs, fmt.Errorf("unsetenv PUBSUB_EMULATOR_HOST: %w", err))
	}
	return errors.Join(errs...)
}

// ProjectID は emulator 起動時に指定した project ID を返す。
func (e *Emulator) ProjectID() string { return e.projectID }

// Host は PUBSUB_EMULATOR_HOST に設定した endpoint (host:port) を返す。
func (e *Emulator) Host() string { return e.host }

// Client は topic / subscription 管理用の emulator 直結 Client を返す。
// production コードと同じ挙動を再現したい場合は PUBSUB_EMULATOR_HOST 経由で
// gpubsub.NewClient(ctx, projectID) を別途生成すること。
func (e *Emulator) Client() *gpubsub.Client { return e.client }

// CreateTopic は prefix + UUID suffix のユニークな topic を作成して完全な
// topic ID (short name) を返す。並列テストでの topic 名衝突を構造的に防ぐ。
func (e *Emulator) CreateTopic(t *testing.T, prefix string) string {
	t.Helper()
	topicID := fmt.Sprintf("%s-%s", prefix, uuid.NewString()[:8])
	ctx := context.Background()
	_, err := e.client.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{
		Name: fmt.Sprintf("projects/%s/topics/%s", e.projectID, topicID),
	})
	if err != nil {
		t.Fatalf("create topic %s: %v", topicID, err)
	}
	return topicID
}

// CreateSubscription は指定 topic に対する UUID suffix 付き subscription を
// 作成し、完全な subscription ID を返す。news のように subscriber が事前に
// 作成された subscription を購読するケース用。
func (e *Emulator) CreateSubscription(t *testing.T, topicID, prefix string) string {
	t.Helper()
	subID := fmt.Sprintf("%s-%s", prefix, uuid.NewString()[:8])
	ctx := context.Background()
	_, err := e.client.SubscriptionAdminClient.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name:  fmt.Sprintf("projects/%s/subscriptions/%s", e.projectID, subID),
		Topic: fmt.Sprintf("projects/%s/topics/%s", e.projectID, topicID),
	})
	if err != nil {
		t.Fatalf("create subscription %s: %v", subID, err)
	}
	t.Cleanup(func() {
		_ = e.client.SubscriptionAdminClient.DeleteSubscription(context.Background(), &pubsubpb.DeleteSubscriptionRequest{
			Subscription: fmt.Sprintf("projects/%s/subscriptions/%s", e.projectID, subID),
		})
	})
	return subID
}

// Publish は topic に 1 メッセージを送信し、publish の完了を待って返す。
// テスト内で「publish してから受信側を検証する」シーケンスを直感的に書くための helper。
func (e *Emulator) Publish(t *testing.T, topicID string, data []byte) {
	t.Helper()
	ctx := context.Background()
	publisher := e.client.Publisher(topicID)
	defer publisher.Stop()
	result := publisher.Publish(ctx, &gpubsub.Message{Data: data})
	if _, err := result.Get(ctx); err != nil {
		t.Fatalf("publish to %s: %v", topicID, err)
	}
}

// SubscribeOption は Subscribe の挙動を構成する。
type SubscribeOption func(*subscribeOpts)

type subscribeOpts struct {
	manualAck bool
}

// WithManualAck は受信メッセージを自動 ack せず、テスト側で Message.Ack() を
// 呼ぶまで保留する。consumer 側の ack タイミング検証 (ack 前に DB 書き込み
// 等) 用途のために用意する。
func WithManualAck() SubscribeOption {
	return func(o *subscribeOpts) { o.manualAck = true }
}

// Subscribe は指定 topic に UUID suffix 付きの subscription を作成し、
// 受信ループを起動する。t.Cleanup で停止と subscription 削除を登録する。
// ここで内部生成する subscription は Subscription ハンドル経由でしか
// アクセスできない (production の subscriber とは別経路)。
func (e *Emulator) Subscribe(t *testing.T, topicID string, opts ...SubscribeOption) *Subscription {
	t.Helper()
	o := &subscribeOpts{}
	for _, opt := range opts {
		opt(o)
	}

	subID := fmt.Sprintf("%s-sub-%s", topicID, uuid.NewString()[:8])
	ctx := context.Background()
	_, err := e.client.SubscriptionAdminClient.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name:  fmt.Sprintf("projects/%s/subscriptions/%s", e.projectID, subID),
		Topic: fmt.Sprintf("projects/%s/topics/%s", e.projectID, topicID),
	})
	if err != nil {
		t.Fatalf("create subscription %s: %v", subID, err)
	}

	// buffered channel で receive goroutine の blocking を避ける。サイズは
	// 想定される 1 テスト内のメッセージ数に対して十分大きい値。
	ch := make(chan *Message, 64)
	receiveCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	subscriber := e.client.Subscriber(subID)
	go func() {
		defer close(done)
		// Receive は長時間ブロッキング。ctx キャンセルで return する。
		_ = subscriber.Receive(receiveCtx, func(_ context.Context, m *gpubsub.Message) {
			msg := &Message{
				Data:       m.Data,
				Attributes: m.Attributes,
			}
			if o.manualAck {
				msg.Ack = m.Ack
			} else {
				m.Ack()
				msg.Ack = func() {}
			}
			// receive ctx が終わっているなら push せずに抜ける (shutdown race)。
			select {
			case ch <- msg:
			case <-receiveCtx.Done():
			}
		})
	}()

	s := &Subscription{
		ch:     ch,
		cancel: cancel,
		done:   done,
	}
	t.Cleanup(func() {
		s.stop()
		// subscription 削除は best-effort (container ごと捨てるので必須ではない)。
		_ = e.client.SubscriptionAdminClient.DeleteSubscription(context.Background(), &pubsubpb.DeleteSubscriptionRequest{
			Subscription: fmt.Sprintf("projects/%s/subscriptions/%s", e.projectID, subID),
		})
	})
	return s
}

// Message は subscription で受信したメッセージのテスト用ビュー。
type Message struct {
	Data       []byte
	Attributes map[string]string
	// Ack は WithManualAck 指定時のみ意味を持つ。デフォルトは no-op。
	Ack func()
}

// Subscription は受信 goroutine と channel を保持する。
type Subscription struct {
	ch     chan *Message
	cancel context.CancelFunc
	done   chan struct{}

	stopOnce sync.Once
}

func (s *Subscription) stop() {
	s.stopOnce.Do(func() {
		s.cancel()
		<-s.done
	})
}

// Ch は受信メッセージを流すチャンネルを返す。複雑な assertion (順序・属性・
// 条件マッチ) は caller 側で select + timeout して書く。
func (s *Subscription) Ch() <-chan *Message { return s.ch }

// WaitForMessage は timeout 以内に 1 件届くのを待つ。届かなければ
// ErrTimeout を返す。ctx キャンセル時は ctx.Err() を返す。
func (s *Subscription) WaitForMessage(ctx context.Context, timeout time.Duration) (*Message, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case msg := <-s.ch:
		return msg, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	}
}

// WaitForN は timeout 以内に n 件届くのを待つ。不足時は届いた分を部分結果
// として返しつつ ErrTimeout を返す (debug 時に「何件まで届いたか」を
// 観測可能にする)。
func (s *Subscription) WaitForN(ctx context.Context, n int, timeout time.Duration) ([]*Message, error) {
	if n <= 0 {
		return nil, fmt.Errorf("pubsubtest: n must be positive, got %d", n)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msgs := make([]*Message, 0, n)
	for len(msgs) < n {
		select {
		case msg := <-s.ch:
			msgs = append(msgs, msg)
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return msgs, ErrTimeout
			}
			return msgs, ctx.Err()
		}
	}
	return msgs, nil
}

// String は assertion / debug 用。
func (m *Message) String() string {
	var b strings.Builder
	b.WriteString("pubsub.Message{Data=")
	if len(m.Data) > 80 {
		b.Write(m.Data[:80])
		b.WriteString("...")
	} else {
		b.Write(m.Data)
	}
	if len(m.Attributes) > 0 {
		b.WriteString(", Attributes=")
		fmt.Fprintf(&b, "%v", m.Attributes)
	}
	b.WriteString("}")
	return b.String()
}
