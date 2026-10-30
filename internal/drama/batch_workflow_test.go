package drama

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func workflowFixture(source string) (*fakeRepository, *fakeWriter, *Service) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	repo := &fakeRepository{project: Project{ID: id, Title: "短剧", Status: "ready", SourceText: source, Overview: json.RawMessage(`{}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, EndOffset: len([]rune(source)), Revision: 1}}}
	writer := &fakeWriter{}
	return repo, writer, NewService(repo, writer)
}

const workflowArt = `{"characters":[],"scenes":[],"props":[]}`
const workflowStory = `{"units":[{"id":1,"event":"甲走入屋中"},{"id":2,"event":"甲发现信件"}]}`
const workflowName = `{"name":"雨夜来信"}`
const workflowPlan = `{"episodes":[{"title":"入屋","synopsis":"甲走入屋中","hook":"发现声响","startUnit":1,"endUnit":1},{"title":"信件","synopsis":"甲发现信件","hook":"发现秘密","startUnit":2,"endUnit":2}]}`
const workflowFirst = `{"episodes":[{"title":"入屋","synopsis":"甲走入屋中","scriptText":"【屋内 日】甲推开门。","characters":[],"scenes":[],"props":[]}]}`
const workflowSecond = `{"episodes":[{"title":"信件","synopsis":"甲发现信件","scriptText":"【屋内 日】甲打开信件。","characters":[],"scenes":[],"props":[]}]}`
const workflowPassed = `{"passed":true,"issues":[]}`

func TestWorkflowPlansWholeBatchBeforeWritingAndResumesFailedEpisode(t *testing.T) {
	repo, writer, service := workflowFixture(strings.Repeat("甲", 1100))
	id := repo.project.ID
	writer.replies = []string{workflowArt, workflowStory, workflowPlan, workflowName, workflowFirst, workflowPassed}
	writer.errors = []error{nil, nil, nil, nil, nil, nil, errors.New("临时供应商故障")}
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AnalyzeBatch(job); err == nil {
		t.Fatal("第二集调用应失败")
	}
	progress, err := service.BatchProgress(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(progress.Plans) != 2 || len(progress.Episodes) != 1 || len(repo.steps) != 7 {
		t.Fatalf("全局规划或阶段成果丢失：%+v", progress)
	}
	repo.settings.TextModelCode = "changed-model"
	job, err = service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if job.ModelCode != "writer" {
		t.Fatal("续跑应沿用运行时模型快照")
	}
	writer.replies = []string{workflowSecond, workflowPassed}
	if err = service.AnalyzeBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 9 || len(repo.batchDraft.Episodes) != 2 {
		t.Fatalf("续跑应只执行未完成的第二集及审校：%d", len(writer.prompts))
	}
	if !strings.Contains(writer.prompts[2], "整批") || !strings.Contains(writer.prompts[4], "已经确定的本集规划") {
		t.Fatal("必须先整批规划再逐集写作")
	}
}
func TestWorkflowRepairsOnlyEpisodeRejectedByReview(t *testing.T) {
	repo, writer, service := workflowFixture(strings.Repeat("甲", 1100))
	id := repo.project.ID
	writer.replies = []string{workflowArt, workflowStory, workflowPlan, workflowName, workflowFirst, workflowPassed, workflowSecond, `{"passed":false,"issues":["信件内容与原文不符"]}`, workflowSecond, workflowPassed}
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AnalyzeBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 10 || !strings.Contains(writer.prompts[8], "信件内容与原文不符") || len(repo.batchDraft.Episodes) != 2 {
		t.Fatal("审校应仅修正对应剧集并重新检查")
	}
}
func TestExplicitRestartClearsCheckpointsWhileResumeRejectsReadyOrChangedRange(t *testing.T) {
	repo, _, service := workflowFixture("甲")
	id := repo.project.ID
	repo.batches[0].Status = "failed"
	repo.batches[0].AnalysisRevision = 1
	repo.steps = []BatchStep{{Stage: "art", ChunkNumber: 1, ChunkTotal: 1, Status: "completed", ResultText: workflowArt}}
	if _, err := service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id); err != nil {
		t.Fatal(err)
	}
	if len(repo.steps) != 1 {
		t.Fatal("续跑不得清空成功步骤")
	}
	repo.batches[0].Status = "failed"
	if _, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id); err != nil {
		t.Fatal(err)
	}
	if len(repo.steps) != 0 {
		t.Fatal("重新解析必须从头开始")
	}
	repo.batches[0].Status = "ready"
	if _, err := service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id); !errors.Is(err, ErrResumeNotAllowed) {
		t.Fatal("已完成批次不可续跑")
	}
	repo.batches[0].Status = "failed"
	repo.batches[0].Revision = 2
	if _, err := service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id); !errors.Is(err, ErrResumeNotAllowed) {
		t.Fatal("范围改变后不可沿用旧检查点")
	}
}
func TestEpisodePlanCoversEverySourceUnitExactlyOnce(t *testing.T) {
	units := makeSourceUnits(strings.Repeat("甲", 2500))
	valid := []EpisodePlan{{Title: "一", Synopsis: "开始", StartUnit: 1, EndUnit: 2}, {Title: "二", Synopsis: "结束", StartUnit: 3, EndUnit: 3}}
	if err := validateEpisodePlan(valid, units); err != nil {
		t.Fatal(err)
	}
	for _, plans := range [][]EpisodePlan{{{Title: "一", Synopsis: "缺头", StartUnit: 2, EndUnit: 3}}, {{Title: "一", Synopsis: "缺尾", StartUnit: 1, EndUnit: 2}}, {{Title: "一", Synopsis: "重叠", StartUnit: 1, EndUnit: 2}, {Title: "二", Synopsis: "重叠", StartUnit: 2, EndUnit: 3}}} {
		if err := validateEpisodePlan(plans, units); err == nil {
			t.Fatal("应拒绝遗漏或重叠")
		}
	}
	for i, u := range units {
		if i == 0 && u.Start != 0 {
			t.Fatal("原文开头丢失")
		}
		if i > 0 && units[i-1].End != u.Start {
			t.Fatal("原文单元不连续")
		}
	}
	if units[len(units)-1].End != 2500 {
		t.Fatal("原文末尾丢失")
	}
}
func TestAdaptiveSchedulerHasAtMostTwoConcurrentTasks(t *testing.T) {
	repo, _, service := workflowFixture("甲")
	w := &batchWorkflow{service: service, ctx: context.Background(), slowThreshold: time.Millisecond}
	var active, peak atomic.Int32
	tasks := []workflowTask{}
	for i := 0; i < 7; i++ {
		tasks = append(tasks, workflowTask{run: func() error {
			n := active.Add(1)
			for {
				old := peak.Load()
				if old >= n || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			active.Add(-1)
			return nil
		}})
	}
	if err := w.schedule(tasks); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 || len(repo.events) == 0 {
		t.Fatalf("调度未按耗时启用有界并行：%d", peak.Load())
	}
}
func TestLiveScriptPreviewDecodesPartialJSONWithoutReasoning(t *testing.T) {
	got := partialScriptText(`{"episodes":[{"title":"测试","scriptText":"【屋内 日】\\n甲推开`)
	if !strings.Contains(got, "甲推开") {
		t.Fatalf("无法抽取流式剧本：%q", got)
	}
	if got := partialScriptText(`{"reasoning_content":"推理过程"}`); got != "" {
		t.Fatal("不得展示推理内容")
	}
}

func TestReviewFailureRemainsResumableWithoutRewritingApprovedEpisodes(t *testing.T) {
	repo, writer, service := workflowFixture(strings.Repeat("甲", 1100))
	id := repo.project.ID
	rejected := `{"passed":false,"issues":["事件与原文不符"]}`
	writer.replies = []string{workflowArt, workflowStory, workflowPlan, workflowName, workflowFirst, workflowPassed, workflowSecond, rejected, workflowSecond, rejected}
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AnalyzeBatch(job); err == nil {
		t.Fatal("审校两次未通过应停止而非无限修正")
	}
	found := false
	for _, step := range repo.steps {
		if step.Stage == "review" && step.ChunkNumber == 2 && step.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("必须明确记录失败的审校步骤")
	}
	writer.replies = []string{workflowPassed}
	job, err = service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AnalyzeBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 11 || len(repo.batchDraft.Episodes) != 2 {
		t.Fatal("续跑应保留完成的剧本，仅继续审校")
	}
}
func TestPlanCannotMergeAcrossExplicitEpisodeHeadings(t *testing.T) {
	units := []sourceUnit{{ID: 1, Start: 0, End: 20, Text: "第1集 开始\n甲进入屋中"}, {ID: 2, Start: 20, End: 40, Text: "第2集 信件\n甲发现信件"}}
	if err := validateEpisodePlan([]EpisodePlan{{Title: "合并", Synopsis: "合并两集", StartUnit: 1, EndUnit: 2}}, units); err == nil {
		t.Fatal("不得跨越原文已有集界")
	}
	if !strings.Contains(unitLengths(units), "第2集") {
		t.Fatal("整批规划必须包含原集界信息")
	}
}

func TestManualBatchNameSkipsAINameCall(t *testing.T) {
	repo, writer, service := workflowFixture(strings.Repeat("甲", 1100))
	id := repo.project.ID
	repo.batches[0].Name = "用户定义名称"
	repo.batches[0].NameManuallyEdited = true
	writer.replies = []string{workflowArt, workflowStory, workflowPlan, workflowFirst, workflowPassed, workflowSecond, workflowPassed}
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AnalyzeBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 7 || repo.batches[0].Name != "用户定义名称" {
		t.Fatal("已手动命名不应重新调用AI起名")
	}
	for _, name := range []string{"", "   ", strings.Repeat("甲", 81)} {
		if _, err := service.RenameBatch(context.Background(), "org", "user", id, id, name); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("应拒绝空或过长名称")
		}
	}
}
