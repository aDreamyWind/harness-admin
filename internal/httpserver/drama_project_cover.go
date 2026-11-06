package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/coverimage"
	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaProjectCoverService interface {
	GetProjectCoverGeneration(context.Context, drama.CoverScope) (drama.CoverGeneration, error)
	StartProjectCoverGeneration(context.Context, drama.CoverScope, drama.CoverGenerationInput) (drama.CoverGeneration, error)
	SelectProjectCover(context.Context, drama.CoverScope, drama.CoverSelection) (drama.CoverGeneration, error)
	ProjectCoverImage(context.Context, drama.CoverScope, string) (drama.CoverImage, error)
	ListProjectCoverHistory(context.Context, drama.CoverScope) ([]drama.CoverRevision, error)
	ApplyProjectCoverHistory(context.Context, drama.CoverScope, string) (drama.CoverGeneration, error)
	UploadProjectCover(context.Context, drama.CoverScope, []byte) (drama.CoverGeneration, error)
}

func registerDramaProjectCoverRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama/projects/:id/cover", requireSession(deps))
	service := deps.DramaProjectCover
	group.GET("/generation", func(c *gin.Context) {
		state, err := service.GetProjectCoverGeneration(c.Request.Context(), dramaCoverScope(c))
		writeDramaCoverResponse(c, deps, http.StatusOK, state, err)
	})
	group.POST("/generation", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var input drama.CoverGenerationInput
		if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
			writeDramaCoverResponse(c, deps, http.StatusBadRequest, nil, drama.ErrInvalidInput)
			return
		}
		state, err := service.StartProjectCoverGeneration(c.Request.Context(), dramaCoverScope(c), input)
		writeDramaCoverResponse(c, deps, http.StatusAccepted, state, err)
	})
	group.POST("/selection", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var input drama.CoverSelection
		if err := c.ShouldBindJSON(&input); err != nil {
			writeDramaCoverResponse(c, deps, http.StatusBadRequest, nil, drama.ErrInvalidInput)
			return
		}
		state, err := service.SelectProjectCover(c.Request.Context(), dramaCoverScope(c), input)
		writeDramaCoverResponse(c, deps, http.StatusOK, state, err)
	})
	group.GET("/history", func(c *gin.Context) {
		history, err := service.ListProjectCoverHistory(c.Request.Context(), dramaCoverScope(c))
		writeDramaCoverResponse(c, deps, http.StatusOK, history, err)
	})
	group.POST("/history/apply", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var input struct {
			Revision string `json:"revision"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			writeDramaCoverResponse(c, deps, http.StatusBadRequest, nil, drama.ErrInvalidInput)
			return
		}
		state, err := service.ApplyProjectCoverHistory(c.Request.Context(), dramaCoverScope(c), input.Revision)
		writeDramaCoverResponse(c, deps, http.StatusOK, state, err)
	})
	group.POST("/upload", requireCSRF(deps), func(c *gin.Context) {
		data, err := readCoverUpload(c)
		if err != nil {
			writeDramaCoverResponse(c, deps, http.StatusBadRequest, nil, err)
			return
		}
		state, err := service.UploadProjectCover(c.Request.Context(), dramaCoverScope(c), data)
		writeDramaCoverResponse(c, deps, http.StatusOK, state, err)
	})
	group.GET("/images/:revision", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		image, err := service.ProjectCoverImage(c.Request.Context(), dramaCoverScope(c), c.Param("revision"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
		c.Header("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox")
		c.Data(http.StatusOK, image.MIME, image.Data)
	})
}

func dramaCoverScope(c *gin.Context) drama.CoverScope {
	user := currentSession(c).User
	return drama.CoverScope{OrganizationID: user.OrganizationID, UserID: user.ID, ProjectID: c.Param("id")}
}

func writeDramaCoverResponse(c *gin.Context, deps Dependencies, status int, data any, err error) {
	c.Header("Cache-Control", "private, no-store")
	if errors.Is(err, drama.ErrCoverConflict) {
		httpx.Fail(c, http.StatusConflict, "drama_cover_conflict", err.Error())
		return
	}
	if errors.Is(err, coverimage.ErrInvalidUpload) {
		httpx.Fail(c, http.StatusBadRequest, "drama_cover_invalid_image", err.Error())
		return
	}
	if err != nil {
		handleDramaError(c, deps, err)
		return
	}
	httpx.OK(c, status, data)
}
