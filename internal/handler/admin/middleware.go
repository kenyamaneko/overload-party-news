// Package admin は運用者向け管理 UI (HTMX + html/template) の delivery 層。
// 認証は IAP に委譲し、news コード内では X-Goog-Authenticated-User-Email ヘッダだけを信頼する。
package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/config"
)

// iapEmailHeader は IAP が認証成功時に付与するヘッダ名。
// 値は "<identity-provider>:<email>" 形式 (例: "accounts.google.com:user@example.com")。
const iapEmailHeader = "X-Goog-Authenticated-User-Email"

// localReviewerFallback は ENV=local のときに reviewer として注入する固定値。
// 本番で誤って使われないよう config.Env で分岐する。
const localReviewerFallback = "local-dev@example.com"

// reviewerContextKey は context に reviewer を埋めるためのキー。
// 型を独立させて誤衝突を防ぐ。
type reviewerContextKey struct{}

// ErrMissingIAPHeader は IAP ヘッダが欠けているときに返す。handler は 401 にマップする。
var ErrMissingIAPHeader = errors.New("missing IAP authenticated user header")

// AuthMiddleware は IAP ヘッダの存在確認と reviewer の context 注入を行う gin middleware を返す。
// ENV=local のときはヘッダ不要で localReviewerFallback を注入する。
func AuthMiddleware(env config.Env) gin.HandlerFunc {
	if env == config.EnvLocal {
		return func(c *gin.Context) {
			c.Set(reviewerKey(), localReviewerFallback)
			c.Next()
		}
	}
	return func(c *gin.Context) {
		email, err := extractIAPEmail(c.GetHeader(iapEmailHeader))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set(reviewerKey(), email)
		c.Next()
	}
}

// reviewerKey は gin.Context.Set/Get に使う文字列キー。
// 型ベースのキーを gin は直接扱えないため、パッケージ内で stable な文字列にする。
func reviewerKey() string {
	return "admin.reviewer"
}

// Reviewer は IAP 由来の運用者 email を取り出す。
// middleware 未適用で呼ばれると空文字列を返すため、handler 側で空チェックはしない前提 (middleware チェーンの契約)。
func Reviewer(c *gin.Context) string {
	v, ok := c.Get(reviewerKey())
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// extractIAPEmail は IAP ヘッダ値から email 部分を取り出す。
// 想定フォーマット: "<provider>:<email>" (例: "accounts.google.com:alice@example.com")。
// プレフィックス無し (":" が無い) の値も許容し、そのまま email として扱う (テスト容易性のため)。
func extractIAPEmail(raw string) (string, error) {
	if raw == "" {
		return "", ErrMissingIAPHeader
	}
	if idx := strings.Index(raw, ":"); idx >= 0 {
		email := raw[idx+1:]
		if email == "" {
			return "", ErrMissingIAPHeader
		}
		return email, nil
	}
	return raw, nil
}
