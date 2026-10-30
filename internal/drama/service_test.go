package drama

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRepository struct {
	project    Project
	settings   Settings
	stages     []string
	failed     string
	batches    []Batch
	batchDraft BatchDraft
	steps      []BatchStep
	events     []BatchEvent
}

func (r *fakeRepository) GetSettings(_ context.Context, _, _ string) (Settings, error) {
	return r.settings, nil
}
func (r *fakeRepository) SaveSettings(_ context.Context, input Settings) (Settings, error) {
	r.settings = input
	return input, nil
}

func (r *fakeRepository) Create(_ context.Context, item Project) (Project, error) {
	r.project = item
	return item, nil
}
func (r *fakeRepository) List(_ context.Context, _, _ string) ([]Project, error) {
	return []Project{r.project}, nil
}
func (r *fakeRepository) Get(_ context.Context, _, _, _ string) (Project, error) {
	return r.project, nil
}
func (r *fakeRepository) RetryFailed(_ context.Context, _, _, _, modelCode string) (Project, error) {
	if r.project.Status != "failed" || r.project.SourceMode != "upload" {
		return Project{}, ErrNotFound
	}
	r.project.Status, r.project.Phase, r.project.Error, r.project.ModelCode = "queued", "waiting", "", modelCode
	r.project.Characters, r.project.Scenes, r.project.Props, r.project.Episodes = json.RawMessage(`[]`), json.RawMessage(`[]`), json.RawMessage(`[]`), json.RawMessage(`[]`)
	return r.project, nil
}
func (r *fakeRepository) QueueOverviewReparse(_ context.Context, _, _, _, modelCode string) (Project, error) {
	if r.project.Status != "ready" && r.project.Status != "failed" {
		return Project{}, ErrNotFound
	}
	r.project.Status, r.project.Phase, r.project.Error, r.project.ModelCode = "queued", "waiting", "", modelCode
	return r.project, nil
}
func (r *fakeRepository) GetSource(_ context.Context, _, _, _ string) (string, error) {
	return r.project.SourceText, nil
}
func (r *fakeRepository) GetSourceRange(_ context.Context, _, _, _ string, start, limit int) (SourceRange, error) {
	source := []rune(r.project.SourceText)
	if start > len(source) {
		return SourceRange{}, ErrInvalidInput
	}
	end := min(start+limit, len(source))
	return SourceRange{StartOffset: start, EndOffset: end, TotalLength: len(source), Text: string(source[start:end])}, nil
}
func (r *fakeRepository) ListBatches(_ context.Context, _, _, _ string) ([]Batch, error) {
	return append([]Batch{}, r.batches...), nil
}
func (r *fakeRepository) GetBatch(_ context.Context, _, _, _, id string) (Batch, error) {
	for _, item := range r.batches {
		if item.ID == id {
			return item, nil
		}
	}
	return Batch{}, ErrNotFound
}
func (r *fakeRepository) CreateBatch(_ context.Context, _, _ string, item Batch) (Batch, error) {
	item.Number = len(r.batches) + 1
	r.batches = append(r.batches, item)
	return item, nil
}
func (r *fakeRepository) UpdateBatchRange(_ context.Context, _, _, _, id string, input BatchRangeChange) (Batch, error) {
	for index, item := range r.batches {
		if item.ID == id {
			if item.Revision != input.Revision {
				return Batch{}, ErrBatchConflict
			}
			item.StartOffset, item.EndOffset, item.Revision, item.Boundary = input.StartOffset, input.EndOffset, item.Revision+1, "manual"
			r.batches[index] = item
			return item, nil
		}
	}
	return Batch{}, ErrNotFound
}
func (r *fakeRepository) QueueBatchAnalysis(_ context.Context, _, _, _, id string, options BatchRunOptions) (Batch, error) {
	for i, item := range r.batches {
		if item.ID == id {
			if options.Resume && (item.Status != "failed" || item.AnalysisRevision != item.Revision) {
				return Batch{}, ErrResumeNotAllowed
			}
			if !options.Resume {
				r.steps = nil
				r.events = nil
				item.AnalysisContext = options.ContextText
			}
			item.Status = "running"
			item.AnalysisRevision = item.Revision
			r.batches[i] = item
			return item, nil
		}
	}
	return Batch{}, ErrNotFound
}
func (r *fakeRepository) ListBatchSteps(_ context.Context, _, _, _, _ string) ([]BatchStep, error) {
	return append([]BatchStep{}, r.steps...), nil
}
func (r *fakeRepository) StartBatchStep(_ context.Context, _, _, _, _ string, _ int, step BatchStep) error {
	step.Status = "running"
	for i, old := range r.steps {
		if old.Stage == step.Stage && old.ChunkNumber == step.ChunkNumber {
			r.steps[i] = step
			return nil
		}
	}
	r.steps = append(r.steps, step)
	return nil
}
func (r *fakeRepository) CompleteBatchStep(_ context.Context, _, _, _, _ string, _ int, step BatchStep) error {
	step.Status = "completed"
	for i, old := range r.steps {
		if old.Stage == step.Stage && old.ChunkNumber == step.ChunkNumber {
			r.steps[i] = step
			return nil
		}
	}
	return ErrNotFound
}
func (r *fakeRepository) SaveBatchResult(_ context.Context, _, _, _, id string, revision int, draft BatchDraft) error {
	for i, item := range r.batches {
		if item.ID == id {
			if item.Revision != revision {
				return ErrBatchConflict
			}
			item.Status = "ready"
			r.batches[i] = item
			r.batchDraft = draft
			return nil
		}
	}
	return ErrNotFound
}
func (r *fakeRepository) FailBatchAnalysis(_ context.Context, _, _, _, id string, _ int, reason string) error {
	for i, item := range r.batches {
		if item.ID == id {
			item.Status = "failed"
			item.ErrorMessage = reason
			r.batches[i] = item
			return nil
		}
	}
	return ErrNotFound
}
func (r *fakeRepository) GetBatchResult(_ context.Context, _, _, _, _ string) (BatchResult, error) {
	return BatchResult{}, nil
}
func (r *fakeRepository) ListProjectAssets(_ context.Context, _, _, _, _, _ string) ([]Asset, error) {
	return []Asset{}, nil
}
func (r *fakeRepository) AddEpisodeAsset(_ context.Context, _, _, _, _, _, _ string) (EpisodeAsset, error) {
	return EpisodeAsset{}, nil
}
func (r *fakeRepository) RemoveEpisodeAsset(_ context.Context, _, _, _, _, _ string) error {
	return nil
}
func (r *fakeRepository) FailInterruptedBatches(context.Context) error { return nil }
func (r *fakeRepository) UpdateOverview(_ context.Context, _, _, _ string, value json.RawMessage) (Project, error) {
	r.project.Overview = value
	return r.project, nil
}
func (r *fakeRepository) FailInterrupted(context.Context) error { return nil }
func (r *fakeRepository) SaveGeneratedScript(_ context.Context, _, _, _, script string) error {
	r.project.SourceText = script
	return nil
}
func (r *fakeRepository) SaveAnalysis(_ context.Context, _, _, _, status, phase string, data json.RawMessage) error {
	r.stages = append(r.stages, phase)
	r.project.Status, r.project.Phase = status, phase
	if len(data) > 0 {
		switch phase {
		case "story":
			r.project.Overview = data
		case "assets":
			var result struct{ Characters, Scenes, Props json.RawMessage }
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			r.project.Characters, r.project.Scenes, r.project.Props = result.Characters, result.Scenes, result.Props
		case "episodes":
			var result struct{ Episodes json.RawMessage }
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			r.project.Episodes = result.Episodes
		}
	}
	return nil
}
func (r *fakeRepository) FailAnalysis(_ context.Context, _, _, _, message string) error {
	r.failed = message
	r.project.Status = "failed"
	return nil
}

type fakeWriter struct {
	prompts []string
	replies []string
	errors  []error
}

func (w *fakeWriter) AvailableModels(context.Context, string, string) ([]Model, error) {
	return []Model{
		{Code: "writer", Name: "编剧模型", ModelType: "chat"},
		{Code: "painter", Name: "图片模型", ModelType: "image"},
		{Code: "director", Name: "视频模型", ModelType: "video"},
	}, nil
}
func (w *fakeWriter) Complete(_ context.Context, _, _, _ string, prompt string) (string, error) {
	w.prompts = append(w.prompts, prompt)
	if len(w.errors) > 0 {
		err := w.errors[0]
		w.errors = w.errors[1:]
		if err != nil {
			return "", err
		}
	}
	if len(w.replies) == 0 {
		return "", errors.New("无响应")
	}
	result := w.replies[0]
	w.replies = w.replies[1:]
	return result, nil
}

func TestCreateAndAnalyzePreservesFullSource(t *testing.T) {
	source := strings.Repeat("山", ParseRuneLimit-1) + "界" + strings.Repeat("海", 456)
	repo := &fakeRepository{settings: Settings{TextModelCode: "writer"}}
	writer := &fakeWriter{replies: append(overviewChunkReplies((ParseRuneLimit+OverviewChunkRuneLimit-1)/OverviewChunkRuneLimit),
		`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"阿青","genre":"奇幻","synopsis":"寻找故乡","background":"古代","setting":"画中世界"}`)}
	service := NewService(repo, writer)
	project, err := service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "画中人", SourceText: source, Style: "国风", AspectRatio: "9:16"})
	if err != nil {
		t.Fatal(err)
	}
	if project.SourceLength != ParseRuneLimit+456 || repo.project.SourceText != source {
		t.Fatal("完整原文未保存")
	}
	if err := service.Analyze(project); err != nil {
		t.Fatal(err)
	}
	if repo.project.Status != "ready" || repo.project.Phase != "overview_done" || len(writer.prompts) != 1+(ParseRuneLimit+OverviewChunkRuneLimit-1)/OverviewChunkRuneLimit {
		t.Fatalf("概览解析阶段错误: %+v, 调用次数 %d", repo.project, len(writer.prompts))
	}
	if !strings.Contains(writer.prompts[len(writer.prompts)-2], "界") || strings.Contains(strings.Join(writer.prompts, ""), "海") {
		t.Fatal("事实提取没有准确覆盖原文前十万字")
	}
	if validateStage("story", repo.project.Overview) != nil || string(repo.project.Characters) != "[]" || string(repo.project.Episodes) != "[]" {
		t.Fatal("上传后应只保存概览，资产和分集留到剧本解析阶段")
	}
}

func TestGenerateScriptBeforeAnalysis(t *testing.T) {
	repo := &fakeRepository{settings: Settings{TextModelCode: "writer"}}
	writer := &fakeWriter{replies: []string{
		`{"script":"第一场：夜，画室。"}`,
		`{"videoStyle":"国风","aspectRatio":"16:9","protagonist":"画师","genre":"悬疑","synopsis":"谜题","background":"现代","setting":"画室"}`,
	}}
	service := NewService(repo, writer)
	project, err := service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "画室", SourceText: "一个画师的故事", SourceMode: "generate", Style: "二维动漫", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Analyze(project); err != nil {
		t.Fatal(err)
	}
	if repo.project.SourceText != "第一场：夜，画室。" || len(writer.prompts) != 2 || !strings.Contains(writer.prompts[1], repo.project.SourceText) || repo.project.Phase != "overview_done" {
		t.Fatal("没有先生成剧本再解析概览")
	}
}

func TestCreateRequiresConfiguredAvailableModel(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo, &fakeWriter{})
	_, err := service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "测试", SourceText: "内容", Style: "国风", AspectRatio: "9:16"})
	if !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("应该要求先配置模型，得到 %v", err)
	}
	repo.settings.TextModelCode = "disabled"
	_, err = service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "测试", SourceText: "内容", Style: "国风", AspectRatio: "9:16"})
	if !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("应该拒绝未授权模型，得到 %v", err)
	}
}

func TestSaveSettingsValidatesModelTypes(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo, &fakeWriter{})
	_, err := service.SaveSettings(context.Background(), Settings{OrganizationID: "org", UserID: "user", ImageModelCode: "writer"})
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("文本模型不应保存为图片模型: %v", err)
	}
	saved, err := service.SaveSettings(context.Background(), Settings{OrganizationID: "org", UserID: "user", TextModelCode: "writer", ImageModelCode: "painter", VideoModelCode: "director"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.TextModelCode != "writer" || repo.settings.ImageModelCode != "painter" || repo.settings.VideoModelCode != "director" {
		t.Fatal("当前用户的默认模型未保存")
	}
}

func TestRetryFailedUploadReusesSavedSource(t *testing.T) {
	id := "b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	repo := &fakeRepository{
		project:  Project{ID: id, Status: "failed", SourceMode: "upload", SourceText: "已保存的完整原文", Error: "旧错误", ModelCode: "old", Overview: json.RawMessage(`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"阿青","genre":"奇幻","synopsis":"寻找故乡","background":"古代","setting":"画中世界"}`)},
		settings: Settings{TextModelCode: "writer"},
	}
	service := NewService(repo, &fakeWriter{})
	project, err := service.Retry(context.Background(), "org", "user", id)
	if err != nil {
		t.Fatal(err)
	}
	if project.Status != "queued" || project.Phase != "waiting" || project.SourceText != "已保存的完整原文" || project.ModelCode != "writer" || project.Error != "" || len(project.Overview) == 0 {
		t.Fatalf("重新解析未复用原文或清除旧状态: %+v", project)
	}
	if _, err := service.Retry(context.Background(), "org", "user", id); !errors.Is(err, ErrRetryNotAllowed) {
		t.Fatalf("非失败项目不能重复排队: %v", err)
	}
	repo.project.Status, repo.project.SourceMode = "failed", "generate"
	if _, err := service.Retry(context.Background(), "org", "user", id); !errors.Is(err, ErrRetryNotAllowed) {
		t.Fatalf("AI 生成剧本项目不应直接重新解析: %v", err)
	}
}

func TestAnalyzeRetrySkipsCompletedOverview(t *testing.T) {
	repo := &fakeRepository{project: Project{
		ID: "b6e6da1c-7c27-4f72-babf-34a7c4e19617", OrganizationID: "org", OwnerID: "user",
		Title: "画中人", SourceMode: "upload", SourceText: "完整原文", ModelCode: "writer",
		Status: "queued", Overview: json.RawMessage(`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"阿青","genre":"奇幻","synopsis":"寻找故乡","background":"古代","setting":"画中世界"}`),
	}}
	writer := &fakeWriter{}
	if err := NewService(repo, writer).Analyze(repo.project); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 0 || len(repo.stages) != 1 || repo.stages[0] != "overview_done" || repo.project.Status != "ready" {
		t.Fatalf("重试应复用已有概览并结束自动解析: 调用 %d 次，阶段 %v", len(writer.prompts), repo.stages)
	}
}

func TestUpdateOverviewValidatesSelectionFields(t *testing.T) {
	repo := &fakeRepository{project: Project{ID: "b6e6da1c-7c27-4f72-babf-34a7c4e19617"}}
	service := NewService(repo, &fakeWriter{})
	for _, value := range []json.RawMessage{
		json.RawMessage(`{"aspectRatio":"1:1"}`),
		json.RawMessage(`{"videoStyle":"` + strings.Repeat("风", 81) + `"}`),
	} {
		if _, err := service.UpdateOverview(context.Background(), "org", "user", repo.project.ID, value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("无效选择项应被拒绝: %s %v", value, err)
		}
	}
}

func TestReanalyzeOverviewUsesSavedSourceAndReplacesResult(t *testing.T) {
	id := "b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	old := json.RawMessage(`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"旧主角","genre":"奇幻","synopsis":"旧梗概","background":"旧背景","setting":"旧设定"}`)
	source := strings.Repeat("山", ParseRuneLimit) + "末尾不应传给模型"
	repo := &fakeRepository{project: Project{ID: id, OrganizationID: "org", OwnerID: "user", Status: "ready", SourceMode: "upload", Title: "长篇", SourceText: source, SourceLength: len([]rune(source)), ModelCode: "old", Overview: old}, settings: Settings{TextModelCode: "writer"}}
	writer := &fakeWriter{replies: append(overviewChunkReplies((ParseRuneLimit+OverviewChunkRuneLimit-1)/OverviewChunkRuneLimit),
		`{"videoStyle":"电影写实","aspectRatio":"9:16","protagonist":"新主角","genre":"冒险","synopsis":"新梗概","background":"新背景","setting":"新设定"}`)}
	service := NewService(repo, writer)
	queued, err := service.ReanalyzeOverview(context.Background(), "org", "user", id)
	if err != nil {
		t.Fatal(err)
	}
	if !queued.ForceOverview || queued.Status != "queued" || queued.ModelCode != "writer" || string(repo.project.Overview) != string(old) {
		t.Fatalf("重新解析排队时应保留旧概览并使用当前模型: %+v", queued)
	}
	if _, err := service.ReanalyzeOverview(context.Background(), "org", "user", id); !errors.Is(err, ErrRetryNotAllowed) {
		t.Fatalf("排队中不能重复解析: %v", err)
	}
	if err := service.Analyze(queued); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 1+(ParseRuneLimit+OverviewChunkRuneLimit-1)/OverviewChunkRuneLimit || !strings.Contains(writer.prompts[len(writer.prompts)-2], strings.Repeat("山", OverviewChunkRuneLimit)) || strings.Contains(strings.Join(writer.prompts, ""), "末尾不应传给模型") {
		t.Fatal("概览应读取原文前十万字")
	}
	var result map[string]string
	if err := json.Unmarshal(repo.project.Overview, &result); err != nil {
		t.Fatal(err)
	}
	if result["protagonist"] != "新主角" || result["synopsis"] != "新梗概" || repo.project.Status != "ready" {
		t.Fatalf("旧结果没有被替换: %+v", repo.project)
	}
}

func TestOverviewChunkWithoutEventsKeepsOtherFacts(t *testing.T) {
	id := "b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	source := strings.Repeat("山", OverviewChunkRuneLimit*4)
	repo := &fakeRepository{project: Project{
		ID: id, OrganizationID: "org", OwnerID: "user", Status: "queued",
		SourceMode: "upload", Title: "长篇", SourceText: source, SourceLength: len([]rune(source)),
		Style: "国风", AspectRatio: "9:16", ModelCode: "writer", Overview: json.RawMessage(`{}`),
	}}
	replies := overviewChunkReplies(4)
	replies[3] = `{"events":"","characters":"","backgroundFacts":"第四段交代城邦制度","settingFacts":"","unresolved":""}`
	writer := &fakeWriter{replies: append(replies,
		`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"主角","genre":"奇幻","synopsis":"故事梗概","background":"城邦制度","setting":"设定"}`)}
	if err := NewService(repo, writer).Analyze(repo.project); err != nil {
		t.Fatal(err)
	}
	if repo.project.Status != "ready" || repo.project.Phase != "overview_done" {
		t.Fatalf("含背景事实的片段不应因 events 为空而失败: %+v", repo.project)
	}
	if !strings.Contains(writer.prompts[len(writer.prompts)-1], "第四段交代城邦制度") {
		t.Fatal("最终概览应包含第四段提取到的事实")
	}
}

func TestOverviewEmptyChunkFallsBackToSourceExcerpt(t *testing.T) {
	repo := &fakeRepository{project: Project{
		ID: "b6e6da1c-7c27-4f72-babf-34a7c4e19617", OrganizationID: "org", OwnerID: "user",
		Status: "queued", SourceMode: "upload", Title: "长篇", SourceText: strings.Repeat("山", OverviewChunkRuneLimit+1),
		SourceLength: OverviewChunkRuneLimit + 1, Style: "国风", AspectRatio: "9:16", ModelCode: "writer", Overview: json.RawMessage(`{}`),
	}}
	writer := &fakeWriter{replies: []string{
		`{}`,
		`{"events":"末尾出现新角色"}`,
		`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"主角","genre":"奇幻","synopsis":"故事梗概","background":"背景","setting":"设定"}`,
	}}
	if err := NewService(repo, writer).Analyze(repo.project); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(writer.prompts[len(writer.prompts)-1], "事实提取为空，以下为原文节选") {
		t.Fatal("空片段应使用原文节选继续解析")
	}
}

func TestReanalyzeOverviewRejectsMissingOverviewAndModel(t *testing.T) {
	id := "b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	repo := &fakeRepository{project: Project{ID: id, Status: "ready", SourceLength: 3, Overview: json.RawMessage(`{}`)}}
	service := NewService(repo, &fakeWriter{})
	if _, err := service.ReanalyzeOverview(context.Background(), "org", "user", id); !errors.Is(err, ErrRetryNotAllowed) {
		t.Fatalf("无完整概览不应重新解析: %v", err)
	}
	repo.project.Overview = json.RawMessage(`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"甲","genre":"奇幻","synopsis":"梗概","background":"背景","setting":"设定"}`)
	if _, err := service.ReanalyzeOverview(context.Background(), "org", "user", id); !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("未配置模型应拒绝: %v", err)
	}
}

func overviewChunkReplies(count int) []string {
	replies := make([]string, count)
	for i := range replies {
		replies[i] = `{"events":"主角采取行动，处境发生变化","characters":"主角","backgroundFacts":"","settingFacts":"","unresolved":""}`
	}
	return replies
}

func TestAnalyzeBatchStoresEpisodesAndAssets(t *testing.T) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	repo := &fakeRepository{project: Project{ID: id, Title: "测试剧", Status: "ready", SourceText: "第一章\n张三走入竹林。", Overview: json.RawMessage(`{"synopsis":"寻找竹林"}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, StartOffset: 0, Revision: 1}}}
	repo.batches[0].EndOffset = len([]rune(repo.project.SourceText))
	art := `{"characters":[{"name":"张三","description":"主角","appearanceDescription":"黑发青衣","voiceDescription":"低沉清亮"}],"scenes":[{"name":"竹林","description":"夜晚的竹林"}],"props":[]}`
	director := `{"episodes":[{"title":"入林","synopsis":"张三进入竹林","scriptText":"【竹林 外 夜】\n张三走入竹林。","characters":[{"name":"张三","stateLabel":"默认形象"}],"scenes":[{"name":"竹林","stateLabel":"夜晚"}],"props":[]}]}`
	writer := &fakeWriter{replies: []string{art, director}}
	service := NewService(repo, writer)
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.analyzeLegacyBatch(job); err != nil {
		t.Fatal(err)
	}
	if repo.batches[0].Status != "ready" || len(repo.batchDraft.Episodes) != 1 || len(repo.batchDraft.Characters) != 1 || repo.batchDraft.Characters[0].VoiceDescription != "低沉清亮" {
		t.Fatalf("两阶段结果未保存: %+v", repo.batchDraft)
	}
	if len(writer.prompts) != 2 || !strings.Contains(writer.prompts[0], "美术指导") || !strings.Contains(writer.prompts[1], "漫剧导演") || !strings.Contains(writer.prompts[1], "黑发青衣") {
		t.Fatal("应先提取角色形象与音色，再提供给导演拆集")
	}
}

func TestAnalyzeBatchRetriesTimedOutArtSegment(t *testing.T) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	repo := &fakeRepository{project: Project{ID: id, Title: "短剧", Status: "ready", SourceText: "第一章\n张三走入竹林。", Overview: json.RawMessage(`{}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, StartOffset: 0, Revision: 1}}}
	repo.batches[0].EndOffset = len([]rune(repo.project.SourceText))
	art := `{"characters":[],"scenes":[],"props":[]}`
	director := `{"episodes":[{"title":"入林","scriptText":"【竹林 外 日】\n张三走入竹林。"}]}`
	writer := &fakeWriter{errors: []error{ErrModelTimeout}, replies: []string{art, director}}
	service := NewService(repo, writer)
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.analyzeLegacyBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 3 || writer.prompts[0] != writer.prompts[1] || repo.batches[0].Status != "ready" {
		t.Fatalf("美术指导超时后未重试当前段: %d 次，状态 %s", len(writer.prompts), repo.batches[0].Status)
	}
}

func TestAnalyzeBatchRetriesOnlyCurrentDirectorSegment(t *testing.T) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	source := strings.Repeat("甲", BatchAnalysisChunkLimit+100)
	repo := &fakeRepository{project: Project{ID: id, Title: "长剧", Status: "ready", SourceText: source, Overview: json.RawMessage(`{}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, StartOffset: 0, EndOffset: len([]rune(source)), Revision: 1}}}
	art := `{"characters":[],"scenes":[],"props":[]}`
	director := `{"episodes":[{"title":"片段","scriptText":"【场景 外 日】\n人物行动。"}]}`
	writer := &fakeWriter{errors: []error{nil, nil, nil, ErrModelTimeout}, replies: []string{art, art, director, director}}
	service := NewService(repo, writer)
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.analyzeLegacyBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 5 || writer.prompts[0] == writer.prompts[2] || writer.prompts[3] != writer.prompts[4] || len(repo.batchDraft.Episodes) != 2 {
		t.Fatalf("应只重试超时的导演片段，调用 %d 次、剧集 %d 集", len(writer.prompts), len(repo.batchDraft.Episodes))
	}
}

func TestAnalyzeBatchKeepsCompletedStepsAfterFailure(t *testing.T) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	source := strings.Repeat("甲", BatchAnalysisChunkLimit+100)
	repo := &fakeRepository{project: Project{ID: id, Title: "长剧", Status: "ready", SourceText: source, Overview: json.RawMessage(`{}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, StartOffset: 0, EndOffset: len([]rune(source)), Revision: 1}}}
	art := `{"characters":[{"name":"甲","description":"主角"}],"scenes":[],"props":[]}`
	director := `{"episodes":[{"title":"片段","scriptText":"【室内 日】\n甲在行动。"}]}`
	writer := &fakeWriter{replies: []string{art, art, director}, errors: []error{nil, nil, nil, errors.New("供应商暂时失败")}}
	service := NewService(repo, writer)
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.analyzeLegacyBatch(job); err == nil {
		t.Fatal("导演第二段失败应终止当前任务")
	}
	progress, err := service.BatchProgress(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(progress.Assets.Characters) != 1 || len(progress.Episodes) != 1 || len(progress.Steps) != 4 {
		t.Fatalf("成功片段应保留并可预览: %+v", progress)
	}
	job, err = service.ResumeBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	writer.replies = []string{director}
	if err = service.analyzeLegacyBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 5 || len(repo.batchDraft.Episodes) != 2 {
		t.Fatalf("重试应仅运行失败的导演片段，调用 %d 次，剧集 %d 集", len(writer.prompts), len(repo.batchDraft.Episodes))
	}
}

func TestAnalyzeBatchRetriesEmptyModelContent(t *testing.T) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	repo := &fakeRepository{project: Project{ID: id, Title: "短剧", Status: "ready", SourceText: "甲", Overview: json.RawMessage(`{}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, EndOffset: 1, Revision: 1}}}
	art := `{"characters":[],"scenes":[],"props":[]}`
	director := `{"episodes":[{"title":"片段","scriptText":"【室内 日】甲在行动。"}]}`
	writer := &fakeWriter{errors: []error{ErrEmptyModelContent}, replies: []string{art, director}}
	service := NewService(repo, writer)
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.analyzeLegacyBatch(job); err != nil {
		t.Fatal(err)
	}
	if len(writer.prompts) != 3 || writer.prompts[0] != writer.prompts[1] {
		t.Fatal("空正文应只重试当前步骤")
	}
}

func TestAnalyzeBatchRejectsMissingArtLists(t *testing.T) {
	id := "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	repo := &fakeRepository{project: Project{ID: id, Title: "短剧", Status: "ready", SourceText: "原文", Overview: json.RawMessage(`{}`)}, settings: Settings{TextModelCode: "writer"}, batches: []Batch{{ID: id, ProjectID: id, StartOffset: 0, EndOffset: 2, Revision: 1}}}
	writer := &fakeWriter{replies: []string{`{}`}}
	service := NewService(repo, writer)
	job, err := service.StartBatchAnalysis(context.Background(), "org", "user", id, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.analyzeLegacyBatch(job); err == nil || !strings.Contains(err.Error(), "美术指导第 1 段") {
		t.Fatalf("应指出美术指导结果格式错误: %v", err)
	}
	if repo.batches[0].Status != "failed" || len(writer.prompts) != 1 {
		t.Fatal("无效资产结果不应进入导演阶段")
	}
}

func (r *fakeRepository) AppendBatchEvent(_ context.Context, _ BatchAnalysisJob, e BatchEvent) error {
	e.ID = int64(len(r.events) + 1)
	r.events = append(r.events, e)
	return nil
}
func (r *fakeRepository) ListBatchEvents(_ context.Context, _, _, _, _ string, after int64) ([]BatchEvent, error) {
	out := []BatchEvent{}
	for _, e := range r.events {
		if e.ID > after {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *fakeRepository) FailBatchStep(_ context.Context, _ BatchAnalysisJob, stage string, number int, reason string) error {
	for i := range r.steps {
		if r.steps[i].Stage == stage && r.steps[i].ChunkNumber == number {
			r.steps[i].Status = "failed"
			r.steps[i].ErrorMessage = reason
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeRepository) RenameBatch(_ context.Context, _, _, _, id, name string) (Batch, error) {
	for i := range r.batches {
		if r.batches[i].ID == id {
			r.batches[i].Name = name
			r.batches[i].NameManuallyEdited = true
			return r.batches[i], nil
		}
	}
	return Batch{}, ErrNotFound
}
func (r *fakeRepository) SaveGeneratedBatchName(_ context.Context, job BatchAnalysisJob, name string) error {
	for i := range r.batches {
		if r.batches[i].ID == job.Batch.ID {
			if !r.batches[i].NameManuallyEdited {
				r.batches[i].Name = name
			}
			return nil
		}
	}
	return ErrNotFound
}
