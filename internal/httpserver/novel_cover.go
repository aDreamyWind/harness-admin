package httpserver

import (
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
	group.GET("/projects/:id/cover/history", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		id := c.Param("id")
		items, err := deps.Novel.ListCoverHistory(c.Request.Context(), novelOwner(c), id)
		for i := range items {
			items[i].URL = novelCoverImageURL(id, items[i].Revision)
		}
		writeNovelResponse(c, deps, http.StatusOK, items, err)
	})
	group.POST("/projects/:id/cover/history/apply", requireCSRF(deps), func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		var input struct {
			Revision string `json:"revision"`
		}
		if !bindNovelInput(c, &input) {
			return
		}
		id := c.Param("id")
		cover, err := deps.Novel.ApplyCoverHistory(c.Request.Context(), novelOwner(c), id, input.Revision)
		writeNovelResponse(c, deps, http.StatusOK, novelCoverWithURL(id, &cover), err)
	})
	group.POST("/projects/:id/cover", requireCSRF(deps), func(c *gin.Context) {
		data, err := readCoverUpload(c)
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
		c.Header("Cache-Control", "private, no-store")
		writeNovelResponse(c, deps, http.StatusOK, novelCoverPreviewWithURL(id, token), err)
	})
	group.POST("/projects/:id/cover/generation", requireCSRF(deps), func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		var input novel.GenerateCoverInput
		if !bindNovelInput(c, &input) {
			return
		}
		id := c.Param("id")
		generation, err := deps.Novel.StartCoverGeneration(c.Request.Context(), novelOwner(c), id, input)
		writeNovelResponse(c, deps, http.StatusAccepted, novelCoverGenerationWithPreview(id, generation), err)
	})
	group.GET("/projects/:id/cover/generation", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		id := c.Param("id")
		generation, err := deps.Novel.GetCoverGeneration(c.Request.Context(), novelOwner(c), id)
		writeNovelResponse(c, deps, http.StatusOK, novelCoverGenerationWithPreview(id, generation), err)
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

type novelCoverPreviewResponse struct {
	Token      string `json:"token"`
	PreviewURL string `json:"previewUrl"`
}

type novelCoverGenerationResponse struct {
	Status       string                     `json:"status"`
	JobID        string                     `json:"jobId,omitempty"`
	Preview      *novelCoverPreviewResponse `json:"preview,omitempty"`
	ErrorMessage string                     `json:"errorMessage,omitempty"`
	ModelCode    string                     `json:"modelCode,omitempty"`
	StyleID      string                     `json:"styleId,omitempty"`
	Instruction  string                     `json:"instruction,omitempty"`
}

func novelCoverPreviewWithURL(id, token string) *novelCoverPreviewResponse {
	if token == "" {
		return nil
	}
	return &novelCoverPreviewResponse{
		Token: token, PreviewURL: "/api/v1/ai-novel/projects/" + url.PathEscape(id) + "/cover/previews/" + url.PathEscape(token),
	}
}

func novelCoverGenerationWithPreview(id string, generation novel.CoverGeneration) novelCoverGenerationResponse {
	return novelCoverGenerationResponse{
		Status: generation.Status, JobID: generation.JobID, Preview: novelCoverPreviewWithURL(id, generation.PreviewToken),
		ErrorMessage: generation.ErrorMessage, ModelCode: generation.ModelCode, StyleID: generation.StyleID, Instruction: generation.Instruction,
	}
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

func novelCoverWithURL(projectID string, cover *novel.Cover) *novel.Cover {
	if cover == nil || cover.Revision == "" {
		return nil
	}
	result := *cover
	result.URL = novelCoverImageURL(projectID, cover.Revision)
	return &result
}

func novelCoverImageURL(projectID, revision string) string {
	return "/api/v1/ai-novel/projects/" + url.PathEscape(projectID) + "/cover?v=" + url.QueryEscape(revision)
}
