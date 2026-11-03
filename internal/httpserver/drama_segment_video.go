package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaSegmentVideoService interface {
	StartSegmentVideo(context.Context, drama.SegmentVideoScope, int) (drama.SegmentVideoJob, error)
	GenerateSegmentVideo(drama.SegmentVideoJob) error
	SegmentVideos(context.Context, drama.SegmentVideoScope) ([]drama.SegmentVideo, error)
	SegmentVideoCalls(context.Context, drama.SegmentVideoScope, string) ([]drama.SegmentVideoCall, error)
	SegmentVideoContent(context.Context, drama.SegmentVideoScope, string) (io.ReadSeekCloser, time.Time, error)
}

func segmentVideoScope(c *gin.Context) drama.SegmentVideoScope {
	u := currentSession(c).User
	return drama.SegmentVideoScope{OrganizationID: u.OrganizationID, UserID: u.ID, ProjectID: c.Param("id"), EpisodeID: c.Param("episodeId"), RunID: c.Param("runId"), SegmentID: c.Param("segmentId")}
}

func registerDramaSegmentVideoRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama/projects/:id/video-episodes/:episodeId/storyboards/:runId/segments/:segmentId/videos", requireSession(deps))
	group.GET("", func(c *gin.Context) {
		jobs, err := deps.DramaSegmentVideos.SegmentVideos(c.Request.Context(), segmentVideoScope(c))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		httpx.OK(c, http.StatusOK, jobs)
	})
	group.POST("", requireCSRF(deps), func(c *gin.Context) {
		var in struct {
			Revision int `json:"revision"`
		}
		if err := c.ShouldBindJSON(&in); err != nil || in.Revision < 1 {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		job, err := deps.DramaSegmentVideos.StartSegmentVideo(c.Request.Context(), segmentVideoScope(c), in.Revision)
		if errors.Is(err, drama.ErrSegmentVideoBusy) {
			httpx.Fail(c, http.StatusConflict, "drama_video_busy", err.Error())
			return
		}
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		go func() {
			if err := deps.DramaSegmentVideos.GenerateSegmentVideo(job); err != nil {
				deps.Logger.Error("片段视频生成失败", "video_id", job.Video.ID, "error", err)
			}
		}()
		httpx.OK(c, http.StatusAccepted, job.Video)
	})
	group.GET("/:videoId/calls", func(c *gin.Context) {
		calls, err := deps.DramaSegmentVideos.SegmentVideoCalls(c.Request.Context(), segmentVideoScope(c), c.Param("videoId"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		httpx.OK(c, http.StatusOK, calls)
	})

	group.GET("/:videoId/content", func(c *gin.Context) {
		file, modified, err := deps.DramaSegmentVideos.SegmentVideoContent(c.Request.Context(), segmentVideoScope(c), c.Param("videoId"))
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		defer file.Close()
		c.Header("Content-Type", "video/mp4")
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		http.ServeContent(c.Writer, c.Request, "segment.mp4", modified, file)
	})
}
