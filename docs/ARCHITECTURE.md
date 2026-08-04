# News サービス設計

本ドキュメントは **コードを読んでも一見しては分からない設計意図** だけを残す。実装詳細（フロー順序・状態遷移・エラー → HTTP ステータス変換・環境変数一覧）は各ファイルの実装とコメントを一次情報とする。

サービス概要・起動手順は [../README.md](../README.md)、保証すべき振る舞いは [FEATURE_SPEC.md](FEATURE_SPEC.md)、エンドポイントは [../data/openapi.yaml](../data/openapi.yaml) (REST) と [../data/asyncapi.yaml](../data/asyncapi.yaml) (Pub/Sub)、テーブル定義は [DATA_DESIGN.md](DATA_DESIGN.md) を参照。

## News の責務境界 (SSoT と書き込み権限)

news は **記事コンテンツ**の single source of truth。`news.news_articles` への書き込みは news のみが行う。

| ライフサイクル | 書き手 | 契機 |
|---|---|---|
| 記事の初期挿入 (`status = pending`) | news | `news-article-collected` イベント購読 |
| 承認・却下・編集 | news | 校閲操作（管理 UI は提供していない。§ 管理 UI を提供していない理由） |
| 読み取り（公開配信） | news → gateway | 公開 REST API |

newsfeed（Cloud Run Job）は DB を触らない。収集結果を Pub/Sub 経由で news に引き渡すのみ。これにより:

- news スキーマの書き込み権限を news サービス 1 つに閉じられる（shop と同じ「1 スキーマ 1 所有者」原則）
- newsfeed 側の障害が news の DB 状態を壊さない
- newsfeed の再実行（同一記事の再取得）は Pub/Sub 経由でしか到達しないため、news 側の冪等性制御 1 箇所で吸収できる

## 公開しているポート

news が listen するのは gateway 向け配信 API の 1 ポートだけである。

| ポート | プロトコル | 想定クライアント | 認証 |
|---|---|---|---|
| `:9008` | HTTP JSON | gateway | 内部サービス間 JWT (RS256) |

## 管理 UI を提供していない理由

管理 UI (HTML + HTMX) のサーバは起動しない。Cloud Run は 1 リビジョンにつき 1 ポートしか公開せず、管理 UI の認証を肩代わりしていた IAP も公開ロードバランサの廃止で無くなったため、到達させるには構成と認証を作り直す必要がある。校閲は当面運用者の手作業とし、管理機能は将来別サービスとして用意する。

以降の管理 UI に関する記述は、その別サービスに引き継ぐ設計として残す。

### 管理 UI を配信 API と同じポートに置かない

- IAP は Ingress / LB 単位で設定するため、`/admin/*` だけに認証を強制することはできない
- ポートを分けることで、`/admin/*` が誤って配信 API 側に露出する構造的リスクを排除する
- 公開 API は gateway 経由のみ、管理 UI は認証済み経路のみ、という信頼境界をインフラ定義のレベルで固定できる

### IAP による認証の肩代わり

管理 UI は認証を news コード内に持たない。

```
ブラウザ → GCLB → IAP ─(認証成功時のみ)→ 管理 UI
                  │
                  └─ X-Goog-Authenticated-User-Email ヘッダを付与
```

news 側の `iapMiddleware` はヘッダ存在確認と context への email 注入のみ行う（校閲操作で `reviewer` 列に記録するため）。許可ユーザーリストの管理は IAP 側に閉じる:

- 運用者追加のたびに news のデプロイが必要になるのを避ける
- IAP の IAM 管理 UI で完結させる方が運用者にとって自然
- ヘッダ偽装の防御は「管理 UI のポートを認証済み経路以外に露出させない」というインフラ定義側の契約で担保する

### ENV=local での認証スキップ

ローカル開発では `iapMiddleware` がパススルーしヘッダ無しで通る。この分岐は `ENV` env var のみで制御し、code flag で制御しない（「production で誤ってスキップが有効になる」リスクを env 設定に集約）。

## インジェスト: Pub/Sub subscriber の冪等性

`news-article-collected` の購読は at-least-once 配送。重複配送を吸収するため以下の 3 段構成:

1. **DB レベル (親)**: `news_articles` に `INSERT ... ON CONFLICT DO NOTHING`
   - `article_id` は newsfeed が採番した ULID
   - 既存行があれば INSERT は no-op。**校閲済みの既存行を上書きしない**ことが重要
2. **DB レベル (翻訳)**: `news_article_translations` に `INSERT ... ON CONFLICT (article_id, lang) DO NOTHING`
   - 既存翻訳は維持し、newsfeed の再送で校閲済みテキストを壊さない
3. **補助キー**: `source_url` の UNIQUE 制約（`article_id` 再採番事故への二次防御）

`InsertArticle` と `InsertTranslation` は **独立した冪等操作**として実装し、トランザクションでまとめない。subscriber は両方を順に呼び、どちらかが失敗した時点で NACK → Pub/Sub が再配送する。再配送時は両方の DO NOTHING により、記事・翻訳のいずれかだけが先に入っていても残りを安全に補完できる。

### なぜ tx を張らないか

1. **翻訳テーブルはインジェスト以外にも書かれる**: 管理 UI からの `UpsertTranslation`（ja 編集 / en 追加）でも独立に更新される。ingest 経路だけが記事と翻訳をまとめて書く必要性は薄い
2. **中間状態が運用で扱える**: 「記事は入ったが翻訳はまだ」は管理 UI の `[ja 未作成]` プレースホルダで可視化される（§FEATURE_SPEC 7）。逆に tx で巻き戻すと、障害が operator から見えなくなる
3. **FEATURE_SPEC §3.1 の単純化**: MVP では ja 翻訳 1 件のみ。複数翻訳の原子挿入は仕様に無く、tx の利益が薄い

### 既存行の UPDATE は行わない

newsfeed が同じ記事を再送してきた場合、校閲済みのテキストが上書きされると運用者の作業が消える。そのため UPDATE ではなく DO NOTHING を選択する。親記事・翻訳の両方で同じ方針を取る。

### ACK 戦略

push 受け口 (`/internal/v1/pubsub/news-article-collected`) は 2xx で ack、非 2xx で Pub/Sub 側が再配送する
(overload-party-infra 側の subscription 設定で `max_delivery_attempts = 5` 到達後は dead letter topic に送られる)。

- INSERT 成功 / 既存ヒット → 200
- 必須フィールド欠落（§FEATURE_SPEC 3.1）→ 200 + warn ログ（再送されても結果が変わらない deterministic error）
- push envelope 自体が不正、または `message.data` が base64 として復号できない → 400（dead letter 到達後に内容を確認できる）
- DB 接続失敗 → 500（Pub/Sub 側リトライ）

「必須フィールド欠落 → 200」は一見奇異だが、DB に書き込めない値を dead letter に送っても運用上得られる情報がないため、warn ログのみで ack する。

## HTMX レンダリング層の構造

管理 UI は `html/template` + `embed.FS` でバイナリに埋め込まれた HTML を動的生成する。FE 成果物は独立に存在しない。

レイヤー配置は他の delivery 経路（REST / Pub/Sub subscriber）と同じく Clean Architecture に従う:

- **handler 層**: 管理 UI の HTTP ルーティング・IAP middleware・テンプレート描画を担当する。REST handler とは別ルータを構築する（2 ポート構成）
- **usecase 層**: 校閲ユースケース（承認・却下・編集）は REST handler も管理 UI handler も同じ usecase を呼ぶ。UI 固有のロジックを usecase に持たせない
- **repository 層**: admin UI は同じ repo を読み書きするだけで、admin 固有の SQL を持たない
- **テンプレート / static**: handler 層に同居し `embed.FS` でバイナリに同梱する

### 部分レスポンスの契約

HTMX は `hx-target` / `hx-swap` で DOM の一部を差し替える。承認・却下は **ページ全体ではなく `<tr>` フラグメントだけを返す**。フルページを返すハンドラと部分を返すハンドラを混在させると HTMX 側の `hx-target` 指定ミスが発生しやすいため、以下を契約とする:

| ハンドラ種別 | 返すもの |
|---|---|
| `GET /admin/articles` | フルページ HTML（常に ja タイトルで表示） |
| `GET /admin/articles/:articleId` | フルページ HTML（ja / en タブ付き） |
| `POST /admin/articles/:articleId/publish` | 差し替え用の `<tr>` フラグメント（HX-Target 指定時）または HX-Redirect |
| `POST /admin/articles/:articleId/reject` | 差し替え用の `<tr>` フラグメント（HX-Target 指定時）または HX-Redirect |
| `POST /admin/articles/:articleId/translations/:lang` (翻訳 upsert) | `200 OK` + `HX-Redirect: /admin/articles/:articleId`（編集画面に戻る） |

### XSS 対策

`html/template` はコンテキスト対応のエスケープを行う。記事本文・タイトルは外部取得テキストなので必ずテンプレート経由で描画し、文字列連結で HTML を組み立てない（厳守）。

## キャッシュ戦略

公開 API (`:9008`) は短期キャッシュを噛ませる（in-memory LRU や Redis 等、実装選択は変更可能）。ただし以下は仕様上の契約:

- **キャッシュキーに `lang` を含める**: ja と en で独立してキャッシュする
- **TTL 上限**: 2 時間（newsfeed 収集周期と同じ）
- **無効化**: 校閲で `status` 遷移 / 翻訳 upsert が起きた瞬間に、影響する記事の全 lang 分のキャッシュを落とす
- **管理 UI はキャッシュしない**: 校閲結果が即時に編集画面 / 一覧に反映される必要があるため

校閲 → 無効化のフックは usecase 層から「記事 ID + 変化した lang 群」を通知するコールバックで行う。キャッシュ実装側は notify を受けて該当キーを削除する。usecase はキャッシュ実装を知らない責務分離を維持する。
