# news スキーマ - データ設計

> **DDL の SSoT:** `db/schema.sql`

## 設計概要

news スキーマはクラウドニュース記事の校閲状態と配信用コンテンツを格納する。言語非依存の共通部分と言語別の翻訳部分を 2 テーブルに正規化する。newsfeed Cloud Run Job が RSS 取得・要約生成した結果を `news-article-collected` Pub/Sub イベントで受信し、news サービスが 2 テーブルを同一 tx で永続化する。初期挿入 (取り込み) は news サービスのみが行う。承認・却下・翻訳編集は運用者が DB を直接更新する。

---

## テーブル構成

### news_articles

記事の共通（言語非依存）部分と校閲状態。

- **PK:** `article_id` (VARCHAR(26), ULID)
- **UNIQUE:** `source_url`
- **TRIGGER:** `updated_at` 自動更新

<!-- BEGIN GENERATED: news_articles -->
| カラム名 | 型 | Nullable | 説明 |
|---|---|---|---|
| `article_id` | VARCHAR(26) | No | ULID（newsfeed が採番し Pub/Sub 経由で受領） |
| `source` | VARCHAR(20) | No | ソース種別。SSoT は [newsfeed/data/newsfeed_constants.yaml](../../overload-party-newsfeed/data/newsfeed_constants.yaml)（`aws` / `google-cloud` / `azure` / `oci` / `other`） |
| `source_url` | TEXT | No | 元記事 URL（UNIQUE で重複取り込みを防止） |
| `tags` | TEXT[] | No | タグ配列（言語横断で共通、デフォルト `{}`） |
| `status` | VARCHAR(20) | No | `pending` / `published` / `rejected`（未知の値は非公開扱い） |
| `source_published_at` | TIMESTAMPTZ | Yes | 元記事（RSS 由来）の公開日時 |
| `published_at` | TIMESTAMPTZ | Yes | アプリ内で公開された日時（承認時セット・更新） |
| `ingested_at` | TIMESTAMPTZ | No | news が INSERT した日時 |
| `reviewed_at` | TIMESTAMPTZ | Yes | 直近の承認または却下の日時（翻訳編集は含まない） |
| `reviewer` | VARCHAR(255) | Yes | 直近の承認・却下を行った運用者のメールアドレス |
| `updated_at` | TIMESTAMPTZ | No | 記事行の最終更新日時（trigger で自動更新） |
<!-- END GENERATED: news_articles -->

### news_article_translations

記事の言語別コンテンツ。

- **PK:** `(article_id, lang)`
- **FK:** `article_id → news_articles.article_id ON DELETE CASCADE`
- **TRIGGER:** `updated_at` 自動更新

<!-- BEGIN GENERATED: news_article_translations -->
| カラム名 | 型 | Nullable | 説明 |
|---|---|---|---|
| `article_id` | VARCHAR(26) | No | 親記事 ID |
| `lang` | VARCHAR(10) | No | 言語コード（`ja` / `en` 等。SSoT は news 内の定数） |
| `title` | TEXT | No | タイトル |
| `summary` | TEXT | No | 要約 |
| `body` | TEXT | No | 本文 |
| `created_at` | TIMESTAMPTZ | No | 翻訳行の作成日時 |
| `updated_at` | TIMESTAMPTZ | No | 翻訳行の最終更新日時（trigger で自動更新） |
<!-- END GENERATED: news_article_translations -->

**設計判断:**

- 言語依存フィールド (`title` / `summary` / `body`) を別テーブルに正規化する。MVP は `ja` のみ newsfeed が生成し、`en` は運用者が DB を直接更新して追加する。新言語追加時に `news_articles` の DDL を変更せず済む
- ON DELETE CASCADE で親記事削除時に翻訳が孤児として残らないことを DB レベルで保証
- 翻訳編集は `news_articles.updated_at` を動かさない。これにより「記事レベルの最終更新 = 承認・却下」という監査性を保つ
- `lang` の列挙は ENUM ではなく VARCHAR を採用。news 側のコードで許容値を強制し、未知値はリクエストをエラー化（`ErrUnsupportedLang`）する
- `status` も同様に VARCHAR。状態追加時の DDL 変更を避け、未知値を非公開扱いするフェイルセーフと組み合わせて柔軟性を確保
- `article_id` は newsfeed 側で採番し PK として引き継ぐ。news 側で再採番すると `ON CONFLICT DO NOTHING` による重複検知が効かない
- `article_id` / `source` の列幅は取り込み時に news 側のコードでも検査する。超過値は INSERT 前に不正イベントとして捨て、Pub/Sub の再配信が止まらなくなるのを防ぐ。コード側の上限値 (`domain.MaxArticleIDLength` / `domain.MaxSourceLength`) と DDL の drift は列幅テストが検出する
- `reviewed_at` / `reviewer` は承認・却下のみ記録し、翻訳編集時刻は各翻訳行の `updated_at` で拾う
- 編集者の履歴は記録しない（運用者少数前提）

---

## テーブル間リレーション

```
news_articles (PK: article_id)
  │
  └── 1:N ── news_article_translations (PK: article_id, lang)
                FK: article_id → news_articles.article_id ON DELETE CASCADE

他テーブル・他スキーマへの参照なし。
```

---

## インデックス戦略

```sql
-- 公開 API の一覧: status='published' + published_at DESC
CREATE INDEX idx_news_articles_published
    ON news.news_articles (published_at DESC NULLS LAST, article_id DESC)
    WHERE status = 'published';

CREATE INDEX idx_news_articles_ingested
    ON news.news_articles (ingested_at DESC);
```

- 前者は部分インデックスで、公開対象の記事のみを索引に載せる
- 翻訳テーブルは PK (`article_id`, `lang`) が JOIN と PK lookup の両方をカバーする
- PK (`article_id`) と UNIQUE (`source_url`) は自動索引される
