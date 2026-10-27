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
var ErrRetryNotAllowed = errors.New("当前项目不能重新解析")

const ParseRuneLimit = 30000

type Project struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"-"`
	OwnerID        string          `json:"-"`
	Title          string          `json:"title"`
	SourceName     string          `json:"sourceName"`
	SourceText     string          `json:"-"`
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
	SaveAnalysis(context.Context, string, string, string, string, string, json.RawMessage) error
	FailAnalysis(context.Context, string, string, string, string) error
	UpdateOverview(context.Context, string, string, string, json.RawMessage) (Project, error)
	FailInterrupted(context.Context) error
	SaveGeneratedScript(context.Context, string, string, string, string) error
	RetryFailed(context.Context, string, string, string, string) (Project, error)
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

func (s *Service) Recover(ctx context.Context) error { return s.repository.FailInterrupted(ctx) }

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
	return s.repository.UpdateOverview(ctx, organizationID, ownerID, id, overview)
}

// Analyze 在后台顺序运行三个独立的创作阶段；每一阶段完成后立即保存，供工作台刷新查看。
func (s *Service) Analyze(project Project) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if project.SourceMode == "generate" {
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
	source := []rune(project.SourceText)
	if len(source) > ParseRuneLimit {
		source = source[:ParseRuneLimit]
	}
	base := fmt.Sprintf("作品名：%s\n视频风格：%s\n画面比例：%s\n请仅依据以下剧本/小说内容工作，不要臆造后续剧情。\n原文：\n%s", project.Title, project.Style, project.AspectRatio, string(source))
	steps := []struct{ phase, prompt string }{
		{"story", "你是漫剧编剧。提取项目概览，返回严格 JSON 对象，键为 videoStyle、aspectRatio、protagonist、genre、synopsis、background、setting，值全部为中文字符串。\n" + base},
		{"assets", "你是漫剧美术。提取原文中明确出现的资产，返回严格 JSON 对象，键为 characters、scenes、props；每个值是对象数组，对象键为 name、description。未知信息留空，不要添加解释。\n" + base},
		{"episodes", "你是漫剧导演。根据提供的原文段落规划分集，返回严格 JSON 对象，键 episodes，值为对象数组；对象键 title、synopsis。最多 8 集，未覆盖的后续剧情不得编造。\n" + base},
	}
	for _, step := range steps {
		if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "running", step.phase, nil); err != nil {
			return s.fail(project, err)
		}
		result, err := s.writer.Complete(ctx, project.OrganizationID, project.OwnerID, project.ModelCode, step.prompt)
		if err != nil {
			return s.fail(project, err)
		}
		data, err := parseObject(result)
		if err != nil {
			return s.fail(project, err)
		}
		if err := validateStage(step.phase, data); err != nil {
			return s.fail(project, err)
		}
		if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "running", step.phase, data); err != nil {
			return s.fail(project, err)
		}
	}
	if err := s.repository.SaveAnalysis(ctx, project.OrganizationID, project.OwnerID, project.ID, "ready", "complete", nil); err != nil {
		return s.fail(project, err)
	}
	return nil
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
