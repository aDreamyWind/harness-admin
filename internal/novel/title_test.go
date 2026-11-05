package novel

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNovelRenamePreservesConfirmedWork(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	original := Project{
		Book:  Book{ID: "c349790e-8d51-4313-aa18-41e194572feb", Title: "潮汐档案", ChapterCount: 1, FinalizedChapterCount: 1},
		Brief: "守塔人听到呼救", Version: 7,
		Workspace: Workspace{
			Confirmed: true, ModelCode: "原创作模型",
			Outline: &Outline{Synopsis: "一封遗失的求救信引出海难真相", World: "孤岛", Characters: "守塔人",
				Beats: []Beat{{Title: "雾笛", Goal: "发现异样"}}},
			Chapters: []Chapter{{Body: "守塔人听到雾笛。", Summary: "雾笛从北侧传来", Final: true}},
		},
	}
	repo, writer := &memoryRepository{owner: owner, project: original}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	manual := Action{Kind: "rename", Version: original.Version, Title: "  流向孤岛的信  "}
	updated, err := service.Apply(ctx, owner, original.ID, manual)
	if err != nil || updated.Title != "流向孤岛的信" || updated.Version != original.Version+1 ||
		!reflect.DeepEqual(updated.Workspace, original.Workspace) {
		t.Fatalf("手动改名应仅更新书名与版本: %+v, %v", updated, err)
	}
	if writer.prompt != "" {
		t.Fatal("手动改名不应调用模型")
	}
	if _, err := service.Apply(ctx, owner, original.ID, manual); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期版本不应覆盖书名: %v", err)
	}
	suggestion := Action{Kind: "suggest_title", Version: updated.Version, ModelCode: "writer"}
	writer.result = `{"title":"逆潮灯语"}`
	generated, err := service.Apply(ctx, owner, original.ID, suggestion)
	if err != nil || generated.Title != "逆潮灯语" || generated.Version != updated.Version+1 ||
		!reflect.DeepEqual(generated.Workspace, original.Workspace) {
		t.Fatalf("AI 拟题应只保存新书名: %+v, %v", generated, err)
	}
	for _, text := range []string{original.Brief, original.Outline.Synopsis, updated.Title} {
		if !strings.Contains(writer.prompt, text) {
			t.Errorf("AI 拟题缺少作品资料 %q", text)
		}
	}
	suggestion.Version = generated.Version
	same, err := service.Apply(ctx, owner, original.ID, suggestion)
	if err != nil || same.Version != generated.Version || same.Title != generated.Title {
		t.Fatalf("模型返回同名时不应写入作品: %+v, %v", same, err)
	}
	writer.beforeReturn = func() { repo.project.Version++ }
	if _, err := service.Apply(ctx, owner, original.ID, suggestion); !errors.Is(err, ErrConflict) {
		t.Fatalf("拟题返回同名时也应识别并发更新: %v", err)
	}
}

func TestNovelRenameValidatesTitleAndDisablesAutomaticNaming(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	project, err := service.Create(ctx, owner, CreateInput{Brief: "灯塔", ChapterCount: 1, SuggestTitle: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{" ", strings.Repeat("灯", 121)} {
		if _, err := service.Apply(ctx, owner, project.ID, Action{Kind: "rename", Title: title, Version: project.Version}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("无效书名未拒绝: %v", err)
		}
	}
	writer.result = `{"title":["不应接受的数组"]}`
	if _, err := service.Apply(ctx, owner, project.ID, Action{Kind: "suggest_title", ModelCode: "writer", Version: project.Version}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("模型返回无效书名未拒绝: %v", err)
	}
	if repo.project.Version != project.Version || !repo.project.NeedsTitle {
		t.Fatalf("失败的改名不应改动作品: %+v", repo.project)
	}
	named, err := service.Apply(ctx, owner, project.ID, Action{Kind: "rename", Title: project.Title, Version: project.Version})
	if err != nil || named.NeedsTitle || named.Version != project.Version+1 {
		t.Fatalf("作者确认保留当前占位书名后，应取消自动命名: %+v, %v", named, err)
	}
}
