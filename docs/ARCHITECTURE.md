# News サービス設計

本ドキュメントは **コードを読んでも一見しては分からない設計意図** だけを残す。実装詳細（フロー順序・状態遷移・エラー → HTTP ステータス変換・環境変数一覧）は各ファイルの実装とコメントを一次情報とする。

サービス概要・起動手順は [../README.md](../README.md)、保証すべき振る舞いは [FEATURE_SPEC.md](FEATURE_SPEC.md)、エンドポイントは [API_REFERENCE.md](API_REFERENCE.md)、テーブル定義は [DATA_DESIGN.md](DATA_DESIGN.md) を参照。

## News の責務境界 (SSoT と書き込み権限)

news は **記事コンテンツ**の single source of truth。`news.news_articles` への書き込みは news のみが行う。

| ライフサイクル | 書き手 | 契機 |
|---|---|---|
| 記事の初期挿入 (`status = pending`) | news | `news-article-collected` イベント購読 |
| 承認・却下・編集 | news | 管理 UI (§ 管理 UI) からの操作 |
| 読み取り（公開配信） | news → gateway | 公開 REST API |

newsfeed（Cloud Run Job）は DB を触らない。収集結果を Pub/Sub 経由で news に引き渡すのみ。これにより:

- news スキーマの書き込み権限を news サービス 1 つに閉じられる（shop と同じ「1 スキーマ 1 所有者」原則）
- newsfeed 側の障害が news の DB 状態を壊さない
- newsfeed の再実行（同一記事の再取得）は Pub/Sub 経由でしか到達しないため、news 側の冪等性制御 1 箇所で吸収できる

## API と管理画面の分離

news は同一バイナリ・同一 Pod で 2 つのポートを listen し、gateway 向け配信 API と運用者向け管理 UI を信頼境界で分ける。

| ポート | プロトコル | 想定クライアント | 認証 | Kubernetes Service |
|---|---|---|---|---|
| `:9008` | HTTP JSON | gateway | ClusterIP（gateway 経由） | ClusterIP |
| `:9108` | HTTP HTML + HTMX | 運用者 | IAP が `X-Goog-Authenticated-User-Email` を付与 | 外部 Ingress + IAP |

### 1 ポート + path 振り分けにしない理由

- IAP は Ingress / LB 単位で設定するため、`/admin/*` だけに認証を強制することはできない
- ポートごとに Service / Ingress を分けることで、`/admin/*` が誤って ClusterIP 側に露出する構造的リスクを排除する
- 公開 API は gateway 経由のみ、管理 UI は IAP 経由のみ、という信頼境界を manifest レベルで固定できる

### IAP による認証の肩代わり

管理 UI は認証を news コード内に持たない。

```
ブラウザ → GCLB → IAP ─(認証成功時のみ)→ GKE Ingress → news :9108
                  │
                  └─ X-Goog-Authenticated-User-Email ヘッダを付与
```

news 側の `iapMiddleware` はヘッダ存在確認と context への email 注入のみ行う（校閲操作で `reviewer` 列に記録するため）。許可ユーザーリストの管理は IAP 側に閉じる:

- 運用者追加のたびに news のデプロイが必要になるのを避ける
- IAP の IAM 管理 UI で完結させる方が運用者にとって自然
- ヘッダ偽装の防御は「ポート `:9108` を ClusterIP に露出させない」という manifest 側の契約で担保する（k8s manifest 変更時はこの前提を検証する）

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

- INSERT 成功 / 既存ヒット → ACK
- 必須フィールド欠落（§FEATURE_SPEC 3.1）→ ACK + warn ログ（再送されても結果が変わらない deterministic error）
- DB 接続失敗 → NACK（Pub/Sub 側リトライ）

「payload 不正 → ACK」は一見奇異だが、NACK して無限リトライさせるより dead-letter に送った方が運用負担が低い。Pub/Sub 側で DLQ 設定を入れる前提。

## Presenter 層の位置づけ

`internal/presenter/` は domain ↔ wire DTO (`packages/api-news`) の境界変換を集約するパッケージ。usecase / handler / repository から変換ロジックを物理的に分離し、wire 表現の変更が業務層に波及しないようにする。

**現状は厳密な Presenter パターンではない。** Uncle Bob クリーンアーキテクチャ原典の Presenter は output port (interface) を介して usecase が結果を「押し出す」構造を取り、usecase 層は wire DTO 型を一切 import しない。本サービスでは usecase が presenter 関数を直接呼び、戻り値で wire DTO を返すため、依存方向としては usecase → wire DTO 型への参照が残っている。実態は Mapper パターンに近い (overload-party-card / scenario と同方針)。

この折衷を選んだ理由:

- Go 慣用は「戻り値で返す」スタイルを好み、output port の副作用ベース設計とは噛み合わせが悪い
- 公開 API は REST のみ、ingest は単一 Pub/Sub topic のみで複数 wire (gRPC / GraphQL) の差し替え要件が現状ない
- 厳密な Presenter は endpoint ごとに output port interface と presenter struct が必要になり、サービス × N endpoint の規模では割に合わない

**命名規則:**

- `ToXxx`: 単純射影 (引数の値をそのまま wire 形に詰め替える)。例: `ToNewsListItem`、`ToNewsDetail`
- `BuildXxx`: 派生計算 (複数引数からの算出) を伴う組み立て (現状未使用)
- `XxxFromCollectedEvent` 等: wire → domain 変換。例: `ArticleFromCollectedEvent` (ingest event のフィールドを `domain.Article` に詰め替える)

**HTML view model の取り扱い:**

`handler/admin/handler.go` 内の `adminListItem` 等は HTMX テンプレート描画用の handler 内ビューモデルで、JSON wire DTO ではない。JSON 契約と公開 wire 表現を集約する presenter とは責務が異なるため、presenter には集約せず admin handler の内側に閉じる。

**ingest event の validation:**

`apinews.ArticleCollectedEvent` の必須欠け・言語制約 (`translations` が `ja` 1 件ちょうど) チェックは `usecase/ingest` 内に残す。presenter (`ArticleFromCollectedEvent`) は妥当性検証済みの event を `domain.Article` に詰め替えるだけの純粋射影に保つ。validation を presenter に移すと「presenter から sentinel error を返す」配線が増えて純度を汚すため、敢えて分けている。

**将来の移行パス。** 複数 wire 形式の差し替えが必要になった時点で、以下の順で段階的に厳密 Presenter へ昇格できる:

1. presenter 関数のシグネチャを output port interface (`type XxxOutput interface { Present(...) }`) に置き換える
2. usecase struct に output port を依存注入し、戻り値返却を `s.output.Present(...)` 呼び出しに差し替える
3. handler 側で wire 形式ごとに presenter struct を実装 (`JSONPresenter` / `GRPCPresenter`)、endpoint 構築時に注入

現状の package 配置 (`internal/presenter/`) と命名はこの移行を阻害しない。usecase の wire DTO への依存を切り離す改修だけで Presenter パターンに到達できる。

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
