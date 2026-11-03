package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"reflect"
	"strings"
	"testing"
)

func pacingFixture() (*sbSource, *sbRepo, StoryboardSegment) {
	source := &sbSource{fakeRepository: fakeRepository{settings: Settings{TextModelCode: "writer"}}, work: VideoEpisodeWorkspace{ScriptText: "【工坊 内 日】\n黎渊进门拿起工具。\n黎渊走到工位。\n黎渊整理台面开始上工。\n黎渊：我来上工。"}}
	seg := StoryboardSegment{ID: uuid.NewString(), Number: 1, Title: "黎渊上工", StartLine: 1, EndLine: 5, Status: "pending", StartState: "工坊门口", EndState: "工位前", FormIDs: []string{}, Notes: "原稿尚未合并镜头", Shots: []StoryboardShot{
		{Number: 1, Description: "黎渊进工坊拿起工具", ShotSize: "中景", Camera: "跟拍", Duration: 6, SourceLines: []int{1, 2}, Utterances: []StoryboardUtterance{}},
		{Number: 2, Description: "黎渊走到工位", ShotSize: "中景", Camera: "跟拍", Duration: 6, SourceLines: []int{3}, Utterances: []StoryboardUtterance{}},
		{Number: 3, Description: "黎渊整理台面开始上工", ShotSize: "中景", Camera: "固定", Duration: 6, SourceLines: []int{4}, Utterances: []StoryboardUtterance{}},
		{Number: 4, Description: "黎渊开口", ShotSize: "近景", Camera: "固定", Duration: 4, SourceLines: []int{5}, Utterances: []StoryboardUtterance{{Kind: "dialogue", Speaker: "黎渊", Text: "我来上工。", SourceLine: 5}}},
	}}
	repo := &sbRepo{run: StoryboardRun{ID: uuid.NewString(), ProjectID: uuid.NewString(), EpisodeID: uuid.NewString(), Revision: 1, Version: 1, ModelCode: "writer", ScriptText: source.work.ScriptText, SourceHash: storyboardHash(source.work), Segments: []StoryboardSegment{seg}}}
	return source, repo, seg
}

var pacingReview = `{"blocking":[],"suggestions":[],"pacing":[{"kind":"repeated_exposition","action":"merge","shots":[1,2,3],"reason":"普通上工动作重复切镜、首句台词前停留18秒","fix":"把进门、取工具、到工位整理台面用连续镜头表现，保留全部动作"}]}`
var pacingMerged = `{"edits":[{"startShot":1,"endShot":3,"shots":[{"description":"黎渊进工坊取工具，快步走到工位整理台面开始上工","shotSize":"中景","camera":"连续跟拍","sound":"脚步和工具声","duration":4,"sourceLines":[1,2,3,4],"utterances":[]}]}]}`
var pacingPassed = `{"blocking":[],"suggestions":[],"pacing":[],"pacingChecks":[{"id":"P1","resolved":true,"reason":"三个上工动作保留在一个连续4秒镜头，口播前铺垫缩短且动作完整"}]}`

func TestPacingSuggestionActuallyMergesAndPersistsBeforeAdoption(t *testing.T) {
	source, repo, original := pacingFixture()
	repairs := &repairMemory{}
	repo.pacingRepairs = repairs
	writes, reviews := 0, 0
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		switch {
		case strings.HasPrefix(prompt, "你是快节奏漫剧的分镜优化师"):
			writes++
			return pacingMerged, nil
		case strings.Contains(prompt, "复核节奏优化后的实际镜头"):
			reviews++
			return pacingPassed, nil
		case strings.HasPrefix(prompt, "你是分镜审阅师"):
			return pacingReview, nil
		default:
			t.Fatal("节奏优化不应重写整个片段或规划")
			return "", nil
		}
	}}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	result := repo.run.Segments[0]
	if writes != 1 || reviews != 1 || repo.run.Status != "ready" || len(result.Shots) != 2 || storyboardSeconds(result) != 8 || !reflect.DeepEqual(result.Shots[1].Utterances, original.Shots[3].Utterances) {
		t.Fatal("建议没有成为真实镜头改动", result)
	}
	if len(repairs.tickets) != 1 {
		t.Fatal(repairs.tickets)
	}
	record := repairs.tickets[0]
	if record.Node != "pacing" || record.Status != "resolved" || record.BeforeShotCount == nil || *record.BeforeShotCount != 4 || record.AfterShotCount == nil || *record.AfterShotCount != 2 || *record.BeforeDuration != 22 || *record.AfterDuration != 8 {
		t.Fatal("采纳记录缺少实际前后数据", record)
	}
	if strings.Contains(result.Notes, "尚未合并") {
		t.Fatal("不应继续展示旧的未执行建议")
	}
}
func TestPacingFailedChangesPreserveApprovedDraftAndContinue(t *testing.T) {
	for _, mode := range []string{"没有修改", "删掉台词", "复核丢失动作"} {
		t.Run(mode, func(t *testing.T) {
			source, repo, original := pacingFixture()
			repairs := &repairMemory{}
			repo.pacingRepairs = repairs
			writes := 0
			writer := &groundedWriter{respond: func(prompt string) (string, error) {
				if strings.HasPrefix(prompt, "你是快节奏漫剧的分镜优化师") {
					writes++
					if mode == "没有修改" {
						return encoded(pacingPatch{Edits: []pacingEdit{{StartShot: 1, EndShot: 3, Shots: original.Shots[:3]}}}), nil
					}
					if mode == "删掉台词" {
						return `{"edits":[{"startShot":1,"endShot":4,"shots":[{"description":"快速上工","shotSize":"中景","camera":"跟拍","duration":4,"sourceLines":[1,2,3,4,5],"utterances":[]}]}]}`, nil
					}
					return pacingMerged, nil
				}
				if strings.Contains(prompt, "复核节奏优化后的实际镜头") {
					return `{"blocking":["优化稿遗漏了拿工具这个关键动作"],"suggestions":[],"pacing":[],"pacingChecks":[]}`, nil
				}
				return pacingReview, nil
			}}
			service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
			if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
				t.Fatal("节奏优化失败不应丢弃已通过的原稿", err)
			}
			if writes != 2 || repo.run.Status != "ready" || !reflect.DeepEqual(repo.run.Segments[0].Shots, original.Shots) {
				t.Fatal("未通过候选覆盖了原稿")
			}
			ticket := repairs.tickets[0]
			if ticket.Status != "declined" || ticket.AfterShotCount == nil || *ticket.AfterShotCount != 4 || *ticket.AfterDuration != 22 || !strings.Contains(ticket.Outcome, "原因") {
				t.Fatal("未记录未采纳与原因", ticket)
			}
		})
	}
}
func TestPacingOrdinarySuggestionDoesNotCompressMeaningfulSilence(t *testing.T) {
	source, repo, original := pacingFixture()
	calls := 0
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		calls++
		return `{"blocking":[],"suggestions":["可考虑更近的景别"],"pacing":[]}`, nil
	}}
	repairs := &repairMemory{}
	repo.pacingRepairs = repairs
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(repairs.tickets) != 0 || !reflect.DeepEqual(repo.run.Segments[0].Shots, original.Shots) {
		t.Fatal("不能仅因无口播18秒而强制删减")
	}
}
func TestPacingCannotAlterUnassignedShotsOrVoice(t *testing.T) {
	_, _, original := pacingFixture()
	lines := StoryboardLines("【工坊 内 日】\n黎渊进门拿起工具。\n黎渊走到工位。\n黎渊整理台面开始上工。\n黎渊：我来上工。")
	issues := []pacingIssue{{Kind: "idle_hold", Action: "shorten", Shots: []int{4}, Reason: "停留", Fix: "缩短"}}
	for _, change := range []func(*pacingPatch){
		func(p *pacingPatch) { p.Edits[0].StartShot = 3 },
		func(p *pacingPatch) { p.Edits[0].Shots[0].Utterances[0].Kind = "vo" },
		func(p *pacingPatch) { p.Edits[0].Shots[0].SourceLines = []int{2} },
	} {
		raw, _ := json.Marshal(original.Shots[3])
		var shot StoryboardShot
		json.Unmarshal(raw, &shot)
		shot.Duration = 2
		patch := pacingPatch{Edits: []pacingEdit{{StartShot: 4, EndShot: 4, Shots: []StoryboardShot{shot}}}}
		change(&patch)
		if _, err := applyPacingPatch(original, issues, patch, lines, nil); err == nil {
			t.Fatal("不安全的节奏修改未拒绝", patch)
		}
	}
}
func TestPacingClaimWithoutPerIssueVerificationIsNotAdopted(t *testing.T) {
	issues := []pacingIssue{{ID: "P1"}}
	for _, report := range []storyboardReviewReport{{}, {PacingChecks: []pacingCheck{{ID: "P1", Resolved: true}}}, {PacingChecks: []pacingCheck{{ID: "invented", Resolved: true, Reason: "已修改"}}}} {
		if verifyPacingChecks(report, issues) == nil {
			t.Fatal("模型口头宣称已采纳不可代替复核")
		}
	}
}

type pacingQueueRepo struct {
	*sbRepo
	queued bool
}

func (r *pacingQueueRepo) QueueStoryboardPacing(_ context.Context, _, _ string, in StoryboardRun, _ int, segment string) (StoryboardRun, error) {
	r.queued = true
	copy := in
	copy.ID = uuid.NewString()
	copy.Status = "queued"
	copy.Version++
	copy.Segments = append([]StoryboardSegment{}, in.Segments...)
	for i := range copy.Segments {
		if copy.Segments[i].ID == segment {
			copy.Segments[i].Status = "pending"
			copy.PacingTarget = copy.Segments[i].Number
		}
		copy.Segments[i].ID = uuid.NewString()
	}
	return copy, nil
}
func TestPacingEntryOnlyAcceptsLatestCompletedOwnedVersion(t *testing.T) {
	source, repo, seg := pacingFixture()
	repo.run.Status = "ready"
	repo.run.Segments[0].Status = "ready"
	queue := &pacingQueueRepo{sbRepo: repo}
	service := NewService(source, &fakeWriter{}).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(queue)
	scope := repo.run
	job, err := service.StartStoryboardPacing(context.Background(), "org", "user", scope.ProjectID, scope.EpisodeID, scope.ID, StoryboardPacingRequest{Revision: 1, SegmentID: seg.ID})
	if err != nil || !queue.queued || job.Run.ID == scope.ID || job.Run.Segments[0].Status != "pending" || repo.run.Segments[0].Status != "ready" {
		t.Fatal("没有建立独立待优化版本", job, err)
	}
	for _, status := range []string{"running", "queued"} {
		repo.run.Status = status
		queue.queued = false
		if _, err = service.StartStoryboardPacing(context.Background(), "org", "user", scope.ProjectID, scope.EpisodeID, scope.ID, StoryboardPacingRequest{Revision: 1, SegmentID: seg.ID}); !errors.Is(err, ErrStoryboardConflict) || queue.queued {
			t.Fatal("运行中或未完成版本不能开始优化", status, err)
		}
	}
}

func TestPacingTargetSkipsOtherUnfinishedSegments(t *testing.T) {
	source, repo, _ := pacingFixture()
	source.work.ScriptText += "\n黎渊下工。"
	repo.run.ScriptText = source.work.ScriptText
	repo.run.SourceHash = storyboardHash(source.work)
	repo.run.PacingTarget = 1
	repo.run.Segments = append(repo.run.Segments, StoryboardSegment{ID: uuid.NewString(), Number: 2, Title: "未完成片段", StartLine: 6, EndLine: 6, Status: "pending"})
	repairs := &repairMemory{}
	repo.pacingRepairs = repairs
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		if strings.Contains(prompt, "[L6]") || strings.HasPrefix(prompt, "你是分镜编写师") {
			t.Fatal("局部优化不能启动其他片段")
		}
		if strings.HasPrefix(prompt, "你是快节奏") {
			return pacingMerged, nil
		}
		if strings.Contains(prompt, "复核节奏优化后的实际镜头") {
			return pacingPassed, nil
		}
		return pacingReview, nil
	}}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	if repo.run.Status != "partial" || len(repo.saved) != 1 || repo.saved[0] != 1 || repo.run.Segments[1].Status != "pending" || len(repo.run.Segments[1].Shots) != 0 {
		t.Fatal("其他未完成片段被误处理", repo.run)
	}
}
func TestPacingResumeClosesUncommittedOptimizationTicket(t *testing.T) {
	source, repo, _ := pacingFixture()
	count, seconds := 4, 22.0
	repairs := &repairMemory{tickets: []RepairTicket{{ID: uuid.NewString(), ProjectID: repo.run.ProjectID, Kind: "storyboard", RunID: repo.run.ID, Item: 1, Node: "pacing", TargetItem: 1, Status: "assigned", BeforeShotCount: &count, BeforeDuration: &seconds}}}
	writer := &groundedWriter{respond: func(string) (string, error) { return `{"blocking":[],"suggestions":[],"pacing":[]}`, nil }}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	if repairs.tickets[0].Status != "declined" || !strings.Contains(repairs.tickets[0].Outcome, "未提交改动") || *repairs.tickets[0].AfterDuration != 22 {
		t.Fatal("中断派单不能永远停留在优化中")
	}
}

func TestManualPacingGroupsShortSilentShotsBeforeThought(t *testing.T) {
	segment := StoryboardSegment{Shots: []StoryboardShot{
		{Duration: 4}, {Duration: 3},
		{Duration: 5, Utterances: []StoryboardUtterance{{Kind: "os", Text: "我还能回去吗？"}}},
	}}
	issues := supplementManualPacingIssues(nil, segment, nil, nil)
	if len(issues) != 1 || issues[0].Action != "merge" || fmt.Sprint(issues[0].Shots) != "[1 2 3]" {
		t.Fatalf("应当把连续铺垫和紧接的心声一起派工，实际为 %+v", issues)
	}
}
