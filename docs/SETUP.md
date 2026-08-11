# セットアップ

## ローカル開発

`make run` はアプリ本体とインフラ (Postgres) を compose 内で起動する。
記事インジェストは Pub/Sub push subscription の HTTP 受け口なので、ローカルでは以下のように直接 POST して動作確認する。

```bash
curl -X POST http://localhost:9008/internal/v1/pubsub/news-article-collected \
  -H 'Content-Type: application/json' \
  -d '{"message":{"data":"<ArticleCollectedEvent を JSON化して base64 化した文字列>"}}'
```
インフラはホストへ公開せず内部ネットワークのサービス名 DNS で参照するため、他リポのローカル
スタックやホスト上の他アプリとポートが衝突しない。ホストへ出るのは news の API ポート (REST 9008) のみ。

```bash
make run      # アプリ + インフラを compose で起動（ソースをバインドマウント）
make down     # 停止して volume を削除
make test     # Testcontainers でテスト実行（Docker 必須）
```

アプリはコンテナ内で `go run` する。ソースを編集して `docker compose restart news` すれば、
イメージを作り直さずに反映される。プライベートモジュールはホストのモジュールキャッシュを読み取り専用でマウント
して解決するため、`make run` は先にホスト側で `go mod download` を実行する。
