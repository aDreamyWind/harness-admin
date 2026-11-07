package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
	"harness-admin/internal/novel"
)

type NovelService interface {
	List(context.Context, novel.Owner) ([]novel.Book, error)
	Models(context.Context, novel.Owner) ([]novel.Model, error)
	RefineIdea(context.Context, novel.Owner, novel.RefineIdeaInput) (novel.RefineIdeaResult, error)
	Create(context.Context, novel.Owner, novel.CreateInput) (novel.Project, error)
	Get(context.Context, novel.Owner, string) (novel.Project, error)
	PlanChapters(context.Context, novel.Owner, string, novel.ChapterPlanInput) (novel.ChapterPlan, error)
	Apply(context.Context, novel.Owner, string, novel.Action) (novel.Project, error)
	CoverModels(context.Context, novel.Owner) ([]novel.Model, error)
	CoverContent(context.Context, novel.Owner, string, string) (novel.CoverImage, error)
	ListCoverHistory(context.Context, novel.Owner, string) ([]novel.CoverHistoryItem, error)
	ApplyCoverHistory(context.Context, novel.Owner, string, string) (novel.Cover, error)
	UploadCover(context.Context, novel.Owner, string, []byte) (novel.Cover, error)
	GenerateCover(context.Context, novel.Owner, string, novel.GenerateCoverInput) (string, error)
	StartCoverGeneration(context.Context, novel.Owner, string, novel.GenerateCoverInput) (novel.CoverGeneration, error)
	GetCoverGeneration(context.Context, novel.Owner, string) (novel.CoverGeneration, error)
	CoverPreviewContent(context.Context, novel.Owner, string, string) (novel.CoverImage, error)
	ApplyCoverPreview(context.Context, novel.Owner, string, string) (novel.Cover, error)
}

func registerNovelRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/ai-novel", requireSession(deps))
	registerNovelCoverRoutes(group, deps)
	group.GET("/models", func(c *gin.Context) {
		items, err := deps.Novel.Models(c.Request.Context(), novelOwner(c))
		writeNovelResponse(c, deps, http.StatusOK, items, err)
	})
	group.POST("/ideas/refine", requireCSRF(deps), func(c *gin.Context) {
		var input novel.RefineIdeaInput
		if !bindNovelInput(c, &input) {
			return
		}
		result, err := deps.Novel.RefineIdea(c.Request.Context(), novelOwner(c), input)
		writeNovelResponse(c, deps, http.StatusOK, result, err)
	})
	group.GET("/projects", func(c *gin.Context) {
		items, err := deps.Novel.List(c.Request.Context(), novelOwner(c))
		for i := range items {
			items[i].Cover = novelCoverWithURL(items[i].ID, items[i].Cover)
		}
		writeNovelResponse(c, deps, http.StatusOK, items, err)
	})
	group.POST("/projects", requireCSRF(deps), func(c *gin.Context) {
		var input novel.CreateInput
		if !bindNovelInput(c, &input) {
			return
		}
		p, err := deps.Novel.Create(c.Request.Context(), novelOwner(c), input)
		writeNovelResponse(c, deps, http.StatusCreated, p, err)
	})
	group.GET("/projects/:id", func(c *gin.Context) {
		p, err := deps.Novel.Get(c.Request.Context(), novelOwner(c), c.Param("id"))
		p.Cover = novelCoverWithURL(p.ID, p.Cover)
		writeNovelResponse(c, deps, http.StatusOK, p, err)
	})
	group.GET("/projects/:id/finalized-chapters", func(c *gin.Context) {
		p, err := deps.Novel.Get(c.Request.Context(), novelOwner(c), c.Param("id"))
		if err != nil {
			writeNovelResponse(c, deps, http.StatusOK, nil, err)
			return
		}
		writeNovelResponse(c, deps, http.StatusOK, p.FinalizedChapters(), nil)
	})
	group.POST("/projects/:id/chapter-plan", requireCSRF(deps), func(c *gin.Context) {
		var input novel.ChapterPlanInput
		if !bindNovelInput(c, &input) {
			return
		}
		result, err := deps.Novel.PlanChapters(c.Request.Context(), novelOwner(c), c.Param("id"), input)
		writeNovelResponse(c, deps, http.StatusOK, result, err)
	})
	group.POST("/projects/:id/actions", requireCSRF(deps), func(c *gin.Context) {
		var input novel.Action
		// 长篇连载撤回确认后可提交完整的 500 章大纲。
		if !bindNovelInputUpTo(c, &input, 4<<20) {
			return
		}
		p, err := deps.Novel.Apply(c.Request.Context(), novelOwner(c), c.Param("id"), input)
		p.Cover = novelCoverWithURL(p.ID, p.Cover)
		writeNovelResponse(c, deps, http.StatusOK, p, err)
	})
}

func bindNovelInput(c *gin.Context, input any) bool {
	return bindNovelInputUpTo(c, input, 512<<10)
}

func bindNovelInputUpTo(c *gin.Context, input any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	if err := c.ShouldBindJSON(input); err != nil {
		httpx.Fail(c, http.StatusBadRequest, "invalid_novel_input", "小说请求格式无效或内容过长")
		return false
	}
	return true
}

func novelOwner(c *gin.Context) novel.Owner {
	session := currentSession(c)
	return novel.Owner{OrganizationID: session.User.OrganizationID, UserID: session.User.ID}
}

func writeNovelResponse(c *gin.Context, deps Dependencies, success int, data any, err error) {
	switch {
	case err == nil:
		httpx.OK(c, success, data)
	case errors.Is(err, novel.ErrInvalid), errors.Is(err, novel.ErrModel), errors.Is(err, novel.ErrCoverModel), errors.Is(err, novel.ErrCoverStyle), errors.Is(err, novel.ErrCoverImage):
		httpx.Fail(c, http.StatusBadRequest, "invalid_novel_input", err.Error())
	case errors.Is(err, novel.ErrNotFound):
		httpx.Fail(c, http.StatusNotFound, "novel_not_found", novel.ErrNotFound.Error())
	case errors.Is(err, novel.ErrCoverPreviewExpired):
		httpx.Fail(c, http.StatusNotFound, "novel_cover_preview_expired", novel.ErrCoverPreviewExpired.Error())
	case errors.Is(err, novel.ErrCoverConflict):
		httpx.Fail(c, http.StatusConflict, "novel_cover_conflict", novel.ErrCoverConflict.Error())
	case errors.Is(err, novel.ErrConflict):
		httpx.Fail(c, http.StatusConflict, "novel_conflict", err.Error())
	case errors.Is(err, novel.ErrState):
		httpx.Fail(c, http.StatusConflict, "novel_state_conflict", err.Error())
	case errors.Is(err, novel.ErrIdeaRepetition):
		httpx.Fail(c, http.StatusBadGateway, "novel_generation_failed", novel.ErrIdeaRepetition.Error())
	case errors.Is(err, novel.ErrGeneration):
		deps.Logger.Warn("AI 小说模型调用失败", "request_id", contextString(c, "request_id"), "error", err)
		message := novel.ErrGeneration.Error()
		var countError *novel.SerialOpeningCountError
		var draftError *novel.DraftOutputError
		if errors.As(err, &countError) {
			message = countError.Error()
		} else if errors.As(err, &draftError) {
			message = draftError.Error()
		}
		httpx.Fail(c, http.StatusBadGateway, "novel_generation_failed", message)
	case errors.Is(err, novel.ErrCoverGeneration):
		deps.Logger.Warn("AI 小说封面生成失败", "error", err)
		httpx.Fail(c, http.StatusBadGateway, "novel_cover_generation_failed", novel.ErrCoverGeneration.Error())
	default:
		deps.Logger.Error("AI 小说请求失败", "error", err)
		httpx.Fail(c, http.StatusInternalServerError, "novel_failed", "AI 小说请求失败，请稍后重试")
	}
}
