package novel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSerialOpeningAndContinuationWithoutInstructions(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Brief: "守塔人发现潮汐可以修改旧档案", StoryMode: StoryModeSerial})
	if err != nil || p.StoryMode != StoryModeSerial || !p.NeedsTitle || p.Outline != nil {
		t.Fatalf("连载作品应支持只填灵感: %+v %v", p, err)
	}
	initialCount, initialVersion := p.ChapterCount, p.Version
	apply := func(action Action, want error) error {
		t.Helper()
		action.Version = p.Version
		next, err := service.Apply(ctx, owner, p.ID, action)
		if !errors.Is(err, want) {
			t.Fatalf("%s 返回 %v，预期 %v", action.Kind, err, want)
		}
		if want == nil {
			p = next
		}
		return err
	}
	setOutlineResult := func(count int) {
		t.Helper()
		beats := []Beat{
			{Title: "错名", Goal: "守塔人发现船名遭到改写"},
			{Title: "落潮", Goal: "试验改写发生的时刻"},
			{Title: "借钥匙", Goal: "求同伴借出旧仓库钥匙"},
			{Title: "空格", Goal: "找到原本登记过的船名"},
			{Title: "挂灯", Goal: "通知同伴下次涨潮时检查船名"},
		}
		raw, err := json.Marshal(struct {
			Title string `json:"title"`
			Outline
		}{Title: "潮线之上", Outline: Outline{
			Synopsis: "查出海图背后的改写者", World: "潮水可以重写档案", Characters: "守塔人与修船匠", Beats: beats[:count],
		}})
		if err != nil {
			t.Fatal(err)
		}
		writer.result = string(raw)
	}
	generate := Action{Kind: "generate_outline", ModelCode: "writer"}
	generationCalls := 0
	writer.beforeReturn = func() { generationCalls++ }
	setOutlineResult(2)
	var countError *SerialOpeningCountError
	if err := apply(generate, ErrGeneration); !errors.As(err, &countError) ||
		countError.InitialCount != 2 || countError.ActualCount != 2 || countError.TargetCount != 3 {
		t.Fatalf("失败时应保留两次实际章数及目标章数: %v", err)
	}
	if generationCalls != 2 || !strings.Contains(writer.prompt, "生成恰好 3 章") {
		t.Fatalf("首批章数连续越界时应只纠正一次，调用 %d 次，提示词 %q", generationCalls, writer.prompt)
	}
	if repo.project.ChapterCount != initialCount || repo.project.Version != initialVersion || repo.project.Outline != nil {
		t.Fatal("无效章数覆盖了作品")
	}
	setOutlineResult(5)
	apply(generate, nil)
	if generationCalls != 3 {
		t.Fatalf("首批章数有效时无需纠正，累计调用 %d 次", generationCalls)
	}
	if p.ChapterCount != 5 || len(p.Chapters) != 5 || p.Title != "潮线之上" || len(p.Outline.Beats) != 5 {
		t.Fatalf("作品没有采用模型决定的开篇章数和书名: %+v", p)
	}
	if !strings.Contains(writer.prompt, "全书主线") || !strings.Contains(writer.prompt, "首个开篇事件") || strings.Contains(writer.prompt, "规划恰好") {
		t.Fatal("首纲提示词没有将全书主线和开篇章节分开")
	}
	setOutlineResult(4)
	apply(generate, ErrGeneration)
	if generationCalls != 4 || repo.project.ChapterCount != 5 || len(repo.project.Outline.Beats) != 5 {
		t.Fatal("重新生成改变了已确定的首批章数")
	}
	apply(Action{Kind: "confirm_outline", Outline: p.Outline}, nil)
	for i := 1; i < p.ChapterCount; i++ {
		apply(Action{Kind: "finalize", Chapter: i, Body: "守塔人查阅旧账。", Summary: "登记簿内有未核实的蓝印。"}, nil)
	}
	writer.result = `{"body":"守塔人点起油灯。","summary":"同伴发现蓝印缺了一角。"}`
	var writingPrompt string
	writer.beforeReturn = func() {
		if strings.HasPrefix(writer.prompt, reviewPrompt) {
			writer.result = `{"review":"未发现明确问题。"}`
		} else {
			writingPrompt = writer.prompt
		}
	}
	apply(Action{Kind: "write", Chapter: 5, ModelCode: "writer"}, nil)
	writer.beforeReturn = nil
	if !strings.Contains(writingPrompt, "当前批的末章") || strings.Contains(writingPrompt, `"NextBeat"`) {
		t.Fatal("当前批末章没有启用连载节奏")
	}
	apply(Action{Kind: "finalize", Chapter: 5, Body: p.Chapters[4].Body, Summary: p.Chapters[4].Summary}, nil)
	writer.result = `{"beats":[{"title":"断角","goal":"核对缺角与修船铺印模"}]}`
	plan, err := service.PlanChapters(ctx, owner, p.ID, ChapterPlanInput{Version: p.Version, Count: 1, ModelCode: "writer"})
	if err != nil || len(plan.Beats) != 1 {
		t.Fatalf("未填写提示词时续章规划失败: %+v %v", plan, err)
	}
	for _, expected := range []string{"查出海图背后的改写者", "同伴发现蓝印缺了一角", "通知同伴下次涨潮时检查船名", "具体阶段矛盾"} {
		if !strings.Contains(writer.prompt, expected) {
			t.Errorf("续章缺少 %q", expected)
		}
	}
	apply(Action{Kind: "append_chapters", Beats: plan.Beats}, nil)
	if p.ChapterCount != 6 || p.FinalizedChapterCount != 5 || p.Outline.Beats[5].Title != "断角" {
		t.Fatalf("追加计划没有保留已定稿章节: %+v", p)
	}
}

func TestSerialOpeningRepairsOversizedPlanWithoutRewritingSynopsis(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo, writer := &memoryRepository{}, &testWriter{}
	service := NewService(repo, writer, nil, nil)
	p, err := service.Create(ctx, owner, CreateInput{Brief: "守塔人发现潮汐可以修改旧档案", StoryMode: StoryModeSerial})
	if err != nil {
		t.Fatal(err)
	}
	firstBeats := make([]Beat, maxSerialOpeningChapters+1)
	for i := range firstBeats {
		firstBeats[i] = Beat{Title: "原计划", Goal: "检查灯塔档案"}
	}
	repairedBeats := make([]Beat, maxSerialOpeningChapters)
	for i := range repairedBeats {
		repairedBeats[i] = Beat{Title: "调整后的计划", Goal: "核对潮线、档案和守塔人所知的先后"}
	}
	first, err := json.Marshal(struct {
		Title string `json:"title"`
		Outline
	}{"潮线之上", Outline{Synopsis: "查出海图背后的改写者", World: "潮水可以重写档案", Characters: "守塔人与修船匠", Beats: firstBeats}})
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := json.Marshal(Outline{Synopsis: "模型在纠正时擅自改动的主线", Beats: repairedBeats})
	if err != nil {
		t.Fatal(err)
	}
	generationCalls := 0
	writer.beforeReturn = func() {
		generationCalls++
		switch generationCalls {
		case 1:
			writer.result = string(first)
		case 2:
			writer.result = string(repaired)
			for _, want := range []string{"有 9 章", "生成恰好 8 章", p.Brief, "查出海图背后的改写者", "原计划"} {
				if !strings.Contains(writer.prompt, want) {
					t.Errorf("纠正提示词缺少 %q", want)
				}
			}
			if repo.project.Version != p.Version || repo.project.Outline != nil {
				t.Fatal("初次生成的无效大纲不应被提前保存")
			}
		default:
			t.Fatal("首批章数纠正最多调用一次")
		}
	}
	next, err := service.Apply(ctx, owner, p.ID, Action{Kind: "generate_outline", ModelCode: "writer", Version: p.Version})
	if err != nil {
		t.Fatal(err)
	}
	if generationCalls != 2 || next.Version != p.Version+1 || next.ChapterCount != maxSerialOpeningChapters ||
		next.Title != "潮线之上" || next.Outline.Synopsis != "查出海图背后的改写者" ||
		next.Outline.World != "潮水可以重写档案" || next.Outline.Characters != "守塔人与修船匠" ||
		next.Outline.Beats[0].Title != "调整后的计划" {
		t.Fatalf("纠正只应替换开篇章纲并保存一次: %+v", next)
	}
}
