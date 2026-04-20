// Package port は外部永続装置・外部サービスとの境界を定義するインタフェース集合。
//
// 依存方向: service → port、repository/adapter → port を実装。
// port パッケージ自体はどの外部ライブラリにも依存せず、ドメイン型 (apinews) だけを参照する。
package port

import "errors"

// ErrNotFound は記事が存在しない (または公開可能条件を満たさない) 場合に返すセンチネル。
// handler 層が 404 にマップする。
var ErrNotFound = errors.New("article not found")
