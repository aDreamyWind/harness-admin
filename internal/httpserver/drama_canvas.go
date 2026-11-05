package httpserver

import (
	"context"
	"github.com/gin-gonic/gin"
	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
	"net/http"
)

type DramaVisualService interface {
	Canvas(context.Context, string, string, string) (drama.Canvas, error)
	SaveCanvas(context.Context, string, string, string, drama.CanvasLayout) (int, error)
	CreateCanvasAsset(context.Context, string, string, string, drama.CanvasAssetInput) (drama.CanvasAssetResult, error)
	EditForm(context.Context, string, string, string, string, drama.FormEdit) error
	DeriveForm(context.Context, string, string, string, string) (string, error)
	StartImage(context.Context, string, string, string, string, drama.ImageInput) (drama.ImageJob, error)
	GenerateImage(drama.ImageJob) error
	ImageContent(context.Context, string, string, string, string) ([]byte, string, error)
}

func registerDramaCanvasRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama/projects/:id", requireSession(deps))
	group.POST("/canvas/assets", requireCSRF(deps), createDramaCanvasAsset(deps))
	group.GET("/canvas", func(c *gin.Context) {
		u := currentSession(c).User
		out, err := deps.DramaVisual.Canvas(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		httpx.OK(c, 200, out)
	})
	group.PUT("/canvas/layout", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
		var in drama.CanvasLayout
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		revision, err := deps.DramaVisual.SaveCanvas(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), in)
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, 200, gin.H{"revision": revision})
	})
	group.PUT("/forms/:formId", requireCSRF(deps), func(c *gin.Context) {
		var in drama.FormEdit
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		if err := deps.DramaVisual.EditForm(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("formId"), in); err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, 200, gin.H{"saved": true})
	})
	group.POST("/forms/:formId/derive", requireCSRF(deps), func(c *gin.Context) {
		u := currentSession(c).User
		id, err := deps.DramaVisual.DeriveForm(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("formId"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, 201, gin.H{"id": id})
	})
	group.POST("/forms/:formId/images", requireCSRF(deps), func(c *gin.Context) {
		var in drama.ImageInput
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		job, err := deps.DramaVisual.StartImage(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("formId"), in)
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		go func() {
			if err := deps.DramaVisual.GenerateImage(job); err != nil {
				deps.Logger.Error("漫剧资产图片生成失败", "job_id", job.ID, "error", err)
			}
		}()
		httpx.OK(c, http.StatusAccepted, job)
	})
	group.GET("/images/:imageId/content", func(c *gin.Context) {
		u := currentSession(c).User
		data, mime, err := deps.DramaVisual.ImageContent(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("imageId"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, mime, data)
	})
}
