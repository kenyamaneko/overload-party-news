CREATE SCHEMA IF NOT EXISTS news;

-- 記事の共通 (言語非依存) 部分と校閲状態。
-- title / summary / body は言語別に news_article_translations で保持する。
CREATE TABLE IF NOT EXISTS news.news_articles (
    article_id          VARCHAR(26) PRIMARY KEY,                 -- ULID (newsfeed 採番)
    source              VARCHAR(20) NOT NULL,                     -- aws / google-cloud / azure / oci / other
    source_url          TEXT        NOT NULL UNIQUE,              -- 元記事 URL (UNIQUE で重複取込防止)
    tags                TEXT[]      NOT NULL DEFAULT '{}',        -- タグ配列 (言語横断で共通)
    status              VARCHAR(20) NOT NULL,                     -- pending / published / rejected
    source_published_at TIMESTAMPTZ,                              -- 元記事 (RSS 由来) の公開日時
    published_at        TIMESTAMPTZ,                              -- アプリ内公開日時 (承認時セット・更新)
    ingested_at         TIMESTAMPTZ NOT NULL DEFAULT now(),       -- news が INSERT した日時
    reviewed_at         TIMESTAMPTZ,                              -- 直近の承認・却下日時 (翻訳編集は含めない)
    reviewer            VARCHAR(255),                             -- 直近の承認・却下を行った運用者の email
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()        -- 記事行の最終更新日時 (trigger で自動更新)
);

-- 記事の言語別コンテンツ。
-- 親記事 1 行に対し、対応言語ごとに 1 行を持つ (MVP は ja / en)。
CREATE TABLE IF NOT EXISTS news.news_article_translations (
    article_id VARCHAR(26)  NOT NULL REFERENCES news.news_articles(article_id) ON DELETE CASCADE,
    lang       VARCHAR(10)  NOT NULL,                   -- ja / en 等 (許容値は news 側の定数)
    title      TEXT         NOT NULL,
    summary    TEXT         NOT NULL,
    body       TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),     -- trigger で自動更新
    PRIMARY KEY (article_id, lang)
);

-- 公開 API の一覧クエリ用: status='published' の記事のみを published_at DESC でソート
CREATE INDEX IF NOT EXISTS idx_news_articles_published
    ON news.news_articles (published_at DESC NULLS LAST, article_id DESC)
    WHERE status = 'published';

-- 管理 UI の一覧クエリ用: status フィルタ + 取り込み順
CREATE INDEX IF NOT EXISTS idx_news_articles_status_ingested
    ON news.news_articles (status, ingested_at DESC);

-- 任意の UPDATE で updated_at を自動更新する trigger。
-- reviewed_at / reviewer が校閲判断 (承認・却下) 限定であるのに対し、
-- updated_at は "最後に触れた時刻" を素直に捉える。
CREATE OR REPLACE FUNCTION news.touch_updated_at()
    RETURNS TRIGGER
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_news_articles_touch_updated_at ON news.news_articles;
CREATE TRIGGER trg_news_articles_touch_updated_at
    BEFORE UPDATE ON news.news_articles
    FOR EACH ROW
    EXECUTE FUNCTION news.touch_updated_at();

DROP TRIGGER IF EXISTS trg_news_article_translations_touch_updated_at ON news.news_article_translations;
CREATE TRIGGER trg_news_article_translations_touch_updated_at
    BEFORE UPDATE ON news.news_article_translations
    FOR EACH ROW
    EXECUTE FUNCTION news.touch_updated_at();
