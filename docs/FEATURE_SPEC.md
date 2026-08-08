# News 機能仕様書

このドキュメントは news サービスがビジネス要件として満たすべき振る舞いを定義する。実装方法ではなく **何を保証するか** を記述する。テストはこの仕様に従っていることを確認する観点で書く。

関連ドキュメント:
- 内部動作・配線・本番運用設定: [ARCHITECTURE.md](ARCHITECTURE.md)
- HTTP エンドポイント契約: [../data/openapi.yaml](../data/openapi.yaml)
- Pub/Sub イベント契約: [../data/asyncapi.yaml](../data/asyncapi.yaml)
- DB スキーマ: [DATA_DESIGN.md](DATA_DESIGN.md)

---

## 1. サービス責務

news は以下の機能ドメインを所有する。

| 機能 | 主要な責務 |
|---|---|
| 記事インジェスト | `news-article-collected` イベントを購読し自スキーマに永続化 |
| 校閲オペレーション | 記事レベルの承認・却下、および言語別翻訳の編集 / 追加 |
| ニュース一覧取得 | 指定言語で公開中の記事メタ情報一覧を返す（gateway 経由のクライアント向け） |
| ニュース詳細取得 | 指定言語で公開中の単一記事の本文を含む全フィールドを返す |

news は **`news` スキーマの DB 行を唯一の真実とし**、書き込み権限を自身のみが持つ。newsfeed（Cloud Run Job）は RSS 取得・要約生成のみを担い、DB への直接書き込みは行わず Pub/Sub 経由で news に受け渡す。運営のお知らせ配信は support サービスの責務であり news のスコープ外。

---

## 2. 記事データモデル

記事は「言語非依存の共通部分」と「言語別の翻訳部分」に正規化する。SSoT は `news.news_articles` と `news.news_article_translations`（[DATA_DESIGN.md](DATA_DESIGN.md) 参照）。

### 2.1 言語非依存フィールド (`news_articles`)

| フィールド | 型 | 公開 API 一覧 | 公開 API 詳細 | 備考 |
|---|---|---|---|---|
| `article_id` | string (ULID) | ✓ | ✓ | PK |
| `source` | string | ✓ | ✓ | ソース種別。許容値の SSoT は newsfeed の `cloud_news_sources` 定数（`aws` / `google-cloud` / `azure` / `oci` / `other`） |
| `tags` | string[] | ✓ | ✓ | タグ配列（言語横断で共通） |
| `source_url` | string | — | ✓ | 元記事 URL |
| `source_published_at` | timestamptz? | ✓ | ✓ | 元記事（RSS 由来）の公開日時（取得不能な場合は null） |
| `published_at` | timestamptz? | ✓ | ✓ | アプリ内で公開された日時（承認時にセット）。未承認時は null |
| `status` | string | — | — | レビュー状態（§2.4）。記事レベル（言語ごとに持たない） |
| `ingested_at` | timestamptz | — | — | news が INSERT した日時 |
| `reviewed_at` | timestamptz? | — | — | 直近の承認または却下の日時（翻訳編集は含まない） |
| `reviewer` | string? | — | — | 直近の承認・却下を行った運用者（Google メールアドレス） |
| `updated_at` | timestamptz | — | — | 記事行の最終更新日時（trigger で自動更新） |

### 2.2 言語別フィールド (`news_article_translations`)

| フィールド | 型 | 公開 API 一覧 | 公開 API 詳細 | 備考 |
|---|---|---|---|---|
| `article_id` | string (ULID) | — | — | FK → `news_articles.article_id` |
| `lang` | string | — | — | 言語コード（§2.3） |
| `title` | string | ✓ | ✓ | 記事タイトル（編集可） |
| `summary` | string | ✓ | ✓ | 要約（編集可） |
| `body` | string | — | ✓ | 本文（編集可） |
| `created_at` | timestamptz | — | — | 翻訳行の作成日時 |
| `updated_at` | timestamptz | — | — | 翻訳行の最終更新日時（trigger で自動更新） |

PK: `(article_id, lang)`。同一記事の同一言語は DB レベルで 1 行に制限される。

### 2.3 対応言語

MVP 対応言語は `ja` と `en` の 2 種。許容値は news サービス内の定数で管理し、未知の言語コードをリクエストで受けた場合はエラーとする（§4 / §5）。

- `ja`: newsfeed Cloud Run Job が Vertex AI で生成した日本語要約由来。インジェスト時に常に挿入される
- `en`: 当面 newsfeed は生成しない。運用者が DB を直接更新して追加する

### 2.4 公開対象の条件

公開 API（§4 / §5）は以下を **すべて** 満たす記事のみをレスポンスに含める（以後「公開可能」と呼ぶ）:

1. `news_articles.status = 'published'`
2. 指定された `lang` に対応する `news_article_translations` 行が存在する

条件 1 のみを満たすが条件 2 を満たさない記事（例: `status=published` かつ ja のみ翻訳済み、`?lang=en` で問い合わせ）は、一覧では除外・詳細では 404。**フォールバックは行わない**（ja に代替表示しない、の大方針）。

`status` 値・`lang` 値そのものは公開 API のレスポンスに含めない。クライアントは「返ってきた記事 = 指定 lang で公開中」とだけ理解すればよい。

### 2.5 status の状態遷移

status は記事レベル（言語横断）であり、翻訳行ごとには持たない。

| status | 意味 | 遷移元 |
|---|---|---|
| `pending` | インジェスト直後の初期状態。公開されない | §3 インジェスト時にセット |
| `published` | 公開中。公開 API が返す唯一の状態（ただし §2.4 の翻訳存在も必要） | 運用者による承認（DB 直接更新） |
| `rejected` | 公開不可と判断され恒久的に非公開 | 運用者による却下（DB 直接更新） |

```
Pub/Sub event (ja translation)
  └─ 記事 (status=pending) + ja 翻訳  ─── 1 トランザクションで INSERT
                                          (取込済みなら何も挿入せず ACK)
        │
        ▼ 運用者による DB 直接更新
        ├─ 翻訳編集 (ja / en) → 該当行のみ UPDATE、status 変更なし
        ├─ en 翻訳の新規追加 → INSERT、status 変更なし
        ├─ 承認 → status = published  ── 存在する翻訳の言語で公開 API に露出
        └─ 却下 → status = rejected   ── 恒久非公開
```

遷移に方向制約は設けない。`published` → `rejected`（誤承認の取り下げ）や `rejected` → `pending`（却下の取り消し）を許容する。

**承認後の翻訳追加も再承認不要**: 公開済み記事に en 翻訳を後から追加した場合、en ユーザーには次のキャッシュ無効化後に即時可視となる。

### 2.6 未知の status 値

news サービスは未知の `status` 値を **非公開として扱う**。将来 `archived` などを追加しても、明示的に公開対象に加えるまでクライアントに露出しない。

---

## 3. 記事インジェスト

news は `news-article-collected` トピックを購読し、受信メッセージごとに `news_articles` 1 行 + `news_article_translations` の **ja 翻訳 1 行** を 1 トランザクションで INSERT する。

記事行が既にあるイベントは取込済みとして何も挿入せず ACK する。記事だけ入って翻訳が無い中間状態は生じない。

### 3.1 購読イベントのペイロード

| フィールド | 型 | Nullable | 備考 |
|---|---|---|---|
| `article_id` | string (ULID) | No | newsfeed が採番、news の PK として使用 |
| `source` | string | No | |
| `source_url` | string | No | |
| `tags` | string[] | No | |
| `source_published_at` | timestamptz | Yes | 元記事（RSS 由来）の公開日時 |
| `translations` | `[]EventTranslation` | No | **MVP では ja 翻訳 1 件ちょうど**（en 等を含むイベントは拒否） |

`EventTranslation`:

| フィールド | 型 | Nullable | 備考 |
|---|---|---|---|
| `lang` | string | No | MVP では `ja` のみ許容（§2.3） |
| `title` | string | No | |
| `summary` | string | No | newsfeed の Vertex AI 要約結果 |
| `body` | string | No | 記事本文 |

**必須フィールドが欠けた / translations が ja 1 件ちょうどでない**イベントは `ErrInvalidEventPayload` で ACK（再送されても結果が変わらないため）。ログには warn で残す。

en 翻訳は運用者が DB を直接更新して手動追加する。これは「AI 生成の初稿を運用者が手直しする前提で、ingest 段階で ja / en の両方を作ると修正工数が倍になる」という運用要件に基づく。

### 3.2 冪等性

同一イベントが複数回配送されても、同じ `source_url` が別の `article_id` で届いても DB 行は重複しない:

1. **article_id**: PK による UNIQUE
2. **source_url**: UNIQUE（newsfeed が取り直して採番し直した記事を弾く）

`news_articles` への INSERT は `ON CONFLICT DO NOTHING` で、上記どちらの衝突も no-op になる。1 行も入らなかったイベントは取込済みとして翻訳を挿入せず ACK する。

**校閲後の翻訳を newsfeed の再送で上書きしない** 契約を維持する。トランザクションの途中で失敗した場合は記事行ごと巻き戻し、NACK → Pub/Sub が再配送する。

### 3.3 インジェスト時の初期値

- `news_articles.status` = `pending`
- `news_articles.ingested_at` = `now()`
- `news_articles.reviewed_at`, `reviewer` = `null`
- `news_article_translations.created_at`, `updated_at` = `now()`

---

## 4. ニュース一覧取得 (`List`)

**入力**: `lang` (必須), `limit`
**出力**: `[]NewsListItem`（§2.1 + §2.2 の「公開 API 一覧」列に ✓ のフィールド）

### 仕様

1. 公開可能な記事（§2.4）のみを対象とする（`status=published` かつ指定 `lang` の翻訳が存在）
2. `published_at DESC NULLS LAST, article_id DESC` の順に並べ、先頭から `limit` 件を返す
   - `published_at` はアプリ内公開日時（承認時刻）。`source_published_at` ではない
   - `published_at` が null の記事は原則存在しないが、安全のため NULLS LAST
   - 同一 `published_at` は `article_id` 降順でタイブレーク
3. `lang` の制約:
   - 省略時は `ErrLangRequired`（400）
   - 対応外の値は `ErrUnsupportedLang`（400）
4. `limit` の制約:
   - 省略時のデフォルト: `20`
   - 許容範囲: `1 <= limit <= 100`
   - 範囲外・非整数は `ErrInvalidLimit`（400）
5. ページング（offset / cursor）は **提供しない**
6. 本文 (`body`) および `source_url` は返却しない

副作用なし。

---

## 5. ニュース詳細取得 (`GetDetail`)

**入力**: `articleID`, `lang` (必須)
**出力**: `NewsDetail`（§2.1 + §2.2 の「公開 API 詳細」列に ✓ のフィールド）

### 仕様

1. `articleID` で `news_articles` を検索し、`(articleID, lang)` で `news_article_translations` を JOIN
2. 以下のいずれかなら `ErrNotFound`（404）:
   - `news_articles` 行が存在しない
   - `status != 'published'`
   - 指定 `lang` の翻訳行が存在しない
3. 一致した場合、`body` と `source_url` を含む全公開フィールドを返す
4. `lang` バリデーションは §4-3 と同じ

`articleID` の形式バリデーション（ULID 26 文字など）は行わない。形式不一致は結果として行ヒットせず 404。

副作用なし。

---

## 6. エラーセマンティクス

サービス層は HTTP ステータスを知らない。エラーはセンチネルとして返し、handler が `errors.Is` ベースの分類関数で transport 層のステータスに変換する。

### 6.1 分類

| 分類関数 | 対象エラー | 用途 |
|---|---|---|
| `IsNotFound` | `ErrNotFound` | 404 |
| `IsValidation` | `ErrInvalidLimit`, `ErrInvalidField`, `ErrLangRequired`, `ErrUnsupportedLang` | 400 |
| `IsDeterministic` | `ErrInvalidEventPayload` | Pub/Sub subscriber で ACK（再送無意味） |

### 6.2 握りつぶし禁止

DB エラー・Pub/Sub ack 失敗をログのみで握りつぶしてはならない。すべて呼び出し元に返す（CLAUDE.md「設計思想」参照）。「記事が 0 件」と「DB エラー」を同じ空配列レスポンスに畳み込まない。

---

## 7. 非機能要件

### 7.1 キャッシュ

公開 API は短期キャッシュ対象。キャッシュキーには `lang` を含める（言語ごとに独立）。

- キャッシュ由来であっても §2.4 の公開可能条件を満たさない記事を返してはならない
- 校閲により `status` が変化した記事は TTL 経過後に消える
- 翻訳追加（en の新規 upsert）も同様に TTL 経過後に反映される

### 7.2 他サービスとの独立性

- news の障害は他サービスに波及しない
- newsfeed Cloud Run Job が停止しても news は既存記事を配信し続ける
- 記事インジェストの push 受け口がエラーを返しても、他のエンドポイント (公開 API) は影響を受けない
