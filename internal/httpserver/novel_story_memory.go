package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
	"harness-admin/internal/novel"
)

func registerNovelStoryMemoryRoutes(group *gin.RouterGroup, deps Dependencies) {
	memory := group.Group("/projects/:id/story-memory", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	})
	memory.GET("", func(c *gin.Context) {
		result, err := deps.Novel.GetStoryMemory(c.Request.Context(), novelOwner(c), c.Param("id"))
		writeNovelResponse(c, deps, http.StatusOK, result, err)
	})
	memory.POST("/resolve", requireCSRF(deps), func(c *gin.Context) {
		var input novel.MemoryResolutionInput
		if !bindNovelInput(c, &input) {
			return
		}
		result, err := deps.Novel.ResolveStoryMemory(c.Request.Context(), novelOwner(c), c.Param("id"), input)
		writeNovelResponse(c, deps, http.StatusOK, result, err)
	})
	memory.POST("/backfill", requireCSRF(deps), func(c *gin.Context) {
		var input map[string]json.RawMessage
		if !bindNovelInput(c, &input) {
			return
		}
		if input == nil {
			httpx.Fail(c, http.StatusBadRequest, "invalid_novel_input", "补提取请求需要 JSON 对象")
			return
		}
		result, err := deps.Novel.BackfillStoryMemory(c.Request.Context(), novelOwner(c), c.Param("id"))
		writeNovelResponse(c, deps, http.StatusAccepted, result, err)
	})
}
