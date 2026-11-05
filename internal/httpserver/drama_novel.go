package httpserver

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

func createDramaFromNovel(ctx *gin.Context, deps Dependencies) {
	var input struct {
		NovelID      string `json:"novelId" binding:"required"`
		StartChapter int    `json:"startChapter" binding:"required"`
		EndChapter   int    `json:"endChapter" binding:"required"`
		Title        string `json:"title"`
		StyleID      string `json:"styleId"`
		Style        string `json:"style"`
		AspectRatio  string `json:"aspectRatio"`
	}
	if !bindNovelInput(ctx, &input) {
		return
	}
	p, err := deps.Novel.Get(ctx.Request.Context(), novelOwner(ctx), input.NovelID)
	if err != nil {
		writeNovelResponse(ctx, deps, http.StatusOK, nil, err)
		return
	}
	text, err := p.ChapterSnapshot(input.StartChapter, input.EndChapter)
	if err != nil {
		writeNovelResponse(ctx, deps, http.StatusOK, nil, err)
		return
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = p.Title
	}
	sourceName := fmt.Sprintf("《%s》第 %d 章", p.Title, input.StartChapter)
	if input.StartChapter != input.EndChapter {
		sourceName = fmt.Sprintf("《%s》第 %d 至 %d 章", p.Title, input.StartChapter, input.EndChapter)
	}
	createAndAnalyzeDramaProject(ctx, deps, drama.CreateInput{
		Title: title, SourceName: sourceName, SourceText: text, SourceMode: "upload",
		StyleID: input.StyleID, Style: input.Style, AspectRatio: input.AspectRatio,
	})
}

func createAndAnalyzeDramaProject(ctx *gin.Context, deps Dependencies, input drama.CreateInput) {
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
}
