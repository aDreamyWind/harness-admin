package drama

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestStoryboardReviewAcceptsObjectsAndLegacyStrings(t *testing.T) {
	for _, raw := range []string{
		`{"blocking":[],"suggestions":[]}`,
		`{"blocking":["保留台词"],"suggestions":["建议"]}`,
		`{"blocking":[{"shot":2,"field":"duration","message":"时长不足","evidence":"长台词","fix":"延长"}],"suggestions":[{"shot":1,"suggestion":"可用近景"}]}`,
	} {
		var report storyboardReviewReport
		if err := decodeStoryboard(raw, &report); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"blocking":null,"suggestions":[]}`, `{"blocking":[{}],"suggestions":[]}`, `{"blocking":[7],"suggestions":[]}`} {
		var report storyboardReviewReport
		if err := decodeStoryboard(raw, &report); err == nil {
			t.Fatal("应拒绝无内容的审阅", raw)
		}
	}
}
func TestStoryboardMalformedReviewOnlyReturnsToReviewer(t *testing.T) {
	source := &sbSource{work: VideoEpisodeWorkspace{ScriptText: "人物开门。"}}
	repo := &sbRepo{run: StoryboardRun{ID: uuid.NewString(), ProjectID: uuid.NewString(), EpisodeID: uuid.NewString(), ScriptText: source.work.ScriptText, SourceHash: storyboardHash(source.work), Segments: []StoryboardSegment{{ID: uuid.NewString(), Number: 1, Title: "开门", StartLine: 1, EndLine: 1, Status: "pending", FormIDs: []string{}, Shots: []StoryboardShot{{Description: "人物开门", ShotSize: "中景", Camera: "固定", Duration: 3, SourceLines: []int{1}}}}}}}
	reviews := 0
	writer := &groundedWriter{respond: func(prompt string) (string, error) {
		if !strings.HasPrefix(prompt, "你是分镜审阅师") {
			t.Fatal("格式错误不应调用编写或AI路由节点")
		}
		reviews++
		if reviews == 1 {
			return `{"blocking":{}}`, nil
		}
		if !strings.Contains(prompt, "review_format") || !strings.Contains(prompt, `{"blocking":{}}`) {
			t.Fatal("审阅修正必须看到格式问题和原始输出")
		}
		return `{"blocking":[],"suggestions":[{"shot":1,"message":"可以缩小景别"}]}`, nil
	}}
	repairs := &repairMemory{}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo).WithRepairCoordinator(repairs)
	if err := service.GenerateStoryboard(StoryboardJob{Run: repo.run}); err != nil {
		t.Fatal(err)
	}
	if reviews != 2 || len(repairs.tickets) != 1 || repairs.tickets[0].Node != "reviewer" || repairs.tickets[0].Status != "resolved" || !strings.Contains(repo.run.Segments[0].Notes, "可以缩小景别") {
		t.Fatal("未正确回审阅节点", repairs.tickets)
	}
}
func TestSchedulerFillsFreeSlotWithoutWaitingForSlowPeer(t *testing.T) {
	_, _, service := workflowFixture("甲")
	w := &batchWorkflow{service: service, ctx: context.Background()}
	slowStarted, thirdStarted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- w.schedule([]workflowTask{
			{run: func() error { close(slowStarted); <-release; return nil }},
			{run: func() error { <-slowStarted; return nil }},
			{run: func() error { close(thirdStarted); return nil }},
		})
	}()
	select {
	case <-thirdStarted:
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("空位未被第三个任务立即补充")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestSchedulerFailureDrainsInFlightAndStopsDispatch(t *testing.T) {
	_, _, service := workflowFixture("甲")
	w := &batchWorkflow{service: service, ctx: context.Background()}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- w.schedule([]workflowTask{
			{run: func() error { <-started; return errors.New("失败") }},
			{run: func() error { close(started); <-release; return nil }},
			{run: func() error { t.Error("失败后不可继续派发"); return nil }},
		})
	}()
	select {
	case <-done:
		t.Fatal("不可留下仍在写库的孤儿任务")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("应返回失败")
	}
}
func TestDirectorAndNamePromptsDoNotRepeatSourceQuotes(t *testing.T) {
	p := EpisodePlan{Title: "归店", Synopsis: "雨夜归来", Hook: "发现信件", FactIDs: []string{"U1F1"}, Sources: []SourceFact{{ID: "U1F1", Claim: "门开着", Quote: "重复长引用", References: []SourceReference{{Quote: "重复长引用"}}}}}
	raw, _ := json.Marshal(directorPlan(p))
	if strings.Contains(string(raw), "重复长引用") || !strings.Contains(string(raw), "U1F1") || !strings.Contains(string(raw), "门开着") {
		t.Fatal(string(raw))
	}
	plan, _ := json.Marshal(episodePlanResult{Episodes: []EpisodePlan{p}})
	name, err := batchNamingContext(string(plan))
	if err != nil || strings.Contains(name, "重复长引用") || strings.Contains(name, "U1F1") || !strings.Contains(name, "发现信件") {
		t.Fatal(name, err)
	}
	if p.Sources[0].Quote != "重复长引用" {
		t.Fatal("不可修改后台证据")
	}
}
