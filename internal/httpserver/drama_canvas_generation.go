package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaCanvasGenerationService interface {
	ListCanvasGenerationModels(context.Context, string, string) ([]drama.CanvasGenerationModel, error)
	ListCanvasGenerations(context.Context, string, string, string) ([]drama.CanvasGenerationJob, error)
	GetCanvasGeneration(context.Context, string, string, string, string) (*drama.CanvasGenerationJob, error)
	StartCanvasGeneration(context.Context, string, string, string, string, drama.CanvasGenerationInput) (drama.CanvasGenerationJob, error)
}

func registerDramaCanvasGenerationRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama", requireSession(deps))
	service := deps.DramaCanvasGeneration
	group.GET("/canvas/generation-models", func(c *gin.Context) {
		user := currentSession(c).User
		models, err := service.ListCanvasGenerationModels(c.Request.Context(), user.OrganizationID, user.ID)
		if models == nil {
			models = []drama.CanvasGenerationModel{}
		}
		writeCanvasGenerationResponse(c, deps, http.StatusOK, models, err)
	})
	group.GET("/projects/:id/canvas/generations", func(c *gin.Context) {
		user := currentSession(c).User
		jobs, err := service.ListCanvasGenerations(c.Request.Context(), user.OrganizationID, user.ID, c.Param("id"))
		if jobs == nil {
			jobs = []drama.CanvasGenerationJob{}
		}
		writeCanvasGenerationResponse(c, deps, http.StatusOK, jobs, err)
	})
	const nodePath = "/projects/:id/canvas/nodes/:nodeId/generation"
	group.GET(nodePath, func(c *gin.Context) {
		user := currentSession(c).User
		job, err := service.GetCanvasGeneration(c.Request.Context(), user.OrganizationID, user.ID, c.Param("id"), c.Param("nodeId"))
		writeCanvasGenerationResponse(c, deps, http.StatusOK, job, err)
	})
	group.POST(nodePath, requireCSRF(deps), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
		var input drama.CanvasGenerationInput
		if err := c.ShouldBindJSON(&input); err != nil || input.Revision < 1 {
			writeCanvasGenerationResponse(c, deps, http.StatusBadRequest, nil, drama.ErrInvalidInput)
			return
		}
		user := currentSession(c).User
		job, err := service.StartCanvasGeneration(c.Request.Context(), user.OrganizationID, user.ID, c.Param("id"), c.Param("nodeId"), input)
		writeCanvasGenerationResponse(c, deps, http.StatusAccepted, job, err)
	})
}

func writeCanvasGenerationResponse(c *gin.Context, deps Dependencies, status int, data any, err error) {
	c.Header("Cache-Control", "private, no-store")
	if err != nil {
		handleDramaError(c, deps, err)
		return
	}
	httpx.OK(c, status, data)
}
