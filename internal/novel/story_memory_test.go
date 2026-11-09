package novel

import (
	"context"
	"strings"
	"testing"
)

type storyMemoryTestStore struct {
	MemoryStore
	rows      []MemoryChapter
	decisions map[string]int
}

func (s storyMemoryTestStore) ListMemory(context.Context, Owner, string) ([]MemoryChapter, error) {
	return s.rows, nil
}

func (s storyMemoryTestStore) MemoryDecisions(context.Context, Owner, string) (map[string]int, error) {
	return s.decisions, nil
}

func TestStoryMemoryRecallsChapterOutsideSummarySample(t *testing.T) {
	const quote = "韩王将青纹账册藏在城南票号的夹墙里。"
	p := Project{Workspace: Workspace{
		Chapters: make([]Chapter, 50),
		Outline:  &Outline{Beats: make([]Beat, 50)},
	}}
	for i := range 49 {
		p.Chapters[i] = Chapter{Body: "旧章正文", Summary: "旧章摘要", Final: true}
	}
	p.Chapters[11].Body, p.Chapters[11].Summary = quote, quote
	p.Outline.Beats[49].Goal = "韩王去取账册"
	archive := storyMemoryTestStore{rows: []MemoryChapter{{Chapter: 12, Status: "succeeded", Facts: []MemoryFact{
		{Kind: "clue", Name: "青纹账册", Field: "location", Value: "城南票号", Related: "城南票号", Quote: quote},
	}}}}
	if strings.Contains(chapterContext(p, 49, "", "", nil), quote) {
		t.Fatal("测试案例中的旧章不应恰好落在原有抽样范围")
	}
	calls := 0
	writer := draftJobWriter(func(_ context.Context, prompt string) (string, error) {
		calls++
		if !strings.Contains(prompt, quote) || !strings.Contains(prompt, `"chapter":12`) {
			t.Fatalf("第 %d 次调用未带回相关旧章原文", calls)
		}
		if calls == 1 {
			return `{"body":"韩王走进票号。","summary":"韩王来到票号。"}`, nil
		}
		return `{"review":[]}`, nil
	})
	s := NewService(nil, writer, nil, nil).WithStoryMemory(archive)
	ctx := context.Background()
	draft, err := s.writeChapterDraft(ctx, Owner{}, &p, 49, "writer", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Chapters[49] = draft
	if err := s.reviewChapter(ctx, Owner{}, &p, 49, "writer", nil, nil); err != nil || calls != 2 {
		t.Fatalf("写作和审稿应各读取一次档案: %v", err)
	}
	p.Chapters[11].Final = false
	if got := s.memoryForChapter(ctx, Owner{}, p, 49, "账册"); got != nil {
		t.Fatalf("已撤回的旧章不得继续提供证据: %+v", got)
	}
}

func TestStoryMemorySeparatesRealChangesFromConflicts(t *testing.T) {
	facts := []MemoryFact{
		{Kind: "character", Name: "韩王", Field: "birth_name", Value: "韩明", Quote: "韩王本名韩明", Chapter: 1},
		{Kind: "character", Name: "韩王", Field: "birth_name", Value: "韩彻", Quote: "韩王本名韩彻", Chapter: 4},
		{Kind: "character", Name: "韩王", Field: "location", Value: "城北", Quote: "韩王在城北", Chapter: 2},
		{Kind: "character", Name: "韩王", Field: "location", Value: "城南", Quote: "韩王来到城南", Chapter: 3},
		{Kind: "character", Name: "韩王", Field: "location", Value: "城北", Quote: "韩王回到城北", Chapter: 5},
	}
	unresolved := resolveMemoryFacts(StoryMemory{}, facts, nil)
	if len(unresolved.Facts) != 3 || unresolved.Facts[2].Chapter != 5 || len(unresolved.Conflicts) != 1 {
		t.Fatalf("地点变化不应被当成冲突，冲突姓名不应当作已确认事实: %+v", unresolved)
	}
	decisions := map[string]int{unresolved.Conflicts[0].ID: 1}
	resolved := resolveMemoryFacts(StoryMemory{}, facts, decisions)
	if len(resolved.Conflicts) != 0 || len(resolved.Facts) != 4 || resolved.Facts[3].Value != "韩彻" {
		t.Fatalf("作者的取舍没有生效: %+v", resolved)
	}
	facts[1].Quote = "众人终于知晓韩王本名韩彻"
	if len(resolveMemoryFacts(StoryMemory{}, facts, decisions).Conflicts) != 1 {
		t.Fatal("证据变化后不能沿用旧的确认")
	}
}

func TestStoryMemoryExtractsOnlyFactsWithChapterEvidence(t *testing.T) {
	writer := draftJobWriter(func(context.Context, string) (string, error) {
		return `{"facts":[
		  {"kind":"character","name":"韩王","field":"birth_name","value":"韩明","quote":"韩王本名是韩明。","chapter":99},
		  {"kind":"clue","name":"账册","field":"location","value":"城南","quote":"韩王翻开账册。"},
		  {"kind":"character","name":"韩王","field":"alias","value":"猎人","quote":"韩王也叫猎人。"}
		]}`, nil
	})
	s := NewService(nil, writer, nil, nil)
	facts, err := s.extractStoryMemory(context.Background(), MemoryJob{
		Chapter: 12, ModelCode: "writer", Body: "韩王本名是韩明。韩王翻开账册。",
	})
	if err != nil || len(facts) != 1 || facts[0].Chapter != 12 || facts[0].Value != "韩明" {
		t.Fatalf("档案应只保留有定稿证据的事实并使用真实章号: %+v, %v", facts, err)
	}
}
