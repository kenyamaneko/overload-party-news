package review

import "errors"

// ErrInvalidField は編集時のバリデーション違反。handler は 400 にマップする。
// 具体的な違反は wrap したメッセージで識別する。
var ErrInvalidField = errors.New("invalid field")
