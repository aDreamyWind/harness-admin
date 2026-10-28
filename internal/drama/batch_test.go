package drama

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFindBatchEndNeverCrossesLimit(t *testing.T) {
	source := "第1集 开场\n" + strings.Repeat("山", 99000) + "\n第2集 转折\n" + strings.Repeat("海", 2000)
	result := FindBatchEnd(source, 0, len([]rune(source)))
	want := len([]rune("第1集 开场\n")) + 99000 + 1
	if result.EndOffset != want || result.Boundary != "episode" || result.EndOffset > BatchRuneLimit {
		t.Fatalf("应在十万字内最近的下一集开头收尾: %+v, want %d", result, want)
	}
	chapter := "正文\n" + strings.Repeat("山", 98000) + "\n第十五章 续篇\n" + strings.Repeat("海", 3000)
	result = FindBatchEnd(chapter, 0, len([]rune(chapter)))
	if result.Boundary != "chapter" || result.EndOffset > BatchRuneLimit {
		t.Fatalf("章节边界超过上限: %+v", result)
	}
	mixed := "开场\n" + strings.Repeat("山", 22000) + "\n第2集 转折\n" + strings.Repeat("海", 22000) + "\n第3章 伏笔\n" + strings.Repeat("云", 62000)
	result = FindBatchEnd(mixed, 0, len([]rune(mixed)))
	if result.Boundary != "chapter" || result.EndOffset != len([]rune("开场\n"+strings.Repeat("山", 22000)+"\n第2集 转折\n"+strings.Repeat("海", 22000)+"\n")) {
		t.Fatalf("十万字内同时存在分集和章节时，应选更接近上限的标题: %+v", result)
	}
	decorated := "===第一章 起始===\n" + strings.Repeat("山", 98900) + "\n===第二十章 贻孝大方===\n" + strings.Repeat("海", 3000)
	result = FindBatchEnd(decorated, 0, len([]rune(decorated)))
	if result.Boundary != "chapter" || result.EndOffset != len([]rune("===第一章 起始===\n"+strings.Repeat("山", 98900)+"\n")) {
		t.Fatalf("带装饰符的章节标题应作为终点: %+v", result)
	}
	result = FindBatchEnd(strings.Repeat("山", 101000), 0, 101000)
	if result.EndOffset != BatchRuneLimit || result.Boundary != "limit" {
		t.Fatalf("无边界时应在十万字截断: %+v", result)
	}
}

func TestCreateBatchModesAndRangeValidation(t *testing.T) {
	source := strings.Repeat("山", 101000)
	repo := &fakeRepository{project: Project{ID: "b6e6da1c-7c27-4f72-babf-34a7c4e19617", Status: "ready", SourceText: source, SourceLength: len([]rune(source))}}
	service := NewService(repo, &fakeWriter{})
	first, err := service.CreateBatch(context.Background(), "org", "user", repo.project.ID, BatchSelection{Mode: "beginning"})
	if err != nil || first.StartOffset != 0 || first.EndOffset != BatchRuneLimit {
		t.Fatalf("从头创建批次失败: %+v %v", first, err)
	}
	next, err := service.CreateBatch(context.Background(), "org", "user", repo.project.ID, BatchSelection{Mode: "previous", PreviousBatchID: first.ID})
	if err != nil || next.StartOffset != BatchRuneLimit || next.EndOffset != 101000 {
		t.Fatalf("承接上一批次结尾失败: %+v %v", next, err)
	}
	start := 100
	end := 100101
	if _, err := service.CreateBatch(context.Background(), "org", "user", repo.project.ID, BatchSelection{Mode: "manual", StartOffset: &start, EndOffset: &end}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("手动范围超过十万字应拒绝: %v", err)
	}
	if _, err := service.UpdateBatchRange(context.Background(), "org", "user", repo.project.ID, first.ID, BatchRangeChange{StartOffset: 0, EndOffset: 100001, Revision: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("解锁修改也不得超过十万字: %v", err)
	}
	page, err := service.SourcePage(context.Background(), "org", "user", repo.project.ID, 100000, 12000)
	if err != nil || page.EndOffset != 101000 || page.TotalLength != 101000 {
		t.Fatalf("分页阅读结果错误: %+v %v", page, err)
	}
}
