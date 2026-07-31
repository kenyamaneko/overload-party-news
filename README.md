# overload-party-news

クラウドニュース記事の校閲・配信を行う内部マイクロサービス。`news-article-collected` Pub/Sub イベントを購読して自スキーマに永続化し、運用者が管理 UI で校閲した後に gateway 経由でクライアントへ配信する。gateway 向け REST は ClusterIP ポート 9008、管理 UI は IAP 背後のポート 9108 で起動する。

詳細は [機能仕様書](docs/FEATURE_SPEC.md) / [サービス設計書](docs/ARCHITECTURE.md) / [REST 契約](data/openapi.yaml) / [Pub/Sub 契約](data/asyncapi.yaml) / [データ設計書](docs/DATA_DESIGN.md) を参照。

[テスト観点カタログ](https://kenyamaneko.github.io/overload-party-news/): テスト名から生成した、テスト済みの観点の一覧。

## アーキテクチャ概要

```
Gateway
  └─ News (:9008 internal REST)
       ├─ PostgreSQL (news スキーマ)
       └─ Pub/Sub push (HTTP)
            └─ news-article-collected ← newsfeed (Cloud Run Job)

運用者 (ブラウザ)
  └─ IAP (Google OAuth)
       └─ News (:9108 admin UI)  ← 同一 Pod 内の別ポート
```

書き込みは news 自身のみ。gateway からは配信のみ、newsfeed は Pub/Sub publish のみで DB には触れない。

## ローカル開発

`make run` はアプリ本体とインフラ (Postgres) を compose 内で起動する。
記事インジェストは Pub/Sub push subscription の HTTP 受け口なので、ローカルでは以下のように直接 POST して動作確認する。

```bash
curl -X POST http://localhost:9008/internal/v1/pubsub/news-article-collected \
  -H 'Content-Type: application/json' \
  -d '{"message":{"data":"<ArticleCollectedEvent を JSON化して base64 化した文字列>"}}'
```
インフラはホストへ publish せず内部ネットワークのサービス名 DNS で参照するため、他リポのローカル
スタックやホスト上の他アプリとポートが衝突しない。ホストへ出るのは news の API ポート (REST 9008 / admin 9108) のみ。

```bash
make run      # アプリ + インフラを compose で起動（ソース bind-mount）
make down     # 停止して volume を削除
make test     # Testcontainers でテスト実行（Docker 必須）
```

アプリはコンテナ内で `go run` する。ソースを編集して `docker compose restart news` すれば、
イメージを作り直さずに反映される。private module は host の module cache を読み取り専用でマウント
して解決するため、`make run` は先に host 側で `go mod download` を実行する。

ローカル起動時は IAP middleware がスキップされ、`http://localhost:9108/admin/` に直接アクセスできる（`ENV=local` 時のみ）。

## 公開パッケージ

[packages/api-news/](packages/api-news/) に REST / Pub/Sub 契約型を公開している。
SSoT は [data/openapi.yaml](data/openapi.yaml) (REST) と [data/asyncapi.yaml](data/asyncapi.yaml) (Pub/Sub)。
spec を編集後に以下で `oapi-codegen` + `asyncapi-codegen` を呼び出して再生成する。

```bash
scripts/generate_types.sh
```
