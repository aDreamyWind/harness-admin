package drama

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func narrationFixture() (StoryboardSegment, []string) {
	text := "旁白：屋外下着雨，窗外仍然下着雨，她收好信等天亮再出发。"
	lines := StoryboardLines(text)
	return StoryboardSegment{Title: "雨夜等候", StartLine: 1, EndLine: 1, Shots: []StoryboardShot{{Description: "雨落窗外，她收好信，等天亮再出发", ShotSize: "中景", Camera: "固定", Duration: 12, SourceLines: []int{1}, Utterances: []StoryboardUtterance{{Kind: "vo", Speaker: "旁白", Text: strings.TrimPrefix(text, "旁白："), SourceLine: 1}}}}}, lines
}
func TestPacingAutomaticallyFitsSpeechBeforeMeasuringGain(t *testing.T) {
	before, lines := narrationFixture()
	before.Shots[0].Duration = 12
	shot := before.Shots[0]
	shot.Duration = 1
	patch := pacingPatch{Edits: []pacingEdit{{StartShot: 1, EndShot: 1, Shots: []StoryboardShot{shot}}}}
	got, err := applyPacingPatch(before, []pacingIssue{{Shots: []int{1}}}, patch, lines, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shots[0].Duration >= 12 || got.Shots[0].Duration < storyboardSpeechSeconds(got.Shots[0]) || got.Shots[0].Utterances[0].Text != before.Shots[0].Utterances[0].Text {
		t.Fatal("补时后应保留实际收益和全部文字", got)
	}
	long := StoryboardShot{Utterances: []StoryboardUtterance{{Text: strings.Repeat("字", 250)}}}
	var diagnostic *workflowProblem
	if err = repairStoryboardShotTiming(&long); !errors.As(err, &diagnostic) || diagnostic.Code != "speech_requires_split" {
		t.Fatal("超60秒应要求拆镜", err)
	}
}
func TestPacingNarrationCompressionAndVisualKeepOriginalEvidence(t *testing.T) {
	for _, visual := range []bool{false, true} {
		before, lines := narrationFixture()
		shot := before.Shots[0]
		shot.Utterances = append([]StoryboardUtterance(nil), shot.Utterances...)
		shot.Duration = 2
		u := &shot.Utterances[0]
		u.Text = "雨夜，她收好信，等天亮再出发。"
		if visual {
			u.Delivery = "visual"
			u.Text = ""
		}
		u.SourceText = "不能相信模型自报的原句"
		got, err := applyPacingPatch(before, []pacingIssue{{Shots: []int{1}}}, pacingPatch{Edits: []pacingEdit{{StartShot: 1, EndShot: 1, Shots: []StoryboardShot{shot}}}}, lines, nil)
		if err != nil {
			t.Fatal(visual, err)
		}
		result := got.Shots[0].Utterances[0]
		if result.SourceText != before.Shots[0].Utterances[0].Text || result.AdaptationReason == "" || storyboardSeconds(got) >= storyboardSeconds(before) {
			t.Fatal("缺少真实原句和收益", got)
		}
		if visual && (storyboardSpeechSeconds(got.Shots[0]) != 0 || storyboardHasSpeech(got.Shots[0]) || got.Shots[0].Duration != 2) {
			t.Fatal("转画面不能计入口播", got)
		}
	}
}
func TestPacingCannotCompressDialogueThoughtOrCharacterVoice(t *testing.T) {
	for _, kind := range []string{"dialogue", "os", "vo"} {
		before, lines := narrationFixture()
		before.Shots[0].Utterances[0].Kind = kind
		before.Shots[0].Utterances[0].Speaker = "林夏"
		shot := before.Shots[0]
		shot.Utterances = append([]StoryboardUtterance(nil), shot.Utterances...)
		shot.Utterances[0].Text = "等天亮。"
		shot.Duration = 2
		_, err := applyPacingPatch(before, []pacingIssue{{Shots: []int{1}}}, pacingPatch{Edits: []pacingEdit{{StartShot: 1, EndShot: 1, Shots: []StoryboardShot{shot}}}}, lines, nil)
		if err == nil {
			t.Fatal("角色口播不能借精简旁白改写", kind, lines)
		}
	}
}
func TestStoryboardVoiceAliasesAndActionableContract(t *testing.T) {
	for raw, want := range map[string]string{"narration": "vo", "内心os": "os", "对白": "dialogue", "VO": "vo"} {
		var u StoryboardUtterance
		b, _ := json.Marshal(map[string]string{"kind": raw, "delivery": "spoken"})
		if err := json.Unmarshal(b, &u); err != nil || u.Kind != want || u.Delivery != "" {
			t.Fatal(raw, u, err)
		}
	}
	seg, lines := narrationFixture()
	seg.Shots[0].Utterances[0].Kind = "不存在的声音"
	var diagnostic *workflowProblem
	err := validateStoryboardSegment(&seg, lines, nil, true)
	if !errors.As(err, &diagnostic) || diagnostic.Code != "voice_kind" || !strings.Contains(err.Error(), "不存在的声音") || !strings.Contains(err.Error(), "没有voice必填字段") {
		t.Fatal("应给出实际错误及真实契约", err)
	}
	seg, lines = narrationFixture()
	seg.Shots[0].Utterances[0].SourceText = "原文不存在的句子"
	if validateStoryboardSegment(&seg, lines, nil, true) == nil {
		t.Fatal("不能伪造旁白证据")
	}
	seg, lines = narrationFixture()
	seg.Shots[0].Utterances[0].Delivery = "visual"
	if validateStoryboardSegment(&seg, lines, nil, true) == nil {
		t.Fatal("画面表达须明确保留原句且取消口播")
	}
}

// 可选的本地历史回放：不联网、不写业务表，私有原文与响应不进入仓库或测试输出。
func TestPacingPrivateFailureReplay(t *testing.T) {
	path := os.Getenv("HARNESS_TEST_DRAMA_PACING_REPLAY")
	if path == "" {
		t.Skip("指定本机私有回放文件后验证历史候选")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Before   StoryboardSegment `json:"before"`
		Response pacingPatch       `json:"response"`
		Prompt   string            `json:"prompt"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal("无法读取回放格式")
	}
	if len(cases) == 0 {
		t.Fatal("回放为空")
	}
	pattern := regexp.MustCompile(`\[L(\d+)\] ?([^\n]*)`)
	for i, c := range cases {
		lines := make([]string, c.Before.EndLine)
		for _, m := range pattern.FindAllStringSubmatch(c.Prompt, -1) {
			n, _ := strconv.Atoi(m[1])
			if n > 0 && n <= len(lines) {
				lines[n-1] = m[2]
			}
		}
		issues := []pacingIssue{}
		for _, edit := range c.Response.Edits {
			shots := []int{}
			for n := edit.StartShot; n <= edit.EndShot; n++ {
				shots = append(shots, n)
			}
			issues = append(issues, pacingIssue{Shots: shots})
		}
		forms := []AssetForm{}
		for _, id := range c.Before.FormIDs {
			forms = append(forms, AssetForm{ID: id})
		}
		got, e := applyPacingPatch(c.Before, issues, c.Response, lines, forms)
		if e != nil {
			t.Fatalf("历史候选%d仍未通过程序检查：%v", i+1, e)
		}
		t.Logf("历史候选%d：补时后 %.1f→%.1f秒；程序检查通过，正式采纳仍需AI内容复核", i+1, storyboardSeconds(c.Before), storyboardSeconds(got))
	}
}

func TestNarrationCompressionLosingKeyFactIsNotAdopted(t *testing.T) {
	before, lines := narrationFixture()
	source, repo, _ := pacingFixture()
	before.ID, before.Number, before.Status = repo.run.Segments[0].ID, 1, "pending"
	source.work.ScriptText = strings.Join(lines, "\n")
	repo.run.ScriptText = source.work.ScriptText
	repo.run.SourceHash = storyboardHash(source.work)
	repo.run.Segments = []StoryboardSegment{before}
	repairs := &repairMemory{}
	repo.pacingRepairs = repairs
	calls := 0
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		if strings.HasPrefix(prompt, "你是快节奏漫剧的分镜优化师") {
			calls++
			var candidate StoryboardSegment
			json.Unmarshal([]byte(encodedStoryboard(before)), &candidate)
			candidate.Shots[0].Utterances[0].Text = "她收好信。"
			candidate.Shots[0].Duration = 2
			return encoded(pacingPatch{Edits: []pacingEdit{{StartShot: 1, EndShot: 1, Shots: candidate.Shots}}}), nil
		}
		if strings.Contains(prompt, "复核节奏优化后的实际镜头") {
			if !strings.Contains(prompt, "sourceText") {
				t.Fatal("复核缺少原句依据")
			}
			return `{"blocking":["候选缺少等天亮再出发的重要条件"],"suggestions":[],"pacing":[],"pacingChecks":[]}`, nil
		}
		return `{"blocking":[],"suggestions":[],"pacing":[{"kind":"repeated_exposition","action":"shorten","shots":[1],"reason":"下雨重复解释","fix":"精简重复的下雨描述，保留收信和天亮出发条件"}]}`, nil
	}}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || repo.run.Segments[0].Shots[0].Utterances[0].Text != before.Shots[0].Utterances[0].Text || len(repairs.tickets) != 1 || repairs.tickets[0].Status != "declined" {
		t.Fatal("关键事实丢失不能因为缩短时长而采纳")
	}
}
