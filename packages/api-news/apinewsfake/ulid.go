package apinewsfake

import (
	"crypto/rand"
	"fmt"
)

// newArticleID は news の PK (VARCHAR(26)) に収まる 26 文字の Crockford base32 文字列を返す。
// テスト用 fake が ArticleID 未指定時の埋め草として使う。strict ULID 形式は保証しない。
func newArticleID() string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var b [26]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("apinewsfake: crypto/rand.Read failed: %v", err))
	}
	out := make([]byte, 26)
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}
