package novel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNovelRefineIdeaReturnsPreviewWithoutProject(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	writer := &testWriter{result: `{"brief":"  守塔人必须在天亮前找出午夜呼救的来历。  "}`}
	service := NewService(nil, writer, nil, nil)

	result, err := service.RefineIdea(ctx, owner, RefineIdeaInput{
		Brief: " 守塔人听到呼救 ", Instruction: " 加入天亮前的时限 ", ModelCode: "writer",
		PreviousSuggestions: []string{"旧稿内容不应发送给模型"},
	})
	if err != nil || result.Brief != "守塔人必须在天亮前找出午夜呼救的来历。" {
		t.Fatalf("灵感优化预览结果错误: %+v, %v", result, err)
	}
	for _, want := range []string{`"Brief":"守塔人听到呼救"`, `"Instruction":"加入天亮前的时限"`} {
		if !strings.Contains(writer.prompt, want) {
			t.Errorf("模型提示缺少创作资料 %s", want)
		}
	}
	if strings.Contains(writer.prompt, `"CreativeDirection":`) || strings.Contains(writer.prompt, "旧稿内容不应发送给模型") {
		t.Fatal("作者已指定优化方向时，不应附加默认创作方向或历史内容")
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

func TestNovelRefineIdeaRetriesRepeatedNames(t *testing.T) {
	const old = "萧景珩成了闲散王爷，绑定闲王日常系统。"
	const improved = "桥吏顾承舟得到渡口验契器。太子的船靠岸时，他从两份通行凭据上发现了同一道裂痕。"
	for _, tt := range []struct{ name, brief, protagonist, system, feedback string }{
		{"主角重名", "萧景珩在渡口截获一封密信。", "萧景珩", "", "主角姓名「萧景珩」"},
		{"系统名只换后缀", "桥吏顾承舟得到闲王日常助手。", "顾承舟", "闲王日常助手", "系统名称「闲王日常助手」"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first := asJSON(ideaCandidate{Brief: tt.brief, ProtagonistName: &tt.protagonist, SystemName: &tt.system})
			second := `{"brief":"` + improved + `","protagonistName":"顾承舟","systemName":"渡口验契器"}`
			writer := &testWriter{}
			var prompts []string
			writer.beforeReturn = func() {
				prompts = append(prompts, writer.prompt)
				writer.result = first
				if len(prompts) > 1 {
					writer.result = second
				}
			}
			service := NewService(nil, writer, nil, nil)
			result, err := service.RefineIdea(context.Background(), Owner{}, RefineIdeaInput{
				Brief: "古代系统流，主角与太子交好。", ModelCode: "writer", PreviousSuggestions: []string{old},
			})
			if err != nil || result.Brief != improved || result.ProtagonistName != "顾承舟" || result.SystemName != "渡口验契器" || len(prompts) != 2 {
				t.Fatalf("预览应在一次重试后换掉重复内容，调用 %d 次，结果 %+v，错误 %v", len(prompts), result, err)
			}
			if !strings.Contains(prompts[0], old) || !strings.Contains(prompts[1], tt.feedback) {
				t.Fatalf("请求缺少历史预览或具体重试原因：%q", prompts)
			}
		})
	}
}

func TestNovelRefineIdeaRejectsUnchangedOpeningAfterOneRetry(t *testing.T) {
	const old = "萧景珩穿越成为一名闲散王爷。他在宫宴上看到太子收到一封匿名密信，随后顺着信纸上留下的线索来到城西废弃的渡口，找到隐姓埋名的前任船夫。"
	result := strings.Replace(old, "萧景珩", "陆明舟", 1)
	calls := 0
	writer := &testWriter{
		result:       `{"brief":"` + result + `","protagonistName":"陆明舟","systemName":""}`,
		beforeReturn: func() { calls++ },
	}
	service := NewService(nil, writer, nil, nil)
	preview, err := service.RefineIdea(context.Background(), Owner{}, RefineIdeaInput{
		Brief: "古代穿越爽文，主角是与太子交好的闲散王爷。", ModelCode: "writer", PreviousSuggestions: []string{old},
	})
	if !errors.Is(err, ErrIdeaRepetition) || preview.Brief != "" || calls != 2 {
		t.Fatalf("只换名字的开场最多重试一次，调用 %d 次，结果 %+v，错误 %v", calls, preview, err)
	}
	if !strings.Contains(writer.prompt, "开场与旧优化结果过于相似") {
		t.Fatal("重试请求未说明开场重复")
	}
}

func TestNovelRefineIdeaAllowsNamesSpecifiedByAuthor(t *testing.T) {
	const improved = "萧景珩靠闲王日常系统修复残缺地图，并在冬至前找出渡口迁移的原因。"
	calls := 0
	writer := &testWriter{
		result:       `{"brief":"` + improved + `","protagonistName":"萧景珩","systemName":"闲王日常系统"}`,
		beforeReturn: func() { calls++ },
	}
	service := NewService(nil, writer, nil, nil)
	preview, err := service.RefineIdea(context.Background(), Owner{}, RefineIdeaInput{
		Brief: "主角叫萧景珩，系统名为闲王日常系统。", ModelCode: "writer",
		PreviousSuggestions: []string{"萧景珩穿越到宫宴上，绑定闲王日常系统。"},
	})
	if err != nil || preview.Brief != improved || calls != 1 {
		t.Fatalf("作者指定的姓名和系统名应当保留，调用 %d 次，结果 %+v，错误 %v", calls, preview, err)
	}
}

func TestNovelRefineIdeaRejectsOversizedHistory(t *testing.T) {
	for _, history := range [][]string{{"甲", "乙", "丙", "丁"}, {strings.Repeat("字", 501)}} {
		writer := &testWriter{}
		service := NewService(nil, writer, nil, nil)
		_, err := service.RefineIdea(context.Background(), Owner{}, RefineIdeaInput{
			Brief: "古代悬疑", ModelCode: "writer", PreviousSuggestions: history,
		})
		if !errors.Is(err, ErrInvalid) || writer.prompt != "" {
			t.Fatalf("历史超限应在调用模型前被拒绝，错误 %v", err)
		}
	}
}

func TestNovelRefineIdeaChoosesDirectionWithoutExistingNovel(t *testing.T) {
	const brief = "守塔人听到呼救"
	calls := 0
	writer := &testWriter{
		result:       `{"brief":"守塔人怀疑呼救来自已沉没的船。"}`,
		beforeReturn: func() { calls++ },
	}
	service := NewService(nil, writer, nil, nil)
	_, err := service.RefineIdea(context.Background(), Owner{OrganizationID: "org", UserID: "author"}, RefineIdeaInput{
		Brief: brief, Instruction: " \t", ModelCode: "writer",
	})
	if err != nil || calls != 1 {
		t.Fatalf("未创建小说时应能通过一次模型调用生成预览: 调用 %d 次，错误 %v", calls, err)
	}

	prompt := strings.TrimSpace(writer.prompt)
	var material struct {
		Brief, Instruction string
		CreativeDirection  ideaDirection
	}
	if err := json.Unmarshal([]byte(prompt[strings.LastIndexByte(prompt, '\n')+1:]), &material); err != nil {
		t.Fatalf("模型请求没有可用的灵感资料: %v", err)
	}
	if material.Brief != brief || material.Instruction != "" {
		t.Fatalf("默认优化应保留作者的原始输入: %+v", material)
	}
	direction := material.CreativeDirection
	if direction.Naming == "" || direction.Protagonist == "" || direction.Mechanism == "" || direction.Opening == "" {
		t.Fatalf("默认优化缺少创作方向: %+v", direction)
	}
}

func TestIdeaDirectionVariesWithIndependentPreviewSeeds(t *testing.T) {
	first := ideaDirectionFor(uuid.MustParse("00000000-0000-4000-8000-000000000000"))
	second := ideaDirectionFor(uuid.MustParse("01010101-0101-4101-8101-010101010101"))
	if first == second {
		t.Fatalf("不同预览种子使用了同一组创作方向: %+v", first)
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
