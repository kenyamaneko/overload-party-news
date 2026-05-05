package port

import "errors"

// ErrNotFound は記事が存在しない / 公開可能条件を満たさない場合のセンチネル。handler は 404 にマップ。
var ErrNotFound = errors.New("article not found")

// ErrInvalidPersistedValue は永続化制約 (CHECK / NOT NULL 等) 違反のセンチネル。handler は 400 にマップ。
var ErrInvalidPersistedValue = errors.New("value violates persistence constraint")
