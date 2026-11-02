package httpserver

import (
	"context"
	"github.com/gin-gonic/gin"
	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaStoryboardService interface {
	StartStoryboardPacing(context.Context, string, string, string, string, string, drama.StoryboardPacingRequest) (drama.StoryboardJob, error)
	Storyboards(context.Context, string, string, string, string, string) (drama.StoryboardPage, error)
	StartStoryboard(context.Context, string, string, string, string, bool) (drama.StoryboardJob, error)
	GenerateStoryboard(drama.StoryboardJob) error
	EditStoryboard(context.Context, string, string, string, string, string, drama.StoryboardEdit) (drama.StoryboardRun, error)
	ConfirmStoryboard(context.Context, string, string, string, string, string, int) error
}

func registerDramaStoryboardRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama/projects/:id/video-episodes/:episodeId/storyboards", requireSession(deps))
	group.GET("", func(c *gin.Context) {
		u := currentSession(c).User
		out, err := deps.DramaStoryboards.Storyboards(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("episodeId"), c.Query("run"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		httpx.OK(c, 200, out)
	})
	group.POST("", requireCSRF(deps), func(c *gin.Context) {
		var in struct {
			Resume bool `json:"resume"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		job, err := deps.DramaStoryboards.StartStoryboard(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("episodeId"), in.Resume)
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		go func() {
			if err := deps.DramaStoryboards.GenerateStoryboard(job); err != nil {
				deps.Logger.Error("单集分镜拆分失败", "run_id", job.Run.ID, "error", err)
			}
		}()
		httpx.OK(c, 202, job.Run)
	})
	group.POST("/:runId/optimize-pacing", requireCSRF(deps), func(c *gin.Context) {
		var in drama.StoryboardPacingRequest
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		if in.Revision < 1 || in.SegmentID == "" {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		job, err := deps.DramaStoryboards.StartStoryboardPacing(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("episodeId"), c.Param("runId"), in)
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		go func() {
			if err := deps.DramaStoryboards.GenerateStoryboard(job); err != nil {
				deps.Logger.Error("分镜节奏优化失败", "run_id", job.Run.ID, "error", err)
			}
		}()
		httpx.OK(c, 202, job.Run)
	})
	group.PUT("/:runId", requireCSRF(deps), func(c *gin.Context) {
		var in drama.StoryboardEdit
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		out, err := deps.DramaStoryboards.EditStoryboard(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("episodeId"), c.Param("runId"), in)
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, 200, out)
	})
	group.POST("/:runId/confirm", requireCSRF(deps), func(c *gin.Context) {
		var in struct {
			Revision int `json:"revision"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		if err := deps.DramaStoryboards.ConfirmStoryboard(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), c.Param("episodeId"), c.Param("runId"), in.Revision); err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, 200, gin.H{"confirmed": true})
	})
}
