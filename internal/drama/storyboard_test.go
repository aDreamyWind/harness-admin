package drama

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestStoryboardValidationCoverageAndDialogue(t *testing.T) {
	lines := StoryboardLines("【书店 内 夜】\r\n林夏：门怎么开着？\r\n林夏推门。")
	good := StoryboardSegment{Title: "推门", StartLine: 1, EndLine: 3, FormIDs: []string{"form"}, Shots: []StoryboardShot{{Description: "林夏推门", ShotSize: "中景", Camera: "固定", Duration: 7, SourceLines: []int{1, 2, 3}, Utterances: []StoryboardUtterance{{Kind: "dialogue", Speaker: "林夏", Text: "门怎么开着？", SourceLine: 2}}}}}
	forms := []AssetForm{{ID: "form", Kind: "character", AssetName: "林夏"}}
	if err := validateStoryboardSegment(&good, lines, forms, true); err != nil {
		t.Fatal(err)
	}
	quoted := append([]string{}, lines...)
	quoted[1] = "林夏：\"门怎么开着？\""
	if err := validateStoryboardSegment(&good, quoted, forms, true); err != nil {
		t.Fatalf("外层引号不应被当成新增剧情: %v", err)
	}

	letter := []string{"信上写着：明日到钟楼。"}
	if storyboardSpeakerLine(letter[0], forms) {
		t.Fatal("书信冒号不应强制转成口播")
	}

	for _, tc := range []struct {
		name   string
		mutate func(*StoryboardSegment)
	}{
		{"越范围", func(s *StoryboardSegment) { s.Shots[0].SourceLines = []int{4} }},
		{"丢结尾", func(s *StoryboardSegment) { s.Shots[0].SourceLines = []int{1, 2} }},
		{"删台词", func(s *StoryboardSegment) { s.Shots[0].Utterances = nil }},
		{"新增台词", func(s *StoryboardSegment) { s.Shots[0].Utterances[0].Text = "我要逃跑！" }},
		{"假形象", func(s *StoryboardSegment) { s.FormIDs = []string{"不存在"} }},
		{"错声音类型", func(s *StoryboardSegment) { s.Shots[0].Utterances[0].Kind = "image" }},
		{"时长", func(s *StoryboardSegment) { s.Shots[0].Duration = 0 }},
		{"台词念不完", func(s *StoryboardSegment) { s.Shots[0].Duration = 0.5 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var copy StoryboardSegment
			b, _ := json.Marshal(good)
			json.Unmarshal(b, &copy)
			tc.mutate(&copy)
			if err := validateStoryboardSegment(&copy, lines, forms, true); err == nil {
				t.Fatal("错误输出未拒绝")
			}
		})
	}
	for _, segments := range [][]StoryboardSegment{{{Title: "a", StartLine: 2, EndLine: 3}}, {{Title: "a", StartLine: 1, EndLine: 2}, {Title: "b", StartLine: 2, EndLine: 3}}, {{Title: "a", StartLine: 1, EndLine: 2}}} {
		if validateStoryboardPlan(segments, 3) == nil {
			t.Fatal("规划丢行或重复未拒绝")
		}
	}
}

type sbRepo struct {
	StoryboardRepository
	run           StoryboardRun
	saved         []int
	pacingRepairs *repairMemory
}

func (r *sbRepo) ListStoryboards(context.Context, string, string, string, string) ([]StoryboardRun, error) {
	return []StoryboardRun{r.run}, nil
}
func (r *sbRepo) GetStoryboard(context.Context, string, string, string, string, string) (StoryboardRun, error) {
	return r.run, nil
}
func (r *sbRepo) ClaimStoryboard(context.Context, string) (bool, error) {
	r.run.Status = "running"
	return true, nil
}
func (r *sbRepo) StoryboardProgress(context.Context, string, string, string) error { return nil }
func (r *sbRepo) SaveStoryboardSegment(_ context.Context, _ string, s StoryboardSegment) error {
	r.saved = append(r.saved, s.Number)
	for i := range r.run.Segments {
		if r.run.Segments[i].Number == s.Number {
			r.run.Segments[i] = s
		}
	}
	return nil
}

func (r *sbRepo) SaveStoryboardPacingResult(ctx context.Context, run string, s StoryboardSegment, ticketID string, result RepairResult) error {
	if err := r.SaveStoryboardCandidate(ctx, run, s); err != nil {
		return err
	}
	if r.pacingRepairs != nil {
		return r.pacingRepairs.FinishRepairTicket(ctx, ticketID, result)
	}
	return nil
}
func (r *sbRepo) SaveStoryboardCandidate(_ context.Context, _ string, s StoryboardSegment) error {
	for i := range r.run.Segments {
		if r.run.Segments[i].Number == s.Number {
			s.Status = "pending"
			r.run.Segments[i] = s
		}
	}
	return nil
}
func (r *sbRepo) ReopenStoryboardFrom(_ context.Context, _ string, n int) error {
	for i := range r.run.Segments {
		if r.run.Segments[i].Number >= n {
			r.run.Segments[i].Status = "pending"
		}
	}
	return nil
}
func (r *sbRepo) ReviseStoryboardPlan(_ context.Context, _ string, s StoryboardSegment) error {
	for i := range r.run.Segments {
		if r.run.Segments[i].Number == s.Number {
			r.run.Segments[i] = s
		}
	}
	return nil
}
func (r *sbRepo) FinishStoryboard(_ context.Context, _ string, status, message string) error {
	r.run.Status, r.run.Error = status, message
	return nil
}

type sbSource struct {
	fakeRepository
	work VideoEpisodeWorkspace
}

func (r *sbSource) GetVideoEpisodeWorkspace(context.Context, string, string, string, string) (VideoEpisodeWorkspace, error) {
	return r.work, nil
}

type sbVisual struct{ VisualRepository }

func (r sbVisual) ReadCanvas(context.Context, string, string, string) (Canvas, error) {
	return Canvas{Forms: []AssetForm{}}, nil
}

type sbWriter struct {
	Writer
	calls int
	fail  bool
}

func (w *sbWriter) Complete(_ context.Context, _, _, _, prompt string) (string, error) {
	w.calls++
	if strings.Contains(prompt, "概览里的假剧情") {
		return "", errors.New("概览污染")
	}
	if w.fail {
		return "", errors.New("测试中断")
	}
	if strings.Contains(prompt, "审阅师") {
		return `{"blocking":[],"suggestions":[]}`, nil
	}
	return `{"formIds":[],"notes":"","shots":[{"description":"人物打开书","shotSize":"特写","camera":"固定","duration":3,"sourceLines":[2],"utterances":[]}]}`, nil
}
func TestStoryboardResumeReusesSavedSegmentsAndExcludesOverview(t *testing.T) {
	source := &sbSource{work: VideoEpisodeWorkspace{ScriptText: "人物进门。\n人物打开书。"}}
	source.project = Project{Overview: json.RawMessage(`{"synopsis":"概览里的假剧情"}`)}
	repo := &sbRepo{run: StoryboardRun{ID: uuid.NewString(), ProjectID: uuid.NewString(), EpisodeID: uuid.NewString(), ScriptText: source.work.ScriptText, SourceHash: storyboardHash(source.work), Segments: []StoryboardSegment{{ID: uuid.NewString(), Number: 1, Title: "进门", StartLine: 1, EndLine: 1, Status: "ready", EndState: "已进门"}, {ID: uuid.NewString(), Number: 2, Title: "开书", StartLine: 2, EndLine: 2, Status: "pending"}}}}
	writer := &sbWriter{fail: true}
	service := NewService(source, writer).WithVisuals(sbVisual{}, nil, nil).WithStoryboards(repo)
	job := StoryboardJob{Run: repo.run}
	if err := service.GenerateStoryboard(job); err == nil || repo.run.Status != "failed" || len(repo.saved) != 0 {
		t.Fatal("中断应保留已有片段")
	}
	writer.fail = false
	writer.calls = 0
	if err := service.GenerateStoryboard(job); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 2 || len(repo.saved) != 1 || repo.saved[0] != 2 || repo.run.Status != "ready" {
		t.Fatalf("不应重做已通过片段: calls=%d saved=%v", writer.calls, repo.saved)
	}
	source.work.ScriptText = "来源改变"
	if err := service.GenerateStoryboard(job); !errors.Is(err, ErrStoryboardConflict) {
		t.Fatalf("源变化应拒绝续跑: %v", err)
	}
}
