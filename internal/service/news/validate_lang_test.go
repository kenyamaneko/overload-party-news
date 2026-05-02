package news

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

// 仕様 (FEATURE_SPEC §4): validateLang は空 / 未対応値を専用エラーで弾き、
// 対応言語のみを通過させる。handler が 400 のメッセージを書き分けるため、
// 空と未対応を別エラーで区別する。
func TestValidateLang_仕様(t *testing.T) {
	cases := []struct {
		name    string
		lang    string
		wantErr error
	}{
		{
			name:    "ja は通過",
			lang:    domain.LangJa,
			wantErr: nil,
		},
		{
			name:    "en は通過",
			lang:    domain.LangEn,
			wantErr: nil,
		},
		{
			name:    "空は ErrLangRequired",
			lang:    "",
			wantErr: ErrLangRequired,
		},
		{
			name:    "対応外 (fr) は ErrUnsupportedLang",
			lang:    "fr",
			wantErr: ErrUnsupportedLang,
		},
		{
			name:    "対応外 (大文字 JA) は ErrUnsupportedLang",
			lang:    "JA",
			wantErr: ErrUnsupportedLang,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLang(tc.lang)
			if tc.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.True(t, errors.Is(err, tc.wantErr))
		})
	}
}
