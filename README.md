# overload-party-news

クラウドニュース記事の校閲・配信を行う内部マイクロサービス。`news-article-collected` Pub/Sub イベントを購読して自スキーマに永続化し、運用者が管理 UI で校閲した後に gateway 経由でクライアントへ配信する。gateway 向け REST は ClusterIP ポート 9008、管理 UI は IAP 背後のポート 9108 で起動する。

詳細は [機能仕様書](docs/FEATURE_SPEC.md) / [サービス設計書](docs/ARCHITECTURE.md) / [API仕様書](docs/API_REFERENCE.md) / [データ設計書](docs/DATA_DESIGN.md) を参照。

## アーキテクチャ概要

```
Gateway
  └─ News (:9008 internal REST)
       ├─ PostgreSQL (news スキーマ)
       └─ Pub/Sub subscribe
            └─ news-article-collected ← newsfeed (Cloud Run Job)

運用者 (ブラウザ)
  └─ IAP (Google OAuth)
       └─ News (:9108 admin UI)  ← 同一 Pod 内の別ポート
```

書き込みは news 自身のみ。gateway からは配信のみ、newsfeed は Pub/Sub publish のみで DB には触れない。

## ローカル開発

```bash
make db-up    # postgres:16-alpine を起動
make run      # サーバー起動（db-up と環境変数の注を含む）
make test     # Testcontainers でテスト実行（Docker 必須）
make db-down  # 停止
make db-reset # volume ごと削除して再作成
```

ローカル起動時は IAP middleware がスキップされ、`http://localhost:9108/admin/` に直接アクセスできる（`ENV=local` 時のみ）。

## 公開パッケージ

[packages/api-news/](packages/api-news/) に REST 契約型を公開している。[data/models.yaml](data/models.yaml) を編集後に以下で再生成する。

```bash
python3 scripts/generate_types.py
```
