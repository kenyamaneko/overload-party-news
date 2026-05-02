package review

import "errors"

// ErrInvalidField は編集時のバリデーション違反のセンチネル。具体的な違反は wrap した message で識別する。
var ErrInvalidField = errors.New("invalid field")
