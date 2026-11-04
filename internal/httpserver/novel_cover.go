package httpserver

import (
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/novel"
)

func registerNovelCoverRoutes(group *gin.RouterGroup, deps Dependencies) {
	group.GET("/cover-models", func(c *gin.Context) {
		models, err := deps.Novel.CoverModels(c.Request.Context(), novelOwner(c))
		writeNovelResponse(c, deps, http.StatusOK, models, err)
	})
	group.GET("/projects/:id/cover", func(c *gin.Context) {
		image, err := deps.Novel.CoverContent(c.Request.Context(), novelOwner(c), c.Param("id"), c.Query("v"))
		writeNovelCoverImage(c, deps, image, err)
	})
	group.POST("/projects/:id/cover", requireCSRF(deps), func(c *gin.Context) {
		data, err := readNovelCoverUpload(c)
		if err != nil {
			writeNovelResponse(c, deps, http.StatusOK, nil, err)
			return
		}
		id := c.Param("id")
		cover, err := deps.Novel.UploadCover(c.Request.Context(), novelOwner(c), id, data)
		writeNovelResponse(c, deps, http.StatusOK, novelCoverWithURL(id, &cover), err)
	})
	group.POST("/projects/:id/cover/generate", requireCSRF(deps), func(c *gin.Context) {
		var input novel.GenerateCoverInput
		if !bindNovelInput(c, &input) {
			return
		}
		id := c.Param("id")
		token, err := deps.Novel.GenerateCover(c.Request.Context(), novelOwner(c), id, input)
		previewURL := "/api/v1/ai-novel/projects/" + url.PathEscape(id) + "/cover/previews/" + url.PathEscape(token)
		c.Header("Cache-Control", "private, no-store")
		writeNovelResponse(c, deps, http.StatusOK, gin.H{"token": token, "previewUrl": previewURL}, err)
	})
	group.GET("/projects/:id/cover/previews/:token", func(c *gin.Context) {
		image, err := deps.Novel.CoverPreviewContent(c.Request.Context(), novelOwner(c), c.Param("id"), c.Param("token"))
		writeNovelCoverImage(c, deps, image, err)
	})
	group.POST("/projects/:id/cover/apply", requireCSRF(deps), func(c *gin.Context) {
		var input struct {
			Token string `json:"token"`
		}
		if !bindNovelInput(c, &input) {
			return
		}
		id := c.Param("id")
		cover, err := deps.Novel.ApplyCoverPreview(c.Request.Context(), novelOwner(c), id, input.Token)
		writeNovelResponse(c, deps, http.StatusOK, novelCoverWithURL(id, &cover), err)
	})
}

func writeNovelCoverImage(c *gin.Context, deps Dependencies, image novel.CoverImage, err error) {
	c.Header("Cache-Control", "private, no-store")
	if err != nil {
		writeNovelResponse(c, deps, http.StatusOK, nil, err)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cross-Origin-Resource-Policy", "same-origin")
	c.Data(http.StatusOK, image.MIME, image.Data)
}

func readNovelCoverUpload(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, novel.MaxCoverBytes+64*1024)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, novel.ErrCoverImage
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return nil, novel.ErrCoverImage
	}
	data, err := io.ReadAll(io.LimitReader(part, novel.MaxCoverBytes+1))
	if err != nil || len(data) == 0 || len(data) > novel.MaxCoverBytes {
		return nil, novel.ErrCoverImage
	}
	if _, err := reader.NextPart(); err != io.EOF {
		return nil, novel.ErrCoverImage
	}
	return data, nil
}

func novelCoverWithURL(projectID string, cover *novel.Cover) *novel.Cover {
	if cover == nil || cover.Revision == "" {
		return nil
	}
	result := *cover
	result.URL = "/api/v1/ai-novel/projects/" + url.PathEscape(projectID) + "/cover?v=" + url.QueryEscape(cover.Revision)
	return &result
}
