package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaCanvasMediaService interface {
	UploadCanvasMedia(context.Context, string, string, string, io.Reader) (drama.CanvasMedia, error)
	ReadCanvasMedia(context.Context, string, string, string, string) (drama.CanvasMediaContent, error)
}

func registerDramaCanvasMediaRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama/projects/:id/canvas/media", requireSession(deps))
	group.POST("", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, drama.MaxCanvasMediaBytes+(64<<10))
		if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeCanvasMediaUploadError(c, deps, drama.ErrCanvasMediaSize)
				return
			}
			httpx.Fail(c, http.StatusBadRequest, "invalid_canvas_media", "请选择一个素材文件")
			return
		}
		form := c.Request.MultipartForm
		defer form.RemoveAll()
		files := form.File["file"]
		if len(form.File) != 1 || len(files) != 1 || len(form.Value) != 0 {
			httpx.Fail(c, http.StatusBadRequest, "invalid_canvas_media", "请选择一个素材文件")
			return
		}
		if files[0].Size > drama.MaxCanvasMediaBytes {
			writeCanvasMediaUploadError(c, deps, drama.ErrCanvasMediaSize)
			return
		}
		file, err := files[0].Open()
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		defer file.Close()
		user := currentSession(c).User
		project := c.Param("id")
		media, err := deps.DramaCanvasMedia.UploadCanvasMedia(c.Request.Context(), user.OrganizationID, user.ID, project, file)
		if err != nil {
			writeCanvasMediaUploadError(c, deps, err)
			return
		}
		httpx.OK(c, http.StatusCreated, struct {
			drama.CanvasMedia
			URL string `json:"url"`
		}{media, "/api/v1/ai-drama/projects/" + url.PathEscape(project) + "/canvas/media/" + url.PathEscape(media.ID) + "/content"})
	})
	group.GET("/:mediaId/content", func(c *gin.Context) {
		user := currentSession(c).User
		media, err := deps.DramaCanvasMedia.ReadCanvasMedia(c.Request.Context(), user.OrganizationID, user.ID, c.Param("id"), c.Param("mediaId"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		defer media.File.Close()
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
		c.Header("Content-Type", media.MIME)
		http.ServeContent(c.Writer, c.Request, "", media.UpdatedAt, media.File)
	})
}

func writeCanvasMediaUploadError(c *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, drama.ErrCanvasMediaSize):
		httpx.Fail(c, http.StatusRequestEntityTooLarge, "canvas_media_too_large", err.Error())
	case errors.Is(err, drama.ErrCanvasMediaType):
		httpx.Fail(c, http.StatusUnsupportedMediaType, "unsupported_canvas_media", err.Error())
	default:
		handleDramaError(c, deps, err)
	}
}
