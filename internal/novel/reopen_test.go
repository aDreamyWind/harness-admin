package novel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestReopenNovelOutlineAndChapter(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo := &memoryRepository{}
	service := NewService(repo, &testWriter{}, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "追查雾中的信号", ChapterCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	repo.project.Confirmed = true
	repo.project.Outline = &Outline{Synopsis: "寻找声音", World: "孤岛", Characters: "林静守塔", Beats: []Beat{
		{Title: "雾笛", Goal: "发现异常"}, {Title: "手记", Goal: "找到旧手记"}, {Title: "旧友", Goal: "调查目击者"}, {Title: "回声", Goal: "确认信号"},
	}}
	repo.project.OutlineCheck = &OutlineCheck{Status: OutlineCheckPassed}
	repo.project.Chapters = []Chapter{
		{Body: "林静听到雾笛。", Summary: "林静听到两声雾笛。", Final: true},
		{Body: "林静找到手记。", Summary: "手记由林静保管。", ReviewPassed: true, AuthorInstruction: "保留线索", Final: true},
		{Body: "林静向旧友展示手记。", Summary: "旧友看到了手记。", Review: "上一版前文已核对。", Final: true},
		{Body: "旧友说明信号来历。", Summary: "信号出自废船。", ReviewPassed: true},
	}
	original := slices.Clone(repo.project.Chapters)
	run := func(action Action, want error) {
		t.Helper()
		action.Version = p.Version
		next, err := service.Apply(ctx, owner, p.ID, action)
		if !errors.Is(err, want) {
			t.Fatalf("%s 返回 %v，预期 %v", action.Kind, err, want)
		}
		if err == nil {
			p = next
		}
	}

	run(Action{Kind: "reopen_outline"}, nil)
	if p.Confirmed || p.OutlineCheck == nil || !slices.Equal(p.Chapters, original) {
		t.Fatalf("撤回大纲确认不应改写现有内容: %+v", p.Workspace)
	}
	if _, err := service.Apply(ctx, owner, p.ID, Action{Kind: "reopen_outline", Version: p.Version - 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期请求必须拒绝: %v", err)
	}
	run(Action{Kind: "reopen_chapter", Chapter: 2}, ErrState)
	run(Action{Kind: "reopen_outline"}, ErrState)
	changed := *p.Outline
	changed.World = "设有信号塔的孤岛"
	run(Action{Kind: "confirm_outline", Outline: &changed}, nil)
	if !p.Confirmed || p.Outline.World != changed.World || p.OutlineCheck != nil || p.FinalizedChapterCount != 3 ||
		!slices.Equal(p.Chapters[:3], original[:3]) || p.Chapters[3].ReviewPassed {
		t.Fatalf("修改大纲应保留定稿，并使旧草稿的审稿结果失效: %+v", p)
	}

	run(Action{Kind: "reopen_chapter", Chapter: 2}, nil)
	if p.FinalizedChapterCount != 1 || len(p.FinalizedChapters()) != 1 || p.Chapters[0] != original[0] {
		t.Fatalf("定稿前缀有误: %+v", p.Chapters)
	}
	for i, chapter := range p.Chapters[1:] {
		before := original[i+1]
		if chapter.Final || chapter.Body != before.Body || chapter.Summary != before.Summary || chapter.AuthorInstruction != before.AuthorInstruction {
			t.Fatalf("第 %d 章的正文、摘要和写作要求应保留: %+v", i+2, chapter)
		}
	}
	if !p.Chapters[1].ReviewPassed || p.Chapters[2].Review != "" {
		t.Fatalf("应保留本章审稿并作废后文的旧审稿结果: %+v", p.Chapters)
	}
	if _, err := p.ChapterSnapshot(2, 2); !errors.Is(err, ErrState) {
		t.Fatalf("撤回定稿后不得将该章用于新漫剧解析: %v", err)
	}
	run(Action{Kind: "finalize", Chapter: 3, Body: original[2].Body, Summary: original[2].Summary}, ErrState)
	run(Action{Kind: "finalize", Chapter: 2, Body: "林静找到两本手记。", Summary: "两本手记由林静保管。"}, nil)
	run(Action{Kind: "finalize", Chapter: 3, Body: original[2].Body, Summary: original[2].Summary}, nil)
	snapshot, err := p.ChapterSnapshot(1, 3)
	if err != nil || p.FinalizedChapterCount != 3 || !strings.Contains(snapshot, "林静找到两本手记。") {
		t.Fatalf("重新定稿后应使用修订版本: %q, %v", snapshot, err)
	}
}

func TestReopenedOutlineAIGuidedByFinalizedChapters(t *testing.T) {
	p := Project{Book: Book{Title: "潮汐档案", ChapterCount: 2}, Workspace: Workspace{
		Outline:  &Outline{Synopsis: "寻找声音", World: "孤岛", Characters: "林静守塔", Beats: []Beat{{Title: "雾笛", Goal: "发现异常"}, {Title: "回声", Goal: "确认信号"}}},
		Chapters: []Chapter{{Summary: "林静听到两声雾笛。", Final: true}, {Summary: "尚未定稿的第二章内容。"}},
	}}
	for _, input := range []Action{{}, {Instruction: "补充岛上的地理设定"}} {
		prompt, err := outlineGenerationPrompt(&p, input)
		if err != nil || !strings.Contains(prompt, "林静听到两声雾笛。") || strings.Contains(prompt, "尚未定稿的第二章内容。") {
			t.Fatalf("修改大纲时应区分定稿事实和未定稿内容: %v", err)
		}
	}
}
