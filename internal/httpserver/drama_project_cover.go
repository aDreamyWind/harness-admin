package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaProjectCoverService interface {
	GetProjectCoverGeneration(context.Context, drama.CoverScope) (drama.CoverGeneration, error)
	StartProjectCoverGeneration(context.Context, drama.CoverScope, string) (drama.CoverGeneration, error)
	SelectProjectCover(context.Context, drama.CoverScope, drama.CoverSelection) (drama.CoverGeneration, error)
	ProjectCoverImage(context.Context, drama.CoverScope, string) (drama.CoverImage, error)
}

func registerDramaProjectCoverRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama/projects/:id/cover", requireSession(deps))
	service := deps.DramaProjectCover
	group.GET("/generation", func(c *gin.Context) {
		state, err := service.GetProjectCoverGeneration(c.Request.Context(), dramaCoverScope(c))
		writeDramaCoverGeneration(c, deps, http.StatusOK, state, err)
	})
	group.POST("/generation", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var input struct {
			ModelCode string `json:"modelCode"`
		}
		if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
			writeDramaCoverGeneration(c, deps, http.StatusBadRequest, drama.CoverGeneration{}, drama.ErrInvalidInput)
			return
		}
		state, err := service.StartProjectCoverGeneration(c.Request.Context(), dramaCoverScope(c), input.ModelCode)
		writeDramaCoverGeneration(c, deps, http.StatusAccepted, state, err)
	})
	group.POST("/selection", requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var input drama.CoverSelection
		if err := c.ShouldBindJSON(&input); err != nil {
			writeDramaCoverGeneration(c, deps, http.StatusBadRequest, drama.CoverGeneration{}, drama.ErrInvalidInput)
			return
		}
		state, err := service.SelectProjectCover(c.Request.Context(), dramaCoverScope(c), input)
		writeDramaCoverGeneration(c, deps, http.StatusOK, state, err)
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

func writeDramaCoverGeneration(c *gin.Context, deps Dependencies, status int, state drama.CoverGeneration, err error) {
	c.Header("Cache-Control", "private, no-store")
	if errors.Is(err, drama.ErrCoverConflict) {
		httpx.Fail(c, http.StatusConflict, "drama_cover_conflict", err.Error())
		return
	}
	if err != nil {
		handleDramaError(c, deps, err)
		return
	}
	httpx.OK(c, status, state)
}
