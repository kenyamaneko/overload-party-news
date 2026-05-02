// Package port は外部永続装置・外部サービスとの境界を定義するインタフェース集合。
//
// 依存方向: usecase → port、repository/adapter → port を実装。
// port パッケージ自体はどの外部ライブラリにも依存せず、ドメイン型 (apinews) だけを参照する。
package port

import "errors"

// ErrNotFound は記事が存在しない (または公開可能条件を満たさない) 場合に返すセンチネル。
// handler 層が 404 にマップする。
var ErrNotFound = errors.New("article not found")

// ErrInvalidPersistedValue は永続化制約 (CHECK / NOT NULL 等) に違反した入力が来た場合に返すセンチネル。
// 例: news_article_translations.lang は CHECK で許容値を絞っており、未対応 lang はここで弾かれる。
// usecase 層がこれを受けたらバリデーションエラー (handler が 400 にマップ) として扱う。
var ErrInvalidPersistedValue = errors.New("value violates persistence constraint")
