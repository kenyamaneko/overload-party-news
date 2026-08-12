# overload-party-news

クラウドニュース記事の校閲・配信を行う内部マイクロサービス。`news-article-collected` Pub/Sub イベントを購読して自スキーマに永続化し、校閲を通った記事を gateway 経由でクライアントへ配信する。起動するのは gateway 向け REST のポート 9008 のみ。

詳細は [REST 契約](data/openapi.yaml) / [Pub/Sub 契約](data/asyncapi.yaml) / [データ設計書](docs/DATA_DESIGN.md) を参照。設計判断 (Why) は [common の ADR](https://github.com/kenyamaneko/overload-party-common/tree/main/docs/adr)、サービス構成全体の図は [common のシステム構成図](https://github.com/kenyamaneko/overload-party-common#システム構成図) を参照。ローカル開発は [docs/SETUP.md](docs/SETUP.md) を参照。

[テスト観点カタログ](https://kenyamaneko.github.io/overload-party-news/): テスト名から生成した、テスト済みの観点の一覧。
