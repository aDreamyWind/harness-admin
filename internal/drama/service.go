package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrInvalidInput = errors.New("漫剧项目参数不正确")
var ErrNotFound = errors.New("漫剧项目不存在")
var ErrModelNotConfigured = errors.New("请先在漫剧设置中配置可用的文本模型")
var ErrModelUnavailable = errors.New("所选模型未启用或当前账号无权使用")
var ErrModelTimeout = errors.New("文本模型响应超时")
var ErrModelTemporary = errors.New("模型供应商暂时不可用")
var ErrEmptyModelContent = errors.New("模型没有返回可解析的正文")
var ErrRetryNotAllowed = errors.New("当前项目不能重新解析")

const ParseRuneLimit = 100000
const OverviewChunkRuneLimit = 20000

type Project struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"-"`
	OwnerID        string          `json:"-"`
	Title          string          `json:"title"`
	SourceName     string          `json:"sourceName"`
	SourceText     string          `json:"-"`
	ForceOverview  bool            `json:"-"`
	SourceLength   int             `json:"sourceLength"`
	SourceMode     string          `json:"sourceMode"`
	Style          string          `json:"style"`
	AspectRatio    string          `json:"aspectRatio"`
	ModelCode      string          `json:"modelCode"`
	Status         string          `json:"status"`
	Phase          string          `json:"phase"`
	Error          string          `json:"error,omitempty"`
	Overview       json.RawMessage `json:"overview"`
	Characters     json.RawMessage `json:"characters"`
	Scenes         json.RawMessage `json:"scenes"`
	Props          json.RawMessage `json:"props"`
	Episodes       json.RawMessage `json:"episodes"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type CreateInput struct {
	OrganizationID string `json:"-"`
	OwnerID        string `json:"-"`
	Title          string `json:"title"`
	SourceName     string `json:"sourceName"`
	SourceText     string `json:"sourceText"`
	SourceMode     string `json:"sourceMode"`
	Style          string `json:"style"`
	AspectRatio    string `json:"aspectRatio"`
}

type Settings struct {
	OrganizationID string    `json:"-"`
	UserID         string    `json:"-"`
	TextModelCode  string    `json:"textModelCode"`
	ImageModelCode string    `json:"imageModelCode"`
	VideoModelCode string    `json:"videoModelCode"`
	UpdatedAt      time.Time `json:"updatedAt,omitempty"`
}

type Repository interface {
	Create(context.Context, Project) (Project, error)
	List(context.Context, string, string) ([]Project, error)
	Get(context.Context, string, string, string) (Project, error)
	GetSource(context.Context, string, string, string) (string, error)
	GetSourceRange(context.Context, string, string, string, int, int) (SourceRange, error)
	ListBatches(context.Context, string, string, string) ([]Batch, error)
	GetBatch(context.Context, string, string, string, string) (Batch, error)
	CreateBatch(context.Context, string, string, Batch) (Batch, error)
	UpdateBatchRange(context.Context, string, string, string, string, BatchRangeChange) (Batch, error)
	QueueBatchAnalysis(context.Context, string, string, string, string, BatchRunOptions) (Batch, error)
	SaveBatchResult(context.Context, string, string, string, string, int, BatchDraft) error
	ListBatchSteps(context.Context, string, string, string, string) ([]BatchStep, error)
	StartBatchStep(context.Context, string, string, string, string, int, BatchStep) error
	CompleteBatchStep(context.Context, string, string, string, string, int, BatchStep) error
	FailBatchStep(context.Context, BatchAnalysisJob, string, int, string) error
	AppendBatchEvent(context.Context, BatchAnalysisJob, BatchEvent) error
	ListBatchEvents(context.Context, string, string, string, string, int64) ([]BatchEvent, error)
	FailBatchAnalysis(context.Context, string, string, string, string, int, string) error
	GetBatchResult(context.Context, string, string, string, string) (BatchResult, error)
	ListProjectAssets(context.Context, string, string, string, string, string) ([]Asset, error)
	AddEpisodeAsset(context.Context, string, string, string, string, string, string) (EpisodeAsset, error)
	RemoveEpisodeAsset(context.Context, string, string, string, string, string) error
	FailInterruptedBatches(context.Context) error
	SaveAnalysis(context.Context, string, string, string, string, string, json.RawMessage) error
	FailAnalysis(context.Context, string, string, string, string) error
	UpdateOverview(context.Context, string, string, string, json.RawMessage) (Project, error)
	FailInterrupted(context.Context) error
	SaveGeneratedScript(context.Context, string, string, string, string) error
	RetryFailed(context.Context, string, string, string, string) (Project, error)
	QueueOverviewReparse(context.Context, string, string, string, string) (Project, error)
	GetSettings(context.Context, string, string) (Settings, error)
	SaveSettings(context.Context, Settings) (Settings, error)
}

type Writer interface {
	Complete(context.Context, string, string, string, string) (string, error)
	AvailableModels(context.Context, string, string) ([]Model, error)
}

type Model struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	ModelType string `json:"modelType"`
}

type Service struct {
	repository Repository
	writer     Writer
}

func NewService(repository Repository, writer Writer) *Service {
	return &Service{repository: repository, writer: writer}
}

func (s *Service) Recover(ctx context.Context) error {
	if err := s.repository.FailInterrupted(ctx); err != nil {
		return err
	}
	return s.repository.FailInterruptedBatches(ctx)
}

func (s *Service) AvailableModels(ctx context.Context, organizationID, userID string) ([]Model, error) {
	models, err := s.writer.AvailableModels(ctx, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("查询可用剧本模型失败: %w", err)
	}
	return models, nil
}

func (s *Service) GetSettings(ctx context.Context, organizationID, userID string) (Settings, error) {
	if organizationID == "" || userID == "" {
		return Settings{}, ErrInvalidInput
	}
	return s.repository.GetSettings(ctx, organizationID, userID)
}

func (s *Service) SaveSettings(ctx context.Context, input Settings) (Settings, error) {
	if input.OrganizationID == "" || input.UserID == "" {
		return Settings{}, ErrInvalidInput
	}
	input.TextModelCode = strings.TrimSpace(input.TextModelCode)
	input.ImageModelCode = strings.TrimSpace(input.ImageModelCode)
	input.VideoModelCode = strings.TrimSpace(input.VideoModelCode)
	models, err := s.AvailableModels(ctx, input.OrganizationID, input.UserID)
	if err != nil {
		return Settings{}, err
	}
	for _, selected := range []struct{ code, kind string }{
		{input.TextModelCode, "chat"}, {input.ImageModelCode, "image"}, {input.VideoModelCode, "video"},
	} {
		if selected.code == "" {
			continue
		}
		found := false
		for _, item := range models {
			if item.Code == selected.code && item.ModelType == selected.kind {
				found = true
				break
			}
		}
		if !found {
			return Settings{}, ErrModelUnavailable
		}
	}
	result, err := s.repository.SaveSettings(ctx, input)
	if err != nil {
		return Settings{}, fmt.Errorf("保存漫剧模型设置失败: %w", err)
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Project, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.Style = strings.TrimSpace(input.Style)
	if input.SourceMode == "" {
		input.SourceMode = "upload"
	}
	if input.OrganizationID == "" || input.OwnerID == "" || input.Title == "" || strings.TrimSpace(input.SourceText) == "" || input.Style == "" ||
		utf8.RuneCountInString(input.Title) > 120 || utf8.RuneCountInString(input.SourceName) > 255 || utf8.RuneCountInString(input.Style) > 80 ||
		(input.AspectRatio != "9:16" && input.AspectRatio != "16:9") || (input.SourceMode != "upload" && input.SourceMode != "generate") || !utf8.ValidString(input.SourceText) {
		return Project{}, ErrInvalidInput
	}
	settings, err := s.GetSettings(ctx, input.OrganizationID, input.OwnerID)
	if err != nil {
		return Project{}, fmt.Errorf("读取漫剧模型设置失败: %w", err)
	}
	if settings.TextModelCode == "" {
		return Project{}, ErrModelNotConfigured
	}
	models, err := s.AvailableModels(ctx, input.OrganizationID, input.OwnerID)
	if err != nil {
		return Project{}, err
	}
	allowed := false
	for _, item := range models {
		if item.Code == settings.TextModelCode && item.ModelType == "chat" {
			allowed = true
			break
		}
	}
	if !allowed {
		return Project{}, ErrModelNotConfigured
	}
	item := Project{
		ID: uuid.NewString(), OrganizationID: input.OrganizationID, OwnerID: input.OwnerID,
		Title: input.Title, SourceName: input.SourceName, SourceText: input.SourceText,
		SourceLength: utf8.RuneCountInString(input.SourceText), Style: input.Style,
		SourceMode:  input.SourceMode,
		AspectRatio: input.AspectRatio, ModelCode: settings.TextModelCode, Status: "queued", Phase: "waiting",
		Overview: json.RawMessage(`{}`), Characters: json.RawMessage(`[]`), Scenes: json.RawMessage(`[]`),
		Props: json.RawMessage(`[]`), Episodes: json.RawMessage(`[]`),
	}
	created, err := s.repository.Create(ctx, item)
	if err != nil {
		return Project{}, fmt.Errorf("保存漫剧项目失败: %w", err)
	}
	return created, nil
}

func (s *Service) List(ctx context.Context, organizationID, ownerID string) ([]Project, error) {
	return s.repository.List(ctx, organizationID, ownerID)
}

func (s *Service) Get(ctx context.Context, organizationID, ownerID, id string) (Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Project{}, ErrInvalidInput
	}
	return s.repository.Get(ctx, organizationID, ownerID, id)
}

// Retry 只重试上传的失败项目；原文从数据库读取，沿用当前用户配置的文本模型。
func (s *Service) Retry(ctx context.Context, organizationID, ownerID, id string) (Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Project{}, ErrInvalidInput
	}
	project, err := s.repository.Get(ctx, organizationID, ownerID, id)
	if err != nil {
		return Project{}, err
	}
	if project.Status != "failed" || project.SourceMode != "upload" {
		return Project{}, ErrRetryNotAllowed
	}
	settings, err := s.GetSettings(ctx, organizationID, ownerID)
	if err != nil {
		return Project{}, err
	}
	if settings.TextModelCode == "" {
		return Project{}, ErrModelNotConfigured
	}
	models, err := s.AvailableModels(ctx, organizationID, ownerID)
	if err != nil {
		return Project{}, err
	}
	available := false
	for _, item := range models {
		if item.Code == settings.TextModelCode && item.ModelType == "chat" {
			available = true
			break
		}
	}
	if !available {
		return Project{}, ErrModelNotConfigured
	}
	project, err = s.repository.RetryFailed(ctx, organizationID, ownerID, id, settings.TextModelCode)
	if errors.Is(err, ErrNotFound) {
		return Project{}, ErrRetryNotAllowed
	}
	if err != nil {
		return Project{}, fmt.Errorf("重新解析漫剧项目失败: %w", err)
	}
	return project, nil
}

// ReanalyzeOverview 使用当前用户的文本模型重新解析已完成的概览，保留旧结果直到新结果写入。
func (s *Service) ReanalyzeOverview(ctx context.Context, organizationID, ownerID, id string) (Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Project{}, ErrInvalidInput
	}
	project, err := s.repository.Get(ctx, organizationID, ownerID, id)
	if err != nil {
		return Project{}, err
	}
	if (project.Status != "ready" && project.Status != "failed") || project.SourceLength == 0 || validateStage("story", project.Overview) != nil {
		return Project{}, ErrRetryNotAllowed
	}
	settings, err := s.GetSettings(ctx, organizationID, ownerID)
	if err != nil {
		return Project{}, err
	}
	if settings.TextModelCode == "" {
		return Project{}, ErrModelNotConfigured
	}
	models, err := s.AvailableModels(ctx, organizationID, ownerID)
	if err != nil {
		return Project{}, err
	}
	available := false
	for _, item := range models {
		if item.Code == settings.TextModelCode && item.ModelType == "chat" {
			available = true
			break
		}
	}
	if !available {
		return Project{}, ErrModelNotConfigured
	}
	project, err = s.repository.QueueOverviewReparse(ctx, organizationID, ownerID, id, settings.TextModelCode)
	if errors.Is(err, ErrNotFound) {
		return Project{}, ErrRetryNotAllowed
	}
	if err != nil {
		return Project{}, fmt.Errorf("重新解析漫剧概览失败: %w", err)
	}
	project.ForceOverview = true
	return project, nil
}

func (s *Service) GetSource(ctx context.Context, organizationID, ownerID, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", ErrInvalidInput
	}
	return s.repository.GetSource(ctx, organizationID, ownerID, id)
}

func (s *Service) UpdateOverview(ctx context.Context, organizationID, ownerID, id string, overview json.RawMessage) (Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Project{}, ErrInvalidInput
	}
	var value map[string]any
	if len(overview) > 16*1024 || json.Unmarshal(overview, &value) != nil || value == nil {
		return Project{}, ErrInvalidInput
	}
	for _, key := range []string{"videoStyle", "aspectRatio", "protagonist", "genre", "synopsis", "background", "setting"} {
		if item, ok := value[key]; ok {
			if _, ok := item.(string); !ok {
				return Project{}, ErrInvalidInput
			}
		}
	}
	if style, ok := value["videoStyle"].(string); ok && utf8.RuneCountInString(style) > 80 {
		return Project{}, ErrInvalidInput
	}
	if ratio, ok := value["aspectRatio"].(string); ok && ratio != "" && ratio != "9:16" && ratio != "16:9" {
		return Project{}, ErrInvalidInput
	}
	return s.repository.UpdateOverview(ctx, organizationID, ownerID, id, overview)
}

const overviewAnalysisPrompt = `你是漫剧项目的原文分析员。只分析给出的原文范围，不续写、不补造后续剧情；把角色推测、传闻与已证实的事实区分开。
请返回严格 JSON 对象，且只包含 videoStyle、aspectRatio、protagonist、genre、synopsis、background、setting 七个键，值全部为中文字符串。
字段要求：
- videoStyle：沿用输入的视频风格；aspectRatio：沿用输入的画面比例。
- protagonist：写原文中的主角姓名或稳定称谓；不要把重要配角误当主角。
- genre：用一个主类型加必要的子类型概括，不用空泛的“剧情”。
- synopsis：用连贯叙述写清已出现的起因、主角目标、阻碍、关键人物关系、已发生的转折和当前悬念。重视因果和具体事件，不写模板化赞美或未出现的结局。
- background：说明原文已交代的时代、地点、社会环境、势力或历史背景；不把人物行动重复成背景。
- setting：按一条一行列出影响剧情的世界规则、能力限制、重要物件和组织关系；只写原文有依据的规则，不把猜测写成设定。
某项缺乏依据时写“原文前段未明确”，不要猜测。梗概、背景、设定应有足够细节，让后续编剧能据此继续工作。
`

// Analyze 在后台完成剧本概览。完整原文入库，模型只读取前 ParseRuneLimit 个字符。
func (s *Service) Analyze(project Project) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if project.SourceMode == "generate" && !project.ForceOverview {
		if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "running", "script", nil); err != nil {
			return s.fail(project, err)
		}
		idea := []rune(project.SourceText)
		if len(idea) > ParseRuneLimit {
			idea = idea[:ParseRuneLimit]
		}
		prompt := fmt.Sprintf("你是漫剧编剧。请根据创意生成完整的第一集中文漫剧剧本，包含开场、角色对白、场景转换和结尾悬念。返回严格 JSON 对象，唯一键为 script，值为剧本文本。创意：%s", string(idea))
		result, err := s.writer.Complete(ctx, project.OrganizationID, project.OwnerID, project.ModelCode, prompt)
		if err != nil {
			return s.fail(project, err)
		}
		data, err := parseObject(result)
		if err != nil {
			return s.fail(project, err)
		}
		var script struct {
			Script string `json:"script"`
		}
		if json.Unmarshal(data, &script) != nil || strings.TrimSpace(script.Script) == "" {
			return s.fail(project, errors.New("模型未返回剧本内容"))
		}
		if err := s.repository.SaveGeneratedScript(ctx, project.OrganizationID, project.OwnerID, project.ID, script.Script); err != nil {
			return s.fail(project, err)
		}
		project.SourceText = script.Script
	}
	if project.ForceOverview || validateStage("story", project.Overview) != nil {
		source := []rune(project.SourceText)
		if len(source) > ParseRuneLimit {
			source = source[:ParseRuneLimit]
		}
		if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "running", "story", nil); err != nil {
			return s.fail(project, err)
		}
		contextText, contextLabel, err := s.overviewSourceContext(ctx, project, source)
		if err != nil {
			return s.fail(project, err)
		}
		base := fmt.Sprintf("作品名：%s\n视频风格：%s\n画面比例：%s\n请仅依据以下%s工作，不要臆造后续剧情。\n%s：\n%s", project.Title, project.Style, project.AspectRatio, contextLabel, contextLabel, contextText)
		result, err := s.writer.Complete(ctx, project.OrganizationID, project.OwnerID, project.ModelCode, overviewAnalysisPrompt+base)
		if err != nil {
			return s.fail(project, err)
		}
		data, err := parseObject(result)
		if err != nil {
			return s.fail(project, err)
		}
		if err := validateStage("story", data); err != nil {
			return s.fail(project, err)
		}
		if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "running", "story", data); err != nil {
			return s.fail(project, err)
		}
	}
	if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "ready", "overview_done", nil); err != nil {
		return s.fail(project, err)
	}
	return nil
}

// overviewSourceContext 对长原文分段提取事实，再交给最终概览汇总。
func (s *Service) overviewSourceContext(ctx context.Context, project Project, source []rune) (string, string, error) {
	if len(source) <= OverviewChunkRuneLimit {
		return string(source), "原文", nil
	}
	count := (len(source) + OverviewChunkRuneLimit - 1) / OverviewChunkRuneLimit
	extracts := make([]string, 0, count)
	for start := 0; start < len(source); start += OverviewChunkRuneLimit {
		end := min(start+OverviewChunkRuneLimit, len(source))
		prompt := fmt.Sprintf("你是小说与剧本的事实提取员。只处理当前片段，不预测后文，不写总体概览。返回严格 JSON 对象，键为 events、characters、backgroundFacts、settingFacts、unresolved，值均为中文字符串。events 按“谁做了什么→结果与变化”记录关键事件及因果；characters 记录人物姓名/别名、目标与关系；backgroundFacts 记录时代地点与势力；settingFacts 记录已证实的规则和限制；unresolved 记录尚未证实的传闻、伏笔和疑问。每项尽量简洁，整份不超过 1200 个汉字。缺乏依据的字段填空字符串。原文片段 %d/%d（字符 %d—%d）：\n%s", len(extracts)+1, count, start+1, end, string(source[start:end]))
		result, err := s.writer.Complete(ctx, project.OrganizationID, project.OwnerID, project.ModelCode, prompt)
		if err != nil {
			return "", "", fmt.Errorf("概览第 %d 段事实提取失败: %w", len(extracts)+1, err)
		}
		data, err := parseObject(result)
		if err != nil {
			return "", "", fmt.Errorf("概览第 %d 段事实格式无效: %w", len(extracts)+1, err)
		}
		var facts map[string]json.RawMessage
		if err := json.Unmarshal(data, &facts); err != nil {
			return "", "", fmt.Errorf("概览第 %d 段事实格式无效: %w", len(extracts)+1, err)
		}
		hasFact := false
		for _, value := range facts {
			fact := strings.TrimSpace(string(value))
			if fact != "" && fact != `""` && fact != "null" && fact != "[]" && fact != "{}" {
				hasFact = true
				break
			}
		}
		if !hasFact {
			// 单段模型没有提取到事实时，保留该段首尾原文供最终概览参考，避免整项任务被空结果中断。
			chunk := source[start:end]
			excerpt := string(chunk[:min(len(chunk), 2000)])
			if len(chunk) > 4000 {
				excerpt += "\n……\n" + string(chunk[len(chunk)-2000:])
			}
			extracts = append(extracts, fmt.Sprintf("原文字符 %d—%d（事实提取为空，以下为原文节选）：%s", start+1, end, excerpt))
			continue
		}
		extracts = append(extracts, fmt.Sprintf("原文字符 %d—%d：%s", start+1, end, string(data)))
	}
	return strings.Join(extracts, "\n"), "逐段事实摘录", nil
}

func (s *Service) fail(project Project, err error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	characters := []rune(err.Error())
	if len(characters) > 500 {
		characters = characters[:500]
	}
	message := string(characters)
	if saveErr := s.repository.FailAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, message); saveErr != nil {
		return fmt.Errorf("%w；记录解析失败状态失败: %v", err, saveErr)
	}
	return err
}

func validateStage(phase string, data json.RawMessage) error {
	if phase == "story" {
		var value map[string]json.RawMessage
		if json.Unmarshal(data, &value) != nil {
			return errors.New("概览数据格式无效")
		}
		for _, key := range []string{"videoStyle", "aspectRatio", "protagonist", "genre", "synopsis", "background", "setting"} {
			var field string
			if len(value[key]) == 0 || json.Unmarshal(value[key], &field) != nil {
				return fmt.Errorf("概览字段 %s 格式无效", key)
			}
		}
		return nil
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(data, &value) != nil {
		return errors.New("资产或分集数据格式无效")
	}
	keys := []string{"characters", "scenes", "props"}
	if phase == "episodes" {
		keys = []string{"episodes"}
	}
	for _, key := range keys {
		var items []map[string]json.RawMessage
		if len(value[key]) == 0 || json.Unmarshal(value[key], &items) != nil {
			return fmt.Errorf("%s 列表格式无效", key)
		}
		if phase == "episodes" && len(items) > 8 {
			return errors.New("分集数量超过 8 集")
		}
		for _, item := range items {
			var name, detail string
			nameKey, detailKey := "name", "description"
			if phase == "episodes" {
				nameKey, detailKey = "title", "synopsis"
			}
			if json.Unmarshal(item[nameKey], &name) != nil || json.Unmarshal(item[detailKey], &detail) != nil {
				return fmt.Errorf("%s 条目格式无效", key)
			}
		}
	}
	return nil
}

func parseObject(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	start, end := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return nil, errors.New("模型未返回可解析的 JSON")
	}
	data := []byte(text[start : end+1])
	var value map[string]json.RawMessage
	if json.Unmarshal(data, &value) != nil || value == nil {
		return nil, errors.New("模型返回的 JSON 格式无效")
	}
	return data, nil
}
