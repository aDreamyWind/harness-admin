package drama

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type groundedWriter struct {
	fakeWriter
	respond func(string) (string, error)
}

func (w *groundedWriter) Complete(_ context.Context, _, _, _ string, prompt string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prompts = append(w.prompts, prompt)
	return w.respond(prompt)
}
func encoded(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestGroundedEvidenceChecksQuoteAndGlobalOffsets(t *testing.T) {
	units := []sourceUnit{{ID: 2, Start: 10, End: 20, Text: "秦熊说：年底入门。"}}
	raw := json.RawMessage(`{"units":[{"id":2,"event":"年底入门","facts":[{"claim":"年底要求","quote":"年底入门"}]}]}`)
	got, err := groundedEvidence(raw, units, 50000)
	if err != nil {
		t.Fatal(err)
	}
	var d evidenceDigest
	if err = json.Unmarshal(got, &d); err != nil {
		t.Fatal(err)
	}
	f := d.Units[0].Facts[0]
	if f.ID != "U2F1" || f.StartOffset != 50014 || f.EndOffset != 50018 {
		t.Fatalf("字符依据定位错误: %+v", f)
	}

	bad := strings.Replace(string(raw), `"quote":"年底入门"`, `"quote":"原文没有的话"`, 1)
	if _, err = groundedEvidence(json.RawMessage(bad), units, 50000); err == nil {
		t.Fatal("不得接受不存在的引文")
	}
}
func TestGroundedPlanRejectsFutureFactsAndReplacesFreeSynopsis(t *testing.T) {
	units := []sourceUnit{{ID: 1, Start: 0, End: 8, Text: "今天传武。"}, {ID: 2, Start: 8, End: 16, Text: "年关入门。"}}
	facts := map[string]SourceFact{"U1F1": {ID: "U1F1", UnitID: 1, Claim: "今天传武", Quote: "今天传武"}, "U2F1": {ID: "U2F1", UnitID: 2, Claim: "年关入门", Quote: "年关入门"}}
	raw := json.RawMessage(`{"episodes":[{"title":"传武","synopsis":"年关入门","startUnit":1,"endUnit":1,"factIds":["U1F1"]},{"title":"入门","startUnit":2,"endUnit":2,"factIds":["U2F1"]}]}`)
	grounded, err := groundPlan(raw, units, facts)
	if err != nil {
		t.Fatal(err)
	}
	var p episodePlanResult
	_ = json.Unmarshal(grounded, &p)
	if p.Episodes[0].Synopsis != "今天传武" || len(p.Episodes[0].Sources) != 1 {
		t.Fatal("不得沿用超出本集范围的自由梗概")
	}
	raw = json.RawMessage(strings.Replace(string(raw), `"factIds":["U1F1"]`, `"factIds":["U2F1"]`, 1))
	if _, err = groundPlan(raw, units, facts); err == nil {
		t.Fatal("后集事实不得引用到前集")
	}
}
func TestGroundedBatchUsesOwnSourceInsteadOfInitialOverview(t *testing.T) {
	repo, _, _ := workflowFixture("前文只供承接。第50章 后期\n乙已经成为掌门。")
	repo.project.Overview = json.RawMessage(`{"videoStyle":"水墨","aspectRatio":"16:9","synopsis":"初期只有凡人","setting":"年关前必须入门"}`)
	repo.batches[0].StartOffset = len([]rune("前文只供承接。"))
	writer := &groundedWriter{}
	writer.respond = func(prompt string) (string, error) {
		switch {
		case strings.Contains(prompt, "你是漫剧美术指导"):
			return workflowArt, nil
		case strings.Contains(prompt, "你是原文事实整理员"):
			return `{"units":[{"id":1,"event":"乙成为掌门","facts":[{"claim":"乙成为掌门","quote":"乙已经成为掌门"}]}]}`, nil
		case strings.Contains(prompt, "只依据本批事件"):
			return `{"summary":"乙成为掌门","background":"后期","setting":"掌门身份","continuity":"只读过起点前一小段，其余未知"}`, nil
		case strings.Contains(prompt, "你是漫剧总导演"):
			return `{"episodes":[{"title":"掌门","startUnit":1,"endUnit":1,"factIds":["U1F1"]}]}`, nil
		case strings.Contains(prompt, "你是漫剧编辑"):
			return workflowName, nil
		case strings.Contains(prompt, "你是漫剧导演兼短剧编剧"):
			return `{"episodes":[{"title":"掌门","synopsis":"乙成为掌门","scriptText":"乙成为掌门。","characters":[],"scenes":[],"props":[]}]}`, nil
		case strings.Contains(prompt, "你是剧本审校"):
			return `{"checks":[],"findings":[]}`, nil
		}
		return "", errors.New("意外提示词")
	}
	service := NewService(repo, writer)
	id := repo.project.ID
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if job.WorkflowVersion != 3 || strings.Contains(string(job.Overview), "初期") {
		t.Fatal("运行快照不得保存旧概览剧情为本批事实")
	}
	if err = service.AnalyzeBatch(job); err != nil {
		t.Fatal(err)
	}
	progress, err := service.BatchProgress(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Context == nil || progress.Context.StartOffset != repo.batches[0].StartOffset || progress.Context.PrecedingEnd != repo.batches[0].StartOffset || len(progress.Plans[0].Sources) != 1 {
		t.Fatal("本批上下文和原文证据未保存")
	}
	for _, prompt := range writer.prompts {
		if strings.Contains(prompt, "你是剧本审校") && !strings.Contains(prompt, "前文只供承接") {
			t.Fatal("审校必须看到与导演相同的已确认承接")
		}
		if strings.Contains(prompt, "初期只有凡人") || strings.Contains(prompt, "年关前必须入门") {
			t.Fatal("旧概览不能倒灌后期剧情")
		}
	}
	for _, step := range repo.steps {
		if len(step.InputHash) != 64 {
			t.Fatal("新检查点必须记录完整输入指纹")
		}
	}
}
func checksFromPrompt(prompt string) []map[string]any {
	part := strings.SplitN(strings.SplitN(prompt, "累计历史约束：", 2)[1], "\n已确认承接信息：", 2)[0]
	var list []ReviewIssue
	_ = json.Unmarshal([]byte(part), &list)
	out := []map[string]any{}
	for _, issue := range list {
		if issue.Severity != "blocking" {
			continue
		}
		out = append(out, map[string]any{"id": issue.ID, "resolved": true})
	}
	return out
}
func TestGroundedRepairResumePreservesResolvedIssuesAndSkipsDirector(t *testing.T) {
	repo, _, _ := workflowFixture("甲走入屋中。他拿起信封。")
	id := repo.project.ID
	repo.batches[0].Status = "running"
	repo.batches[0].AnalysisRevision = 1
	repo.batches[0].AnalysisRunID = uuid.NewString()
	repo.batches[0].AnalysisContext = `{"version":3,"title":"短剧","model":"writer","overview":{}}`
	job := BatchAnalysisJob{Batch: repo.batches[0], OrganizationID: "org", OwnerID: "user", WorkflowVersion: 3, ModelCode: "writer"}
	writer := &groundedWriter{}
	reviewCount := 0
	directorCount := 0
	patchCount := 0
	writer.respond = func(prompt string) (string, error) {
		if strings.Contains(prompt, "你是漫剧工作流中心调度Agent") {
			return `{"node":"script_patch","targetItem":1,"reason":"续修当前集","instructions":"保留已解决约束，补上拿信动作","returnTo":"剧本审校"}`, nil
		}
		switch {
		case strings.Contains(prompt, "你是漫剧工作流中心调度Agent"):
			return `{"node":"script_patch","targetItem":1,"reason":"当前集存在局部事实遗漏","instructions":"保留已解决约束，修正当前问题","returnTo":"剧本审校"}`, nil
		case strings.HasPrefix(prompt, "生成"):
			directorCount++
			return `{"episodes":[{"title":"入屋","synopsis":"入屋","scriptText":"甲走出屋中。","characters":[],"scenes":[],"props":[]}]}`, nil
		case strings.Contains(prompt, "你是剧本修订编辑"):
			patchCount++
			if patchCount == 2 {
				return "", errors.New("模拟中断")
			}
			return `{"replacements":[{"oldText":"走出","newText":"走入"}]}`, nil
		default:
			reviewCount++
			if reviewCount == 1 {
				return `{"checks":[],"findings":[{"category":"fact","severity":"blocking","message":"应为走入","sourceQuote":"甲走入屋中","scriptQuote":"走出"}]}`, nil
			}
			return encoded(map[string]any{"checks": checksFromPrompt(prompt), "findings": []any{map[string]any{"category": "causality", "severity": "blocking", "message": "遗漏拿信", "sourceQuote": "他拿起信封", "scriptQuote": ""}}}), nil
		}
	}
	service := NewService(repo, writer)
	w := &batchWorkflow{service: service, job: job, ctx: context.Background(), saved: map[string]BatchStep{}}
	assets := BatchDraft{Characters: []AssetDraft{}, Scenes: []AssetDraft{}, Props: []AssetDraft{}}
	plan := EpisodePlan{Title: "入屋"}
	original := repo.project.SourceText
	base := "生成当前集"
	_, err := w.writeReviewedEpisode(1, 1, plan, assets, original, 0, base)
	if err == nil {
		t.Fatal("修正中断应保留失败")
	}
	if err = service.failBatch(job, err); err == nil {
		t.Fatal("应返回失败")
	}
	if len(repo.reviewIssues) != 2 || repo.reviewIssues[0].Status != "resolved" || repo.reviewIssues[1].Status != "open" {
		t.Fatalf("累计问题丢失: %+v", repo.reviewIssues)
	}
	job, err = service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	writer.respond = func(prompt string) (string, error) {
		if strings.Contains(prompt, "你是漫剧工作流中心调度Agent") {
			return `{"node":"script_patch","targetItem":1,"reason":"续修当前集","instructions":"保留已解决约束，补上拿信动作","returnTo":"剧本审校"}`, nil
		}
		if strings.Contains(prompt, "你是剧本修订编辑") {
			if !strings.Contains(prompt, "应为走入") || !strings.Contains(prompt, `"status":"resolved"`) {
				t.Fatal("必须保留此前修复约束")
			}
			return `{"replacements":[{"oldText":"甲走入屋中。","newText":"甲走入屋中。他拿起信封。"}]}`, nil
		}
		if strings.HasPrefix(prompt, "生成") {
			t.Fatal("续跑不得重新生成完成的导演正文")
		}
		return encoded(map[string]any{"checks": checksFromPrompt(prompt), "findings": []any{map[string]any{"category": "style", "severity": "blocking", "message": "措辞可更精炼", "sourceQuote": "", "scriptQuote": ""}}}), nil
	}
	w = &batchWorkflow{service: service, job: job, ctx: context.Background(), saved: map[string]BatchStep{}}
	for _, s := range repo.steps {
		w.saved[fmtStep(s.Stage, s.ChunkNumber)] = s
	}
	episode, err := w.writeReviewedEpisode(1, 1, plan, assets, original, 0, base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(episode.ScriptText, "走入") || !strings.Contains(episode.ScriptText, "拿起信封") || directorCount != 1 {
		t.Fatal("局部修正应保留旧成果")
	}
	w = &batchWorkflow{service: service, job: job, ctx: context.Background(), saved: map[string]BatchStep{}}
	for _, step := range repo.steps {
		w.saved[fmtStep(step.Stage, step.ChunkNumber)] = step
	}
	writer.respond = func(string) (string, error) {
		t.Fatal("已通过的剧集继续时不得重复调用模型")
		return "", nil
	}
	if _, err = w.writeReviewedEpisode(1, 1, plan, assets, original, 0, base); err != nil {
		t.Fatal(err)
	}
	for _, i := range repo.reviewIssues {
		if i.Category == "style" && i.Severity != "suggestion" {
			t.Fatal("表达建议不能阻断")
		}
		if i.Category != "style" && i.Status != "resolved" {
			t.Fatal("修正结果必须逐项复核")
		}
	}
}
func fmtStep(stage string, n int) string { return stage + ":" + strconv.Itoa(n) }
func TestReviewChecksCannotOmitResolvedConstraints(t *testing.T) {
	yes := true
	history := []ReviewIssue{{ID: "old", Status: "resolved", Severity: "blocking"}}
	if _, err := mergeReview(groundedReview{Checks: []reviewCheck{}, Findings: []reviewFinding{}}, history, EpisodeDraft{}, "原文", 0, 1, BatchAnalysisJob{}, "hash"); err == nil {
		t.Fatal("不能忽略历史已解决问题")
	}
	out, err := mergeReview(groundedReview{Checks: []reviewCheck{{ID: "old", Resolved: &yes}}, Findings: []reviewFinding{}}, history, EpisodeDraft{}, "原文", 0, 1, BatchAnalysisJob{}, "hash")
	if err != nil || len(blockingIssues(out)) != 0 {
		t.Fatal("已解决问题应保留通过状态")
	}
}

func TestResolvedIssueCanReopenAndPatchMustBeUnambiguous(t *testing.T) {
	no := false
	out, err := mergeReview(groundedReview{Checks: []reviewCheck{{ID: "old", Resolved: &no}}, Findings: []reviewFinding{}}, []ReviewIssue{{ID: "old", Status: "resolved", Severity: "blocking"}}, EpisodeDraft{}, "原文", 0, 1, BatchAnalysisJob{}, "hash")
	if err != nil || len(blockingIssues(out)) != 1 || out[0].Status != "open" {
		t.Fatal("旧问题重新出现时必须重新打开")
	}
	var patch scriptPatch
	_ = json.Unmarshal([]byte(`{"replacements":[{"oldText":"甲","newText":"乙"}]}`), &patch)
	if _, err = patchEpisode(EpisodeDraft{ScriptText: "甲甲"}, patch, BatchDraft{}, "甲甲"); err == nil {
		t.Fatal("不能把有歧义的替换应用到剧本")
	}
}
func TestChangedInputCannotReuseOldCheckpoint(t *testing.T) {
	repo, _, _ := workflowFixture("原文")
	repo.batches[0].Status = "running"
	repo.batches[0].AnalysisRevision = 1
	repo.batches[0].AnalysisRunID = uuid.NewString()
	job := BatchAnalysisJob{Batch: repo.batches[0], WorkflowVersion: 3, ModelCode: "writer"}
	writer := &groundedWriter{respond: func(string) (string, error) { return `{"value":"new"}`, nil }}
	w := &batchWorkflow{job: job, ctx: context.Background(), service: NewService(repo, writer), saved: map[string]BatchStep{"context:1": {Stage: "context", Status: "completed", ChunkTotal: 1, InputHash: inputFingerprint(job, "old"), ResultText: `{"value":"old"}`}}}
	validate := func(json.RawMessage) error { return nil }
	if _, err := w.runStep("context", 1, 1, "old", validate, false); err != nil || len(writer.prompts) != 0 {
		t.Fatal("相同输入应复用检查点")
	}
	raw, err := w.runStep("context", 1, 1, "new", validate, false)
	if err != nil || len(writer.prompts) != 1 || !strings.Contains(string(raw), "new") {
		t.Fatal("输入变化时必须重新处理，不能复用旧事实")
	}
}
