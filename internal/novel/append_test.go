package novel

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNovelAppendChaptersKeepsOriginalWork(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo := &memoryRepository{}
	writer := &testWriter{result: `{"beats":[{"title":" 新航线 ","goal":"追踪旧航海图留下的坐标"}]}`}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "孤岛灯塔的信号", ChapterCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	input := ChapterPlanInput{Version: p.Version, Count: 1, ModelCode: "writer", Instruction: "由海图上的坐标引出新航线"}
	if _, err := service.PlanChapters(ctx, owner, p.ID, input); !errors.Is(err, ErrState) {
		t.Fatalf("未确认的大纲允许追加预览: %v", err)
	}
	outline := &Outline{Synopsis: "寻找信号来源", World: "孤岛", Characters: "守塔人",
		Beats: []Beat{{Title: "夜雾", Goal: "发现信号"}, {Title: "海图", Goal: "找到航海图"}}}
	p, err = service.Apply(ctx, owner, p.ID, Action{Kind: "confirm_outline", Version: p.Version, Outline: outline})
	if err != nil {
		t.Fatal(err)
	}
	p, err = service.Apply(ctx, owner, p.ID, Action{Kind: "finalize", Version: p.Version, Chapter: 1,
		Body: "守塔人在夜雾中发现信号。", Summary: "午夜时分首次收到求救信号。"})
	if err != nil {
		t.Fatal(err)
	}
	p, err = service.Apply(ctx, owner, p.ID, Action{Kind: "save_draft", Version: p.Version, Chapter: 2,
		Body: "守塔人取出航海图。", Summary: "找到航海图。"})
	if err != nil {
		t.Fatal(err)
	}
	input.Version = p.Version
	plan, err := service.PlanChapters(ctx, owner, p.ID, input)
	if err != nil || len(plan.Beats) != 1 || plan.Beats[0].Title != "新航线" {
		t.Fatalf("生成预览失败: %+v, %v", plan, err)
	}
	for _, value := range []string{"从第 3 章开始", "午夜时分首次收到求救信号", "找到航海图", input.Instruction} {
		if !strings.Contains(writer.prompt, value) {
			t.Fatalf("续章上下文缺少 %q", value)
		}
	}
	if repo.project.Version != p.Version || repo.project.ChapterCount != 2 {
		t.Fatalf("生成预览改动了作品: %d 章，版本 %d", repo.project.ChapterCount, repo.project.Version)
	}
	plan.Beats[0].Title = "远洋航线"
	updated, err := service.Apply(ctx, owner, p.ID, Action{Kind: "append_chapters", Version: p.Version, Beats: plan.Beats})
	if err != nil || updated.ChapterCount != 3 || updated.Version != p.Version+1 || !updated.Confirmed {
		t.Fatalf("保存追加失败: %+v, %v", updated, err)
	}
	if !reflect.DeepEqual(updated.Chapters[:2], p.Chapters) || updated.Chapters[2] != (Chapter{}) ||
		!reflect.DeepEqual(updated.Outline.Beats[:2], outline.Beats) || updated.Outline.Beats[2].Title != "远洋航线" {
		t.Fatalf("已有章节被改变或新增计划不符: %+v", updated)
	}
	if _, err := service.Apply(ctx, owner, p.ID, Action{Kind: "append_chapters", Version: p.Version, Beats: plan.Beats}); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期确认未拒绝: %v", err)
	}
	if _, err := service.Apply(ctx, owner, p.ID, Action{Kind: "save_draft", Version: updated.Version, Chapter: 3, Body: "远航"}); !errors.Is(err, ErrState) {
		t.Fatalf("追加章节应遵守顺序解锁: %v", err)
	}
}

func TestNovelAppendChaptersEnforcesBatchAndTotalLimits(t *testing.T) {
	p := Project{Book: Book{ChapterCount: MaxChapters - 1}, Workspace: Workspace{
		Confirmed: true, Chapters: make([]Chapter, MaxChapters-1), Outline: &Outline{Beats: make([]Beat, MaxChapters-1)},
	}}
	if err := appendChapters(&p, []Beat{{Title: "最后一章", Goal: "收束冲突"}}); err != nil || p.ChapterCount != MaxChapters {
		t.Fatalf("无法追加到累计上限: %d, %v", p.ChapterCount, err)
	}
	if err := appendChapters(&p, []Beat{{Title: "超额", Goal: "增加情节"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("超过累计上限未拒绝: %v", err)
	}
	p = Project{Book: Book{ChapterCount: 1}, Workspace: Workspace{
		Confirmed: true, Chapters: make([]Chapter, 1), Outline: &Outline{Beats: make([]Beat, 1)},
	}}
	if err := validateAppend(&p, MaxAppendChapters+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("超过单批上限未拒绝: %v", err)
	}
}
