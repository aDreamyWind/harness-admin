package novel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type memoryRepository struct {
	owner      Owner
	project    Project
	coverImage CoverImage
}

func (r *memoryRepository) List(_ context.Context, owner Owner) ([]Book, error) {
	if owner != r.owner {
		return []Book{}, nil
	}
	return []Book{r.project.Book}, nil
}

func (r *memoryRepository) Create(_ context.Context, owner Owner, p Project) (Project, error) {
	r.owner, r.project = owner, p
	return p, nil
}

func (r *memoryRepository) Get(_ context.Context, owner Owner, id string) (Project, error) {
	if owner != r.owner || id != r.project.ID {
		return Project{}, ErrNotFound
	}
	p := r.project
	p.Chapters = append([]Chapter(nil), p.Chapters...)
	return p, nil
}

func (r *memoryRepository) Save(_ context.Context, owner Owner, p Project) (Project, error) {
	if r.owner != owner || r.project.ID != p.ID || r.project.Version != p.Version {
		return Project{}, ErrConflict
	}
	p.Version++
	p.Cover = r.project.Cover
	r.project = p
	return p, nil
}

func (r *memoryRepository) GetCover(_ context.Context, owner Owner, id, revision string) (CoverImage, error) {
	if r.owner != owner || r.project.ID != id || r.project.Cover == nil ||
		(revision != "" && revision != r.project.Cover.Revision) {
		return CoverImage{}, ErrNotFound
	}
	return r.coverImage, nil
}

func (r *memoryRepository) SaveCover(_ context.Context, owner Owner, id, previousRevision string, cover Cover, image CoverImage) (Cover, error) {
	if r.owner != owner || r.project.ID != id {
		return Cover{}, ErrNotFound
	}
	currentRevision := ""
	if r.project.Cover != nil {
		currentRevision = r.project.Cover.Revision
	}
	if currentRevision != previousRevision {
		return Cover{}, ErrConflict
	}
	r.project.Cover, r.coverImage = &cover, image
	return cover, nil
}

type testWriter struct {
	result       string
	prompt       string
	beforeReturn func()
}

func (w *testWriter) Models(context.Context, Owner) ([]Model, error) {
	return []Model{{Code: "writer", Name: "测试文本模型"}}, nil
}

func (w *testWriter) Complete(_ context.Context, _ Owner, _, prompt string) (string, error) {
	w.prompt = prompt
	if w.beforeReturn != nil {
		w.beforeReturn()
	}
	return w.result, nil
}

func TestNovelWorkflowUsesOnlyConfirmedContinuity(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: " 潮汐档案 ", Brief: "守塔人听到不存在的船只呼救", ChapterCount: 2})
	if err != nil || p.Title != "潮汐档案" || len(p.Chapters) != 2 {
		t.Fatalf("创建作品失败: %+v %v", p, err)
	}
	if _, err := service.Get(ctx, Owner{OrganizationID: "other", UserID: owner.UserID}, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨组织读取未拒绝: %v", err)
	}
	if models, err := service.Models(ctx, owner); err != nil || len(models) != 1 {
		t.Fatalf("读取模型失败: %+v %v", models, err)
	}
	if books, err := service.List(ctx, owner); err != nil || len(books) != 1 {
		t.Fatalf("读取作品列表失败: %+v %v", books, err)
	}
	run := func(action Action, want error) {
		t.Helper()
		action.Version = p.Version
		next, err := service.Apply(ctx, owner, p.ID, action)
		if !errors.Is(err, want) {
			t.Fatalf("%s 返回 %v，预期 %v", action.Kind, err, want)
		}
		if want == nil {
			p = next
		}
	}

	writer.result = "{"
	run(Action{Kind: "generate_outline", ModelCode: "writer"}, ErrGeneration)
	if repo.project.Outline != nil || repo.project.Version != p.Version {
		t.Fatal("无效的模型输出污染了作品")
	}
	outline := &Outline{Synopsis: "追查呼救", World: "孤岛", Characters: "林静守塔", Beats: []Beat{{Title: "雾笛", Goal: "发现异常信号"}, {Title: "回音", Goal: "验证呼救的来源"}}}
	raw, err := json.Marshal(map[string]any{
		"synopsis": outline.Synopsis, "world": outline.World,
		"characters": []map[string]string{{"name": "林静", "role": "守塔人"}}, "beats": outline.Beats,
	})
	if err != nil {
		t.Fatal(err)
	}
	writer.result = "```json\n" + string(raw) + "\n```"
	run(Action{Kind: "generate_outline", ModelCode: "writer"}, nil)
	if p.Outline.Characters != "姓名：林静\n身份：守塔人" {
		t.Fatalf("大纲人物列表未整理成文本: %q", p.Outline.Characters)
	}
	run(Action{Kind: "write", Chapter: 1, ModelCode: "writer"}, ErrState)
	p.Outline.Beats[0].Goal = "保存后可修改的目标"
	run(Action{Kind: "save_outline", Outline: p.Outline}, nil)
	run(Action{Kind: "confirm_outline", Outline: p.Outline}, nil)
	run(Action{Kind: "save_outline", Outline: outline}, ErrState)
	run(Action{Kind: "write", Chapter: 2, ModelCode: "writer"}, ErrState)
	writer.result = `{"body":"林静走进雾里。","summary":"模型摘要"}`
	run(Action{Kind: "write", Chapter: 1, ModelCode: "writer"}, nil)
	writer.result = `{"review":["请具体描写她听到的声响。", "检查时间。"]}`
	run(Action{Kind: "review", Chapter: 1, ModelCode: "writer"}, nil)
	if p.Chapters[0].Review != "请具体描写她听到的声响。\n\n检查时间。" {
		t.Fatal("未按顺序保存数组形式的审稿意见")
	}
	writer.result = `{"review":["有效片段", 1]}`
	run(Action{Kind: "review", Chapter: 1, ModelCode: "writer"}, ErrGeneration)
	if repo.project.Chapters[0].Review != p.Chapters[0].Review {
		t.Fatal("无效的审稿结果覆盖了已保存的意见")
	}
	run(Action{Kind: "save_draft", Chapter: 1, Body: p.Chapters[0].Body, Summary: "人工更正摘要"}, nil)
	if p.Chapters[0].Review != "" {
		t.Fatal("摘要修改后保留了已过期的审稿意见")
	}
	writer.result = `{"review":"请具体描写她听到的声响。"}`
	run(Action{Kind: "review", Chapter: 1, ModelCode: "writer"}, nil)
	run(Action{Kind: "save_draft", Chapter: 1, Body: "林静听到了两声雾笛。"}, nil)
	if p.Chapters[0].Review != "" {
		t.Fatal("正文修改后保留了已过期的审稿意见")
	}
	run(Action{Kind: "finalize", Chapter: 1, Body: p.Chapters[0].Body}, ErrInvalid)
	run(Action{Kind: "finalize", Chapter: 1, Body: p.Chapters[0].Body, Summary: "林静在灯塔听见两声雾笛，来源未明。"}, nil)
	run(Action{Kind: "write", Chapter: 1, ModelCode: "writer"}, ErrState)
	writer.result = `{"body":"她循声下楼。","summary":"林静离开灯塔。"}`
	run(Action{Kind: "write", Chapter: 2, ModelCode: "writer"}, nil)
	for _, expected := range []string{"来源未明", "林静听到了两声雾笛", "验证呼救的来源"} {
		if !strings.Contains(writer.prompt, expected) {
			t.Errorf("第二章缺少上下文 %q", expected)
		}
	}
	if strings.Contains(writer.prompt, "模型摘要") {
		t.Error("第二章读取了已替换的摘要")
	}
}

func TestNovelGenerationCannotOverwriteConcurrentChange(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "灯塔", ChapterCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	writer.result = `{"synopsis":"查明声源","world":"孤岛","characters":"守塔人","beats":[{"title":"雾笛","goal":"发现异样"}]}`
	writer.beforeReturn = func() { repo.project.Version++ }
	if _, err := service.Apply(ctx, owner, p.ID, Action{Kind: "generate_outline", ModelCode: "writer", Version: p.Version}); !errors.Is(err, ErrConflict) {
		t.Fatalf("预期拒绝并发覆盖: %v", err)
	}
	if repo.project.Outline != nil {
		t.Fatal("生成结果覆盖了更新后的作品")
	}
}

func TestNovelIdeaModeGeneratesTitleWithOutline(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	input := CreateInput{Brief: "守塔人听到呼救", ChapterCount: 1}
	if _, err := service.Create(ctx, owner, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("自行规划时仍需提供书名，实际错误: %v", err)
	}
	input.SuggestTitle = true
	project, err := service.Create(ctx, owner, input)
	if err != nil || project.Title != "未命名小说" || !project.NeedsTitle || project.Outline != nil {
		t.Fatalf("未成功从灵感创建作品: %+v, %v", project, err)
	}
	outline := `"synopsis":"查明声源","world":"孤岛","characters":"守塔人","beats":[{"title":"雾笛","goal":"发现异样"}]`
	for _, prefix := range []string{"", `"title":[],`, `"title":"未命名小说",`} {
		writer.result = `{` + prefix + outline + `}`
		project, err = service.Apply(ctx, owner, project.ID, Action{Kind: "generate_outline", Version: project.Version, ModelCode: "writer"})
		if err != nil || project.Title != "未命名小说" || !project.NeedsTitle || project.Outline == nil {
			t.Fatalf("缺少有效书名时仍应保存大纲: %+v, %v", project, err)
		}
	}
	if !strings.Contains(writer.prompt, "同时拟定一个贴合故事的简体中文书名") {
		t.Fatal("待命名作品的生成请求缺少命名指令")
	}
	writer.result = `{"title":"  逆潮灯语  ",` + outline + `}`
	project, err = service.Apply(ctx, owner, project.ID, Action{Kind: "generate_outline", Version: project.Version, ModelCode: "writer"})
	if err != nil || project.Title != "逆潮灯语" || project.NeedsTitle || project.Confirmed {
		t.Fatalf("未将书名与待确认大纲一并保存: %+v, %v", project, err)
	}
	books, err := service.List(ctx, owner)
	if err != nil || len(books) != 1 || books[0].Title != project.Title {
		t.Fatalf("列表未反映生成后的书名: %+v, %v", books, err)
	}

	input.Title = "未命名小说"
	manual, err := service.Create(ctx, owner, input)
	if err != nil || manual.NeedsTitle {
		t.Fatalf("作者已填写的书名被标记为待命名: %+v, %v", manual, err)
	}
	manual, err = service.Apply(ctx, owner, manual.ID, Action{Kind: "generate_outline", Version: manual.Version, ModelCode: "writer"})
	if err != nil || manual.Title != input.Title || strings.Contains(writer.prompt, "同时拟定一个贴合故事的简体中文书名") {
		t.Fatalf("生成大纲时覆盖了作者已填写的书名: %+v, %v", manual, err)
	}
}
