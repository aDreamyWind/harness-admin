package novel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNovelAuthorGuidancePersistsAndAppliesToContinuation(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	writer := &testWriter{result: `{"beats":[{"title":"夜访","goal":"找到失物"}]}`}
	const id = "c349790e-8d51-4313-aa18-41e194572feb"
	finalized := Chapter{Body: "他向哥哥告别。", Summary: "他离开宫门。", Final: true}
	repo := &memoryRepository{owner: owner, project: Project{
		Book: Book{ID: id, ChapterCount: 1}, Version: 3,
		Workspace: Workspace{
			Confirmed: true, Chapters: []Chapter{finalized},
			Outline: &Outline{Beats: []Beat{{Title: "启程", Goal: "离开宫门"}}},
		},
	}}
	service := NewService(repo, writer, nil, nil)
	action := Action{Kind: "save_author_guidance", Version: 3}
	if _, err := service.Apply(ctx, owner, id, action); !errors.Is(err, ErrInvalid) {
		t.Fatalf("缺少写作约定字段应拒绝更新: %v", err)
	}
	tooLong := strings.Repeat("字", maxAuthorGuidanceRunes+1)
	action.AuthorGuidance = &tooLong
	if _, err := service.Apply(ctx, owner, id, action); !errors.Is(err, ErrInvalid) {
		t.Fatalf("过长的约定应拒绝更新: %v", err)
	}
	guidance := "  人物对太子统一称「皇兄」，不用「哥」。  "
	action.AuthorGuidance = &guidance
	updated, err := service.Apply(ctx, owner, id, action)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AuthorGuidance != strings.TrimSpace(guidance) || updated.Chapters[0] != finalized || updated.Version != 4 {
		t.Fatalf("保存约定应保留已有定稿: %+v", updated)
	}
	if _, err := service.Apply(ctx, owner, id, action); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期版本不能覆盖约定: %v", err)
	}
	plan := ChapterPlanInput{Version: updated.Version, Count: 1, ModelCode: "writer"}
	if _, err := service.PlanChapters(ctx, owner, id, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(writer.prompt, `"AuthorGuidance":"`+strings.TrimSpace(guidance)+`"`) ||
		!strings.Contains(writer.prompt, finalized.Summary) {
		t.Fatal("续章规划应同时使用当前写作约定和已定稿事实")
	}
	empty := ""
	action.Version, action.AuthorGuidance = updated.Version, &empty
	cleared, err := service.Apply(ctx, owner, id, action)
	if err != nil || cleared.AuthorGuidance != "" || cleared.Version != 5 {
		t.Fatalf("显式提交空文本应清空约定: %+v %v", cleared, err)
	}
}
