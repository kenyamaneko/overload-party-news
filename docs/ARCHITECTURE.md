# News サービス設計

本ドキュメントは **コードを読んでも一見しては分からない設計意図** だけを残す。実装詳細（フロー順序・状態遷移・エラー → HTTP ステータス変換・環境変数一覧）は各ファイルの実装とコメントを一次情報とする。

サービス概要・起動手順は [../README.md](../README.md)、保証すべき振る舞いは [FEATURE_SPEC.md](FEATURE_SPEC.md)、エンドポイントは [../data/openapi.yaml](../data/openapi.yaml) (REST) と [../data/asyncapi.yaml](../data/asyncapi.yaml) (Pub/Sub)、テーブル定義は [DATA_DESIGN.md](DATA_DESIGN.md) を参照。

## News の責務境界 (SSoT と書き込み権限)

news は **記事コンテンツ**の single source of truth。`news.news_articles` への初期挿入 (取り込み) は news サービスのみが行う。

| ライフサイクル | 書き手 | 契機 |
|---|---|---|
| 記事の初期挿入 (`status = pending`) | news | `news-article-collected` イベント購読 |
| 承認・却下・編集 | 運用者（DB を直接更新） | 校閲操作 |
| 読み取り（公開配信） | news → gateway | 公開 REST API |

newsfeed（Cloud Run Job）は DB を触らない。収集結果を Pub/Sub 経由で news に引き渡すのみ。これにより:

- news 以外のサービスがスキーマに書き込む経路を持たない（shop と同じ「1 スキーマ 1 所有者」原則）
- newsfeed 側の障害が news の DB 状態を壊さない
- newsfeed の再実行（同一記事の再取得）は Pub/Sub 経由でしか到達しないため、news 側の冪等性制御 1 箇所で吸収できる

## 公開しているポート

news が listen するのは gateway 向け配信 API の 1 ポートだけである。

| ポート | プロトコル | 想定クライアント | 認証 |
|---|---|---|---|
| `:9008` | HTTP JSON | gateway | 内部サービス間 JWT (RS256) |

## インジェスト: Pub/Sub subscriber の冪等性

`news-article-collected` の購読は at-least-once 配送。加えて newsfeed は要約・publish に失敗した `source_url` の予約を解放して次の周期で取り直すため、同じ URL が新しい `article_id` で届くこともある。両方をまとめて吸収するため、記事行と ja 翻訳行を 1 トランザクションで INSERT する。

1. **記事行**: `news_articles` に `INSERT ... ON CONFLICT DO NOTHING`
   - `article_id` は newsfeed が採番した ULID
   - PK の衝突（同じ記事の再送）でも `source_url` の UNIQUE 衝突（同じ URL の採番違い）でも INSERT は no-op になり、**校閲済みの既存行を上書きしない**
2. **取込済みの判定**: 記事行が 1 行入ったかどうかで「新規取込」と「取込済み」を分ける
3. **翻訳行**: 記事行が新規に入ったときだけ `news_article_translations` へ INSERT する

取込済みと判定したメッセージは ACK し、再配送を止める。

### なぜ記事と翻訳を 1 トランザクションで書くか

記事行だけが入って翻訳行が入らない中間状態を作らないため。中間状態を許すと、取込済みと判定した後に翻訳だけを補完する経路が要り、記事の有無と翻訳の有無の組み合わせごとに振る舞いを決めることになる。1 トランザクションなら記事行が入ったかどうかだけで取込の成否が決まり、失敗時は記事行ごと巻き戻って再配送でやり直せる。

### 既存行の UPDATE は行わない

newsfeed が同じ記事を再送してきた場合、校閲済みのテキストが上書きされると運用者の作業が消える。そのため UPDATE ではなく DO NOTHING を選択する。同じ `source_url` を別の `article_id` で取り直したときも、先に入った記事を正とする。

### ACK 戦略

push 受け口 (`/internal/v1/pubsub/news-article-collected`) は 2xx で ack、非 2xx で Pub/Sub 側が再配送する
(overload-party-infra 側の subscription 設定で `max_delivery_attempts = 5` 到達後は dead letter topic に送られる)。

- INSERT 成功 / 既存ヒット → 200
- 必須フィールド欠落（§FEATURE_SPEC 3.1）→ 200 + warn ログ（再送されても結果が変わらない deterministic error）
- push envelope 自体が不正、または `message.data` が base64 として復号できない → 400（dead letter 到達後に内容を確認できる）
- DB 接続失敗 → 500（Pub/Sub 側リトライ）

「必須フィールド欠落 → 200」は一見奇異だが、DB に書き込めない値を dead letter に送っても運用上得られる情報がないため、warn ログのみで ack する。

## キャッシュ戦略

公開 API (`:9008`) は短期キャッシュを噛ませる（in-memory LRU や Redis 等、実装選択は変更可能）。ただし以下は仕様上の契約:

- **キャッシュキーに `lang` を含める**: ja と en で独立してキャッシュする
- **TTL 上限**: 2 時間（newsfeed 収集周期と同じ）
- **無効化**: 校閲で `status` 遷移 / 翻訳 upsert が起きた瞬間に、影響する記事の全 lang 分のキャッシュを落とす

校閲 → 無効化のフックは usecase 層から「記事 ID + 変化した lang 群」を通知するコールバックで行う。キャッシュ実装側は notify を受けて該当キーを削除する。usecase はキャッシュ実装を知らない責務分離を維持する。
