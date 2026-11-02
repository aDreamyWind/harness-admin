package drama

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrStoryboardConflict = errors.New("分镜正在处理、版本已变化或来源已变化，请刷新后重新拆分")

type StoryboardRun struct {
	ID          string              `json:"id"`
	EpisodeID   string              `json:"episodeId"`
	ProjectID   string              `json:"projectId"`
	Version     int                 `json:"version"`
	Revision    int                 `json:"revision"`
	Status      string              `json:"status"`
	Phase       string              `json:"phase"`
	Message     string              `json:"message"`
	Error       string              `json:"error"`
	ScriptText  string              `json:"scriptText"`
	SourceHash  string              `json:"-"`
	ModelCode   string              `json:"modelCode"`
	StyleID     string              `json:"styleId"`
	AspectRatio string              `json:"aspectRatio"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Segments    []StoryboardSegment `json:"segments"`
	Events      []StoryboardEvent   `json:"events"`
	Stale       bool                `json:"stale"`
}
type StoryboardSegment struct {
	ID         string              `json:"id"`
	Number     int                 `json:"number"`
	Title      string              `json:"title"`
	Scene      string              `json:"scene"`
	Purpose    string              `json:"purpose"`
	StartLine  int                 `json:"startLine"`
	EndLine    int                 `json:"endLine"`
	StartState string              `json:"startState"`
	EndState   string              `json:"endState"`
	Status     string              `json:"status"`
	Notes      string              `json:"notes"`
	FormIDs    []string            `json:"formIds"`
	Bindings   []StoryboardBinding `json:"bindings"`
	Shots      []StoryboardShot    `json:"shots"`
}
type StoryboardBinding struct {
	FormID  string `json:"formId"`
	ImageID string `json:"imageId"`
}
type StoryboardShot struct {
	ID          string                `json:"id"`
	Number      int                   `json:"number"`
	Description string                `json:"description"`
	ShotSize    string                `json:"shotSize"`
	Camera      string                `json:"camera"`
	Sound       string                `json:"sound"`
	Duration    float64               `json:"duration"`
	SourceLines []int                 `json:"sourceLines"`
	Utterances  []StoryboardUtterance `json:"utterances"`
}
type StoryboardUtterance struct {
	Kind       string `json:"kind"`
	Speaker    string `json:"speaker"`
	Text       string `json:"text"`
	SourceLine int    `json:"sourceLine"`
}
type StoryboardEvent struct {
	ID        int64     `json:"id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}
type StoryboardPage struct {
	Runs    []StoryboardRun `json:"runs"`
	Current *StoryboardRun  `json:"current"`
	Forms   []AssetForm     `json:"forms"`
}
type StoryboardJob struct {
	Run                     StoryboardRun
	OrganizationID, OwnerID string
}
type StoryboardEdit struct {
	Revision int                 `json:"revision"`
	Segments []StoryboardSegment `json:"segments"`
}
type StoryboardRepository interface {
	ListStoryboards(context.Context, string, string, string, string) ([]StoryboardRun, error)
	GetStoryboard(context.Context, string, string, string, string, string) (StoryboardRun, error)
	QueueStoryboard(context.Context, string, string, StoryboardRun, bool) (StoryboardRun, error)
	ClaimStoryboard(context.Context, string) (bool, error)
	SaveStoryboardPlan(context.Context, string, []StoryboardSegment) error
	SaveStoryboardSegment(context.Context, string, StoryboardSegment) error
	StoryboardProgress(context.Context, string, string, string) error
	FinishStoryboard(context.Context, string, string, string) error
	EditStoryboard(context.Context, string, string, StoryboardRun, int) (StoryboardRun, error)
	ConfirmStoryboard(context.Context, string, string, string, string, string, int) error
	FailInterruptedStoryboards(context.Context) error
}

func (s *Service) WithStoryboards(repo StoryboardRepository) *Service { s.storyboards = repo; return s }
func StoryboardLines(script string) []string {
	var result []string
	for _, line := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(script, "\r\n", "\n"), "\r", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}
func storyboardHash(w VideoEpisodeWorkspace) string {
	// 目录文案和人工概览不参与事实摘要；正文、实际关联及状态变化需要重新核对。
	type link struct{ ID, State string }
	refs := []link{}
	for _, group := range [][]EpisodeAsset{w.Characters, w.Scenes, w.Props} {
		for _, a := range group {
			refs = append(refs, link{a.AssetID, a.StateLabel})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	data, _ := json.Marshal(struct {
		Text  string
		Links []link
	}{w.ScriptText, refs})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (s *Service) Storyboards(ctx context.Context, org, user, project, episode, run string) (StoryboardPage, error) {
	out := StoryboardPage{Runs: []StoryboardRun{}, Forms: []AssetForm{}}
	if s.storyboards == nil || !validVisualIDs(project, episode) || (run != "" && !validVisualIDs(run)) {
		return out, ErrInvalidInput
	}
	w, err := s.VideoEpisodeWorkspace(ctx, org, user, project, episode)
	if err != nil {
		return out, err
	}
	canvas, err := s.Canvas(ctx, org, user, project)
	if err != nil {
		return out, err
	}
	allowed := map[string]bool{}
	for _, group := range [][]EpisodeAsset{w.Characters, w.Scenes, w.Props} {
		for _, a := range group {
			allowed[a.AssetID] = true
		}
	}
	for _, f := range canvas.Forms {
		if allowed[f.AssetID] {
			out.Forms = append(out.Forms, f)
		}
	}
	out.Runs, err = s.storyboards.ListStoryboards(ctx, org, user, project, episode)
	if err != nil {
		return out, err
	}
	if run == "" && len(out.Runs) > 0 {
		run = out.Runs[0].ID
	}
	if run != "" {
		r, e := s.storyboards.GetStoryboard(ctx, org, user, project, episode, run)
		if e != nil {
			return out, e
		}
		r.Stale = r.SourceHash != storyboardHash(w) || w.Episode.ScriptNeedsReview
		out.Current = &r
	}
	return out, nil
}
func (s *Service) StartStoryboard(ctx context.Context, org, user, project, episode string, resume bool) (StoryboardJob, error) {
	page, err := s.Storyboards(ctx, org, user, project, episode, "")
	if err != nil {
		return StoryboardJob{}, err
	}
	w, err := s.VideoEpisodeWorkspace(ctx, org, user, project, episode)
	if err != nil {
		return StoryboardJob{}, err
	}
	if w.Episode.ScriptNeedsReview || len(StoryboardLines(w.ScriptText)) == 0 {
		return StoryboardJob{}, ErrStoryboardConflict
	}
	run := StoryboardRun{ID: uuid.NewString(), EpisodeID: episode, ProjectID: project, ScriptText: w.ScriptText, SourceHash: storyboardHash(w)}
	if resume {
		if page.Current == nil || page.Current.Status != "failed" || page.Current.Stale {
			return StoryboardJob{}, ErrStoryboardConflict
		}
		run = *page.Current
	} else {
		settings, e := s.GetSettings(ctx, org, user)
		if e != nil {
			return StoryboardJob{}, e
		}
		run.ModelCode = settings.TextModelCode
		if run.ModelCode == "" {
			return StoryboardJob{}, ErrModelNotConfigured
		}
		p, e := s.Get(ctx, org, user, project)
		if e != nil {
			return StoryboardJob{}, e
		}
		run.StyleID, run.AspectRatio = p.StyleID, p.AspectRatio
	}
	models, err := s.AvailableModels(ctx, org, user)
	if err != nil {
		return StoryboardJob{}, err
	}
	allowed := false
	for _, m := range models {
		if m.ModelType == "chat" && m.Code == run.ModelCode {
			allowed = true
		}
	}
	if !allowed {
		return StoryboardJob{}, ErrModelUnavailable
	}
	run, err = s.storyboards.QueueStoryboard(ctx, org, user, run, resume)
	return StoryboardJob{Run: run, OrganizationID: org, OwnerID: user}, err
}
func (s *Service) EditStoryboard(ctx context.Context, org, user, project, episode, runID string, in StoryboardEdit) (StoryboardRun, error) {
	page, err := s.Storyboards(ctx, org, user, project, episode, runID)
	if err != nil {
		return StoryboardRun{}, err
	}
	if page.Current == nil || page.Current.Stale || page.Current.Status == "running" || page.Current.Status == "queued" || in.Revision < 1 {
		return StoryboardRun{}, ErrStoryboardConflict
	}
	r := *page.Current
	r.Segments = in.Segments
	if len(r.Segments) == 0 || len(r.Segments) > 200 {
		return r, ErrInvalidInput
	}
	lines := StoryboardLines(r.ScriptText)
	for i := range r.Segments {
		r.Segments[i].Number = i + 1
		if err = validateStoryboardSegment(&r.Segments[i], lines, page.Forms, false); err != nil {
			return r, fmt.Errorf("%w：%v", ErrInvalidInput, err)
		}
	}
	return s.storyboards.EditStoryboard(ctx, org, user, r, in.Revision)
}
func (s *Service) ConfirmStoryboard(ctx context.Context, org, user, project, episode, runID string, revision int) error {
	p, err := s.Storyboards(ctx, org, user, project, episode, runID)
	if err != nil {
		return err
	}
	if p.Current == nil || p.Current.Stale || p.Current.Status != "ready" || revision < 1 {
		return ErrStoryboardConflict
	}
	return s.storyboards.ConfirmStoryboard(ctx, org, user, project, episode, runID, revision)
}
func validateStoryboardSegment(seg *StoryboardSegment, lines []string, forms []AssetForm, strict bool) error {
	if strings.TrimSpace(seg.Title) == "" || utf8.RuneCountInString(seg.Title) > 120 || seg.StartLine < 1 || seg.EndLine > len(lines) || seg.EndLine < seg.StartLine || len(seg.Shots) == 0 || len(seg.Shots) > 100 {
		return errors.New("片段标题、来源范围或镜头数量不正确")
	}
	valid := map[string]bool{}
	for _, f := range forms {
		valid[f.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range seg.FormIDs {
		if !valid[id] || seen[id] {
			return errors.New("引用了不存在、重复或本集未关联的资产形象")
		}
		seen[id] = true
	}
	coverage := map[int]bool{}
	spoken := map[int]string{}
	for i := range seg.Shots {
		shot := &seg.Shots[i]
		shot.Number = i + 1
		if strings.TrimSpace(shot.Description) == "" || len(shot.Description) > 30000 || strings.TrimSpace(shot.ShotSize) == "" || strings.TrimSpace(shot.Camera) == "" || math.IsNaN(shot.Duration) || math.IsInf(shot.Duration, 0) || shot.Duration < 0.5 || shot.Duration > 60 || len(shot.SourceLines) == 0 {
			return errors.New("镜头缺少画面、景别、运镜、有效时长或剧本来源")
		}
		refs := map[int]bool{}
		for _, n := range shot.SourceLines {
			if n < seg.StartLine || n > seg.EndLine || refs[n] {
				return errors.New("镜头来源越过本片段或重复")
			}
			refs[n] = true
			coverage[n] = true
		}
		speechSeconds := 0.0
		for _, u := range shot.Utterances {
			if !refs[u.SourceLine] || strings.TrimSpace(u.Text) == "" || (u.Kind != "dialogue" && u.Kind != "os" && u.Kind != "vo") {
				return errors.New("台词来源或声音类型不正确")
			}
			if strict && !strings.Contains(lines[u.SourceLine-1], u.Text) {
				return errors.New("台词必须来自本集剧本，不得编造或改写")
			}
			spoken[u.SourceLine] += u.Text
			speechSeconds += estimateStoryboardSpeech(u.Text) + 0.5
		}

		if strict && speechSeconds > shot.Duration+0.25 {
			return fmt.Errorf("镜头%d台词按普通语速约需%.1f秒，超过规划%.1f秒，请延长或按语义拆镜，不删台词", i+1, speechSeconds, shot.Duration)
		}
	}
	if strict {
		for n := seg.StartLine; n <= seg.EndLine; n++ {
			if !coverage[n] {
				return fmt.Errorf("遗漏剧本第%d行", n)
			}
			if text := storyboardDialogue(lines[n-1]); text != "" && spoken[n] != text {
				return fmt.Errorf("第%d行台词未完整保留或重复，请按原文顺序分配声音", n)
			}
		}
	}
	seg.Status = "ready"
	return nil
}
func storyboardDialogue(line string) string {
	if strings.HasPrefix(line, "【") || strings.HasPrefix(line, "第") || strings.HasPrefix(line, "△") || strings.HasPrefix(line, "▲") {
		return ""
	}
	at := strings.IndexAny(line, "：:")
	if at <= 0 || utf8.RuneCountInString(line[:at]) > 25 {
		return ""
	}
	// 行首为说话者的剧本行；画面标签和场次说明不是口播。
	label := line[:at]
	for _, skip := range []string{"场景", "时间", "地点", "画面", "动作", "分镜", "音效", "字幕", "角色", "出场", "剧情"} {
		if strings.Contains(label, skip) {
			return ""
		}
	}
	_, size := utf8.DecodeRuneInString(line[at:])
	return strings.TrimSpace(line[at+size:])
}

// 中文按约4字/秒、拉丁语按约3词/秒估算，停顿另计；是可检查的规划下界，不是模型能力承诺。
func estimateStoryboardSpeech(text string) float64 {
	cjk, words := 0, 0
	inWord := false
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			cjk++
			inWord = false
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !inWord {
				words++
			}
			inWord = true
		} else {
			inWord = false
		}
	}
	return float64(cjk)/4 + float64(words)/3
}
