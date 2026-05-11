package rest

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// HeaderName は内部認証 JWT を運ぶ HTTP ヘッダ名。
const HeaderName = "X-Internal-Auth"

// PlayerIDContextKey は middleware が gin.Context に player_id を保存するキー名。
// handler は c.GetString(PlayerIDContextKey) で取り出す。
const PlayerIDContextKey = "player_id"

// VerifyInternalAuth は X-Internal-Auth (HMAC JWT) の検証を行う Gin middleware を返す。
// header 欠落 / 検証失敗時はいずれも 401 を返す。
func VerifyInternalAuth(verifier port.InternalAuthVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(HeaderName)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": HeaderName + " header is required"})
			return
		}
		playerID, err := verifier.Verify(token)
		if err != nil {
			slog.WarnContext(c.Request.Context(), "internal auth verify failed",
				slog.String("error", err.Error()),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid internal auth token"})
			return
		}
		c.Set(PlayerIDContextKey, playerID)
		c.Next()
	}
}
