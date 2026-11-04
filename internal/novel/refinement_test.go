package novel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNovelRefineIdeaReturnsPreviewWithoutProject(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	writer := &testWriter{result: `{"brief":"  守塔人必须在天亮前找出午夜呼救的来历。  "}`}
	service := NewService(nil, writer, nil, nil)

	result, err := service.RefineIdea(ctx, owner, RefineIdeaInput{
		Brief: " 守塔人听到呼救 ", Instruction: " 加入天亮前的时限 ", ModelCode: "writer",
	})
	if err != nil || result.Brief != "守塔人必须在天亮前找出午夜呼救的来历。" {
		t.Fatalf("灵感优化预览结果错误: %+v, %v", result, err)
	}
	for _, want := range []string{`"Brief":"守塔人听到呼救"`, `"Instruction":"加入天亮前的时限"`} {
		if !strings.Contains(writer.prompt, want) {
			t.Errorf("模型提示缺少创作资料 %s", want)
		}
	}

	writer.prompt = ""
	_, err = service.RefineIdea(ctx, owner, RefineIdeaInput{Brief: "守塔人听到呼救", Instruction: strings.Repeat("改", 1201), ModelCode: "writer"})
	if !errors.Is(err, ErrInvalid) || writer.prompt != "" {
		t.Fatalf("优化要求超长仍调用模型: %v", err)
	}
	writer.result = `{"brief":"   "}`
	_, err = service.RefineIdea(ctx, owner, RefineIdeaInput{Brief: "守塔人听到呼救", ModelCode: "writer"})
	if !errors.Is(err, ErrGeneration) {
		t.Fatalf("接受了空预览结果: %v", err)
	}
}

func TestNovelOutlineRevisionUsesCurrentDraftAndFeedback(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo := &memoryRepository{}
	writer := &testWriter{
		result: `{"synopsis":"追查呼救","world":"晚唐海港","characters":"林静守塔","beats":[{"title":"夜潮","goal":"发现呼救"}]}`,
	}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "守塔人听到呼救", ChapterCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	action := Action{Kind: "generate_outline", Version: p.Version, ModelCode: "writer", Instruction: "使用晚唐背景"}
	_, err = service.Apply(ctx, owner, p.ID, action)
	if !errors.Is(err, ErrInvalid) || writer.prompt != "" {
		t.Fatalf("缺少当前稿时仍调用模型: %v", err)
	}

	saved := &Outline{Synopsis: "原始梗概", World: "已保存设定", Characters: "林静守塔", Beats: []Beat{{Title: "夜潮", Goal: "发现呼救"}}}
	p, err = service.Apply(ctx, owner, p.ID, Action{Kind: "save_outline", Version: p.Version, Outline: saved})
	if err != nil {
		t.Fatal(err)
	}
	action.Version = p.Version
	p, err = service.Apply(ctx, owner, p.ID, action)
	if err != nil || !strings.Contains(writer.prompt, "已保存设定") {
		t.Fatalf("省略当前稿时未使用已保存大纲: %v", err)
	}

	current := &Outline{Synopsis: " 原始梗概 ", World: "未保存设定", Characters: "林静守塔", Beats: []Beat{{Title: " 夜潮 ", Goal: "新手写目标"}}}
	action.Version = p.Version
	action.Outline = current
	p, err = service.Apply(ctx, owner, p.ID, action)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"守塔人听到呼救", "使用晚唐背景", "未保存设定", "新手写目标"} {
		if !strings.Contains(writer.prompt, want) {
			t.Errorf("定向修订缺少创作资料 %q", want)
		}
	}
	if strings.Contains(writer.prompt, `"CurrentOutline":{"synopsis":"追查呼救"`) {
		t.Error("定向修订未使用传入的当前稿")
	}
	if p.Outline.World != "晚唐海港" || p.Confirmed {
		t.Fatalf("未保存待确认的修订稿: %+v", p.Outline)
	}
	if current.Synopsis != " 原始梗概 " || current.Beats[0].Title != " 夜潮 " {
		t.Fatal("生成过程中修改了传入的当前稿")
	}

	writer.prompt = ""
	action.Instruction = ""
	action.Version = p.Version
	_, err = service.Apply(ctx, owner, p.ID, action)
	if err != nil || strings.Contains(writer.prompt, "CurrentOutline") || !strings.Contains(writer.prompt, "守塔人听到呼救") {
		t.Fatalf("无修改意见时偏离原生成流程: %v", err)
	}
}
