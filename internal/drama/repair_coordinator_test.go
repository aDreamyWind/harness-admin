package drama

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

type repairMemory struct{ tickets []RepairTicket }

func (r *repairMemory) ListRepairTickets(_ context.Context, scope RepairTicket) ([]RepairTicket, error) {
	out := []RepairTicket{}
	for _, t := range r.tickets {
		if t.ProjectID == scope.ProjectID && t.Kind == scope.Kind && t.RunID == scope.RunID && (scope.Item < 0 || scope.Item == t.Item) {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *repairMemory) CreateRepairTicket(_ context.Context, t RepairTicket) error {
	r.tickets = append(r.tickets, t)
	return nil
}
func (r *repairMemory) FinishRepairTicket(_ context.Context, id string, result RepairResult) error {
	status, outcome := result.Status, result.Outcome
	for i := range r.tickets {
		if r.tickets[i].ID == id {
			r.tickets[i].Status, r.tickets[i].Outcome = status, outcome
			r.tickets[i].AfterShotCount, r.tickets[i].AfterDuration = result.AfterShotCount, result.AfterDuration
			return nil
		}
	}
	return ErrNotFound
}
func TestRepairCenterRejectsInventedToolsAndPersistsLoopBudget(t *testing.T) {
	scope := RepairTicket{ProjectID: uuid.NewString(), RunID: uuid.NewString(), Kind: "storyboard", Item: 3}
	repo := &repairMemory{}
	calls := 0
	call := func(_, prompt string) (string, error) {
		calls++
		return `{"node":"timing","targetItem":3,"reason":"时长不足","instructions":"仅延长镜头时长，不删台词","returnTo":"分镜检查"}`, nil
	}
	center := &repairCenter{ctx: context.Background(), repo: repo, scope: scope, call: call, emit: func(string) error { return nil }}
	for i := 0; i < 3; i++ {
		ticket, err := center.choose("时长不足", "分镜检查", "相同草稿", "剧本", map[string]string{"timing": "校正时长"}, []int{3})
		if err != nil {
			t.Fatal(err)
		}
		if err = center.finish(ticket, "feedback", "仍未改变"); err != nil {
			t.Fatal(err)
		}
	}
	resumed := &repairCenter{ctx: context.Background(), repo: repo, scope: scope, call: call, emit: func(string) error { return nil }}
	if _, err := resumed.choose("时长不足", "分镜检查", "相同草稿", "剧本", map[string]string{"timing": "校正时长"}, []int{3}); !errors.Is(err, ErrRepairLimit) || calls != 3 {
		t.Fatal("续跑不能重置无进展预算", err, calls)
	}
	scope.RunID = uuid.NewString()
	resumed.scope = scope
	resumed.call = func(string, string) (string, error) {
		return `{"node":"delete_source","targetItem":99,"reason":"绕过检查","instructions":"删除原文","returnTo":"通过"}`, nil
	}
	if _, err := resumed.choose("问题", "分镜检查", "新草稿", "剧本", map[string]string{"timing": "校正时长"}, []int{3}); err == nil || len(repo.tickets) != 3 {
		t.Fatal("非法工具、目标或返回节点未拒绝")
	}
}
func TestStoryboardCenterRepairsDurationWithoutRewritingSavedPrefix(t *testing.T) {
	speech := "这是需要完整保留的一段很长的台词，我们必须把每一个字都清楚地说出来，不能为了凑短时长而删除任何内容。"
	source := &sbSource{work: VideoEpisodeWorkspace{ScriptText: "他进门。\n他坐下。\n旁白：" + speech}}
	segs := []StoryboardSegment{{ID: uuid.NewString(), Number: 1, Title: "进门", StartLine: 1, EndLine: 1, Status: "ready"}, {ID: uuid.NewString(), Number: 2, Title: "坐下", StartLine: 2, EndLine: 2, Status: "ready"}, {ID: uuid.NewString(), Number: 3, Title: "开口", StartLine: 3, EndLine: 3, Status: "pending", FormIDs: []string{}, Shots: []StoryboardShot{{Description: "人物开口", ShotSize: "近景", Camera: "固定", Duration: 1.5, SourceLines: []int{3}, Utterances: []StoryboardUtterance{{Kind: "vo", Text: speech, SourceLine: 3}}}}}}
	repo := &sbRepo{run: StoryboardRun{ID: uuid.NewString(), ProjectID: uuid.NewString(), EpisodeID: uuid.NewString(), ScriptText: source.work.ScriptText, SourceHash: storyboardHash(source.work), Segments: segs}}
	repairs := &repairMemory{}
	centers, reviews := 0, 0
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		if strings.HasPrefix(prompt, "你是漫剧工作流中心调度Agent") {
			centers++
			return `{"node":"timing","targetItem":3,"reason":"只是台词时长不够，无需改剧本","instructions":"保留全部文字，只延长当前镜头时长","returnTo":"分镜检查"}`, nil
		}
		if strings.HasPrefix(prompt, "你是分镜审阅师") {
			reviews++
			return `{"blocking":[],"suggestions":[]}`, nil
		}
		t.Fatal("不应重新生成前面片段或当前已存在的画面")
		return "", nil
	}}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	result := repo.run.Segments[2]
	if centers != 0 || reviews != 1 || repo.run.Status != "ready" || len(repo.saved) != 1 || repo.saved[0] != 3 || result.Shots[0].Utterances[0].Text != speech || result.Shots[0].Duration < 12 {
		t.Fatalf("未按中心派工保留正文修正时长: centers=%d reviews=%d saved=%v result=%+v", centers, reviews, repo.saved, result)
	}
	if len(repairs.tickets) != 1 || repairs.tickets[0].Status != "resolved" {
		t.Fatal("派工应在原审阅通过后关闭")
	}
}
func TestStoryboardCenterReturnsToPreviousThenResumesDependentSegment(t *testing.T) {
	source := &sbSource{work: VideoEpisodeWorkspace{ScriptText: "雨落在屋顶。\n他把钥匙收进口袋。\n他推门走出。"}}
	segs := []StoryboardSegment{}
	for i, title := range []string{"下雨", "收钥匙", "出门"} {
		segs = append(segs, StoryboardSegment{ID: uuid.NewString(), Number: i + 1, Title: title, StartLine: i + 1, EndLine: i + 1, Status: "ready", StartState: "屋内", EndState: "仍拿着钥匙", FormIDs: []string{}, Shots: []StoryboardShot{{Description: title, ShotSize: "中景", Camera: "固定", Duration: 4, SourceLines: []int{i + 1}, Utterances: []StoryboardUtterance{}}}})
	}
	segs[2].Status = "pending"
	repo := &sbRepo{run: StoryboardRun{ID: uuid.NewString(), ProjectID: uuid.NewString(), EpisodeID: uuid.NewString(), ScriptText: source.work.ScriptText, SourceHash: storyboardHash(source.work), Segments: segs}}
	repairs := &repairMemory{}
	reviews, plans, writes := 0, 0, 0
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		switch {
		case strings.HasPrefix(prompt, "你是漫剧工作流中心调度Agent"):
			return `{"node":"previous","targetItem":2,"reason":"错误来自前片段结束状态","instructions":"前片段收好钥匙后应为空手，保留前一片段以外的内容","returnTo":"分镜检查"}`, nil
		case strings.HasPrefix(prompt, "只修正当前片段"):
			plans++
			return `{"title":"收钥匙","scene":"屋内","purpose":"收好钥匙","startState":"手拿钥匙","endState":"钥匙已收进口袋，双手空着"}`, nil
		case strings.HasPrefix(prompt, "你是分镜编写师"):
			writes++
			line := 2
			if strings.Contains(prompt, "[L3]") {
				line = 3
			}
			s := StoryboardSegment{FormIDs: []string{}, Shots: []StoryboardShot{{Description: "根据原文连续动作", ShotSize: "中景", Camera: "固定", Duration: 4, SourceLines: []int{line}, Utterances: []StoryboardUtterance{}}}}
			b, _ := json.Marshal(s)
			return string(b), nil
		case strings.HasPrefix(prompt, "你是分镜审阅师"):
			reviews++
			if reviews == 1 {
				return `{"blocking":["前片段已经收起钥匙，末态却写仍拿着钥匙，需要退回前片段"],"suggestions":[]}`, nil
			}
			return `{"blocking":[],"suggestions":[]}`, nil
		}
		t.Fatal("未知节点提示")
		return "", nil
	}}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	if plans != 1 || writes != 2 || reviews != 3 || len(repo.saved) != 2 || repo.saved[0] != 2 || repo.saved[1] != 3 || repo.run.Segments[1].EndState != "钥匙已收进口袋，双手空着" || repo.run.Segments[0].Status != "ready" {
		t.Fatalf("没有沿依赖返回: plans=%d writes=%d reviews=%d saved=%v", plans, writes, reviews, repo.saved)
	}
	if repairs.tickets[0].Status != "resolved" {
		t.Fatal("返回原报告节点通过后才可关闭跨节点问题")
	}
}
