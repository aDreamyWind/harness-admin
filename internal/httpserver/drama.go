package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

type DramaService interface {
	AvailableModels(context.Context, string, string) ([]drama.Model, error)
	GetSettings(context.Context, string, string) (drama.Settings, error)
	SaveSettings(context.Context, drama.Settings) (drama.Settings, error)
	Create(context.Context, drama.CreateInput) (drama.Project, error)
	Retry(context.Context, string, string, string) (drama.Project, error)
	ReanalyzeOverview(context.Context, string, string, string) (drama.Project, error)
	List(context.Context, string, string) ([]drama.Project, error)
	Get(context.Context, string, string, string) (drama.Project, error)
	GetSource(context.Context, string, string, string) (string, error)
	SourcePage(context.Context, string, string, string, int, int) (drama.SourceRange, error)
	ListBatches(context.Context, string, string, string) ([]drama.Batch, error)
	SuggestBatch(context.Context, string, string, string, drama.BatchSelection) (drama.BatchSuggestion, error)
	CreateBatch(context.Context, string, string, string, drama.BatchSelection) (drama.Batch, error)
	UpdateBatchRange(context.Context, string, string, string, string, drama.BatchRangeChange) (drama.Batch, error)
	UpdateOverview(context.Context, string, string, string, json.RawMessage) (drama.Project, error)
	Analyze(drama.Project) error
}

func registerDramaRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-drama", requireSession(deps))
	group.GET("/models", func(ctx *gin.Context) {
		session := currentSession(ctx)
		items, err := deps.Drama.AvailableModels(ctx.Request.Context(), session.User.OrganizationID, session.User.ID)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.GET("/settings", func(ctx *gin.Context) {
		session := currentSession(ctx)
		settings, err := deps.Drama.GetSettings(ctx.Request.Context(), session.User.OrganizationID, session.User.ID)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, settings)
	})
	group.PUT("/settings", requireCSRF(deps), func(ctx *gin.Context) {
		var settings drama.Settings
		if err := ctx.ShouldBindJSON(&settings); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "模型设置格式无效")
			return
		}
		session := currentSession(ctx)
		settings.OrganizationID, settings.UserID = session.User.OrganizationID, session.User.ID
		saved, err := deps.Drama.SaveSettings(ctx.Request.Context(), settings)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, saved)
	})
	group.GET("/projects", func(ctx *gin.Context) {
		session := currentSession(ctx)
		items, err := deps.Drama.List(ctx.Request.Context(), session.User.OrganizationID, session.User.ID)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.POST("/projects", requireCSRF(deps), func(ctx *gin.Context) {
		var input drama.CreateInput
		if err := ctx.ShouldBindJSON(&input); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "请填写有效的剧本内容和项目配置")
			return
		}
		session := currentSession(ctx)
		input.OrganizationID, input.OwnerID = session.User.OrganizationID, session.User.ID
		project, err := deps.Drama.Create(ctx.Request.Context(), input)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		go func() {
			if err := deps.Drama.Analyze(project); err != nil {
				deps.Logger.Error("AI 漫剧解析失败", "project_id", project.ID, "error", err)
			}
		}()
		httpx.OK(ctx, http.StatusCreated, project)
	})
	group.GET("/projects/:id", func(ctx *gin.Context) {
		session := currentSession(ctx)
		project, err := deps.Drama.Get(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"))
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, project)
	})
	group.POST("/projects/:id/retry", requireCSRF(deps), func(ctx *gin.Context) {
		session := currentSession(ctx)
		project, err := deps.Drama.Retry(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"))
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		go func() {
			if err := deps.Drama.Analyze(project); err != nil {
				deps.Logger.Error("AI 漫剧重新解析失败", "project_id", project.ID, "error", err)
			}
		}()
		httpx.OK(ctx, http.StatusAccepted, project)
	})
	group.POST("/projects/:id/reanalyze-overview", requireCSRF(deps), func(ctx *gin.Context) {
		session := currentSession(ctx)
		project, err := deps.Drama.ReanalyzeOverview(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"))
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		go func() {
			if err := deps.Drama.Analyze(project); err != nil {
				deps.Logger.Error("AI 漫剧概览重新解析失败", "project_id", project.ID, "error", err)
			}
		}()
		httpx.OK(ctx, http.StatusAccepted, project)
	})
	group.GET("/projects/:id/source", func(ctx *gin.Context) {
		session := currentSession(ctx)
		source, err := deps.Drama.GetSource(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"))
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "private, no-store")
		httpx.OK(ctx, http.StatusOK, gin.H{"sourceText": source})
	})
	group.GET("/projects/:id/source/range", func(ctx *gin.Context) {
		start, err := strconv.Atoi(ctx.DefaultQuery("start", "0"))
		if err != nil {
			handleDramaError(ctx, deps, drama.ErrInvalidInput)
			return
		}
		limit, err := strconv.Atoi(ctx.DefaultQuery("limit", "12000"))
		if err != nil {
			handleDramaError(ctx, deps, drama.ErrInvalidInput)
			return
		}
		session := currentSession(ctx)
		page, err := deps.Drama.SourcePage(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"), start, limit)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "private, no-store")
		httpx.OK(ctx, http.StatusOK, page)
	})
	group.GET("/projects/:id/batches", func(ctx *gin.Context) {
		session := currentSession(ctx)
		items, err := deps.Drama.ListBatches(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"))
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.GET("/projects/:id/batches/suggest", func(ctx *gin.Context) {
		input := drama.BatchSelection{Mode: ctx.Query("mode"), PreviousBatchID: ctx.Query("previousBatchId")}
		if raw, ok := ctx.GetQuery("startOffset"); ok {
			start, err := strconv.Atoi(raw)
			if err != nil {
				handleDramaError(ctx, deps, drama.ErrInvalidInput)
				return
			}
			input.StartOffset = &start
		}
		session := currentSession(ctx)
		result, err := deps.Drama.SuggestBatch(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"), input)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, result)
	})
	group.POST("/projects/:id/batches", requireCSRF(deps), func(ctx *gin.Context) {
		var input drama.BatchSelection
		if err := ctx.ShouldBindJSON(&input); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "批次范围格式无效")
			return
		}
		session := currentSession(ctx)
		item, err := deps.Drama.CreateBatch(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"), input)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, item)
	})
	group.PATCH("/projects/:id/batches/:batchId/range", requireCSRF(deps), func(ctx *gin.Context) {
		var input drama.BatchRangeChange
		if err := ctx.ShouldBindJSON(&input); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "批次范围格式无效")
			return
		}
		session := currentSession(ctx)
		item, err := deps.Drama.UpdateBatchRange(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"), ctx.Param("batchId"), input)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	group.PUT("/projects/:id/overview", requireCSRF(deps), func(ctx *gin.Context) {
		var overview json.RawMessage
		if err := ctx.ShouldBindJSON(&overview); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "项目概览格式无效")
			return
		}
		session := currentSession(ctx)
		project, err := deps.Drama.UpdateOverview(ctx.Request.Context(), session.User.OrganizationID, session.User.ID, ctx.Param("id"), overview)
		if err != nil {
			handleDramaError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, project)
	})
}

func handleDramaError(ctx *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, drama.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_drama_project", err.Error())
	case errors.Is(err, drama.ErrModelUnavailable):
		httpx.Fail(ctx, http.StatusBadRequest, "drama_model_unavailable", err.Error())
	case errors.Is(err, drama.ErrBatchNotReady), errors.Is(err, drama.ErrBatchConflict):
		httpx.Fail(ctx, http.StatusConflict, "drama_batch_conflict", err.Error())
	case errors.Is(err, drama.ErrRetryNotAllowed):
		httpx.Fail(ctx, http.StatusConflict, "drama_retry_not_allowed", err.Error())
	case errors.Is(err, drama.ErrNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "drama_project_not_found", err.Error())
	case errors.Is(err, drama.ErrModelNotConfigured):
		httpx.Fail(ctx, http.StatusConflict, "drama_model_not_configured", err.Error())
	default:
		deps.Logger.Error("AI 漫剧请求失败", "error", err)
		httpx.Fail(ctx, http.StatusInternalServerError, "drama_failed", "AI 漫剧请求失败，请稍后重试")
	}
}
