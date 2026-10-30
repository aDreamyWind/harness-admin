package httpserver

import (
	"context"
	"github.com/gin-gonic/gin"
	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaStyleService interface {
	Styles(context.Context) ([]drama.StyleOption, error)
}

func registerDramaStyleRoutes(router *gin.Engine, deps Dependencies) {
	router.GET("/api/v1/ai-drama/styles", requireSession(deps), func(c *gin.Context) {
		styles, err := deps.DramaStyles.Styles(c.Request.Context())
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, 200, styles)
	})
}
