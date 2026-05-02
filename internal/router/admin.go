package router

import (
	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/config"
	"github.com/kenyamaneko/overload-party-news/internal/handler/admin"
)

// NewAdmin は管理 UI のルータを構築する。/admin/* に IAP middleware を適用する (ENV=local はパススルー)。
func NewAdmin(env config.Env, adminH *admin.Handler) *gin.Engine {
	r := gin.New()
	r.Use(requestLogger(), gin.Recovery())

	r.GET("/health", healthHandler)

	adminGroup := r.Group("/admin", admin.AuthMiddleware(env))
	{
		adminGroup.GET("/articles", adminH.List)
		adminGroup.GET("/articles/:articleId", adminH.GetEdit)
		adminGroup.POST("/articles/:articleId/translations/:lang", adminH.UpsertTranslation)
		adminGroup.POST("/articles/:articleId/publish", adminH.Publish)
		adminGroup.POST("/articles/:articleId/reject", adminH.Reject)
	}
	return r
}
