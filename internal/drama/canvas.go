package drama

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrCanvasConflict = errors.New("内容已被其他页面修改，请重新加载画布")
var ErrImageBusy = errors.New("当前形象正在生成图片，请等待任务完成")
var ErrImageModelMissing = errors.New("请先在设置中选择可用的图片模型")
var ErrParentImageMissing = errors.New("请先为原始形象生成图片，再生成派生形象")

type AssetForm struct {
	ID             string  `json:"id"`
	AssetID        string  `json:"assetId"`
	AssetName      string  `json:"assetName"`
	Kind           string  `json:"kind"`
	ParentID       string  `json:"parentId,omitempty"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Gender         string  `json:"gender"`
	StyleID        string  `json:"styleId"`
	Style          string  `json:"style"`
	AspectRatio    string  `json:"aspectRatio"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Revision       int     `json:"revision"`
	ImageURL       string  `json:"imageUrl,omitempty"`
	ImageStatus    string  `json:"imageStatus"`
	ImageError     string  `json:"imageError,omitempty"`
	CurrentImageID string  `json:"-"`
}
type Canvas struct {
	Forms    []AssetForm   `json:"forms"`
	X        float64       `json:"x"`
	Y        float64       `json:"y"`
	Zoom     float64       `json:"zoom"`
	Revision int           `json:"revision"`
	Extras   *CanvasExtras `json:"extras,omitempty"`
}
type CanvasPosition struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}
type CanvasLayout struct {
	Revision  int              `json:"revision"`
	X         float64          `json:"x"`
	Y         float64          `json:"y"`
	Zoom      float64          `json:"zoom"`
	Positions []CanvasPosition `json:"positions"`
	Extras    *CanvasExtras    `json:"extras,omitempty"`
}
type FormEdit struct {
	Revision       int          `json:"revision"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Gender         string       `json:"gender"`
	StyleID        string       `json:"styleId"`
	Style          string       `json:"style"`
	AspectRatio    string       `json:"aspectRatio"`
	CanvasRevision *int         `json:"canvasRevision,omitempty"`
	MentionLinks   []CanvasLink `json:"mentionLinks,omitempty"`
}

// MergeCanvasFormMentions 保留已有布局，将新引用合并为指向当前形象的入线。
func MergeCanvasFormMentions(project, form string, extras CanvasExtras, links []CanvasLink) (CanvasExtras, error) {
	if len(links) > 100 {
		return CanvasExtras{}, fmt.Errorf("%w：一次最多添加 100 条引用连线", ErrInvalidInput)
	}
	hidden := make(map[string]bool, len(extras.Hidden))
	for _, id := range extras.Hidden {
		hidden[id] = true
	}
	seen := make(map[string]bool)
	for _, link := range extras.Links {
		if link.Target == form {
			seen[link.Source] = true
		}
	}
	extras.Links = slices.Clone(extras.Links)
	for _, link := range links {
		if link.Target != form || hidden[link.Source] || hidden[link.Target] {
			return CanvasExtras{}, fmt.Errorf("%w：引用连线须连接到当前形象，且两端都可见", ErrInvalidInput)
		}
		if !seen[link.Source] {
			extras.Links = append(extras.Links, link)
			seen[link.Source] = true
		}
	}
	return normalizeCanvasExtras(project, extras)
}

type ImageInput struct {
	Prompt         string `json:"prompt"`
	CanvasRevision *int   `json:"canvasRevision,omitempty"`
	ReferenceGuide string `json:"-"`
}
type ImageJob struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	FormID         string `json:"formId"`
	StyleID        string `json:"styleId"`
	ModelCode      string `json:"modelCode"`
	Prompt         string `json:"-"`
	AspectRatio    string `json:"aspectRatio"`
	ReferenceID    string `json:"referenceId,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	StorageKey     string `json:"-"`
	MIME           string `json:"-"`
	ByteSize       int    `json:"-"`
	OrganizationID string `json:"-"`
	UserID         string `json:"-"`
	canvasSources  []canvasGenerationSource
}

// 图片供应商可能在错误中回显请求，普通任务状态仅暴露经过分类的短消息。
type ImageFailure struct {
	Message string
	Cause   error
}

func (e *ImageFailure) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return e.Message + "：" + e.Cause.Error()
}
func (e *ImageFailure) Unwrap() error { return e.Cause }

type ImageRequest struct{ OrganizationID, UserID, ModelCode, Prompt, AspectRatio, ReferenceDataURI string }
type ImageGenerator interface {
	Generate(context.Context, ImageRequest) ([]byte, string, error)
}
type ImageFiles interface {
	Put(context.Context, string, []byte, string) (string, error)
	Get(context.Context, string) ([]byte, string, error)
}
type VisualRepository interface {
	ReadCanvas(context.Context, string, string, string) (Canvas, error)
	SaveCanvasLayout(context.Context, string, string, string, CanvasLayout) (int, error)
	EditAssetForm(context.Context, string, string, string, string, FormEdit) error
	DeriveAssetForm(context.Context, string, string, string, string, string) (string, error)
	QueueImage(context.Context, string, string, string, string, string, ImageInput) (ImageJob, error)
	ClaimImage(context.Context, string) (bool, error)
	FinishImage(context.Context, ImageJob) error
	ReadImage(context.Context, string, string, string, string) (ImageJob, error)
	FailInterruptedImages(context.Context) error
}

func (s *Service) WithVisuals(repo VisualRepository, generator ImageGenerator, files ImageFiles) *Service {
	s.visual, s.images, s.files = repo, generator, files
	return s
}
func validVisualIDs(ids ...string) bool {
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
	}
	return true
}
func (s *Service) Canvas(ctx context.Context, org, user, project string) (Canvas, error) {
	if !validVisualIDs(project) || s.visual == nil {
		return Canvas{}, ErrInvalidInput
	}
	return s.visual.ReadCanvas(ctx, org, user, project)
}
func finiteCanvas(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 1000000
}
func (s *Service) SaveCanvas(ctx context.Context, org, user, project string, in CanvasLayout) (int, error) {
	if !validVisualIDs(project) || s.visual == nil || in.Revision < 1 || !finiteCanvas(in.X) || !finiteCanvas(in.Y) || in.Zoom < 0.05 || in.Zoom > 2 || math.IsNaN(in.Zoom) || len(in.Positions) > 10000 {
		return 0, ErrInvalidInput
	}
	seen := map[string]bool{}
	for _, p := range in.Positions {
		if !validVisualIDs(p.ID) || !finiteCanvas(p.X) || !finiteCanvas(p.Y) || seen[p.ID] {
			return 0, ErrInvalidInput
		}
		seen[p.ID] = true
	}
	if in.Extras != nil {
		extras, err := normalizeCanvasExtras(project, *in.Extras)
		if err != nil {
			return 0, err
		}
		in.Extras = &extras
	}
	return s.visual.SaveCanvasLayout(ctx, org, user, project, in)
}
func (s *Service) EditForm(ctx context.Context, org, user, project, form string, in FormEdit) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Style = strings.TrimSpace(in.Style)
	if s.visual == nil || !validVisualIDs(project, form) || in.Revision < 1 || !validCanvasText(in.Name, 120) ||
		(in.Description != "" && !validCanvasText(in.Description, 10000)) || utf8.RuneCountInString(in.Style) > 120 {
		return ErrInvalidInput
	}
	if in.Gender != "unspecified" && in.Gender != "male" && in.Gender != "female" && in.Gender != "other" {
		return ErrInvalidInput
	}
	if in.AspectRatio != "9:16" && in.AspectRatio != "16:9" && in.AspectRatio != "1:1" {
		return ErrInvalidInput
	}
	if in.CanvasRevision != nil && *in.CanvasRevision < 1 {
		return ErrInvalidInput
	}
	if len(in.MentionLinks) > 0 {
		if in.CanvasRevision == nil {
			return ErrInvalidInput
		}
		extras, err := MergeCanvasFormMentions(project, form, CanvasExtras{}, in.MentionLinks)
		if err != nil {
			return err
		}
		in.MentionLinks = extras.Links
	}
	style, err := s.resolveStyle(ctx, in.StyleID, in.Style)
	if err != nil {
		return err
	}
	in.StyleID, in.Style = style.ID, style.Name
	return s.visual.EditAssetForm(ctx, org, user, project, form, in)
}
func (s *Service) DeriveForm(ctx context.Context, org, user, project, parent string) (string, error) {
	if s.visual == nil || !validVisualIDs(project, parent) {
		return "", ErrInvalidInput
	}
	return s.visual.DeriveAssetForm(ctx, org, user, project, parent, uuid.NewString())
}
func (s *Service) StartImage(ctx context.Context, org, user, project, form string, in ImageInput) (ImageJob, error) {
	if s.visual == nil || s.images == nil || !validVisualIDs(project, form) || strings.ContainsRune(in.Prompt, 0) || utf8.RuneCountInString(in.Prompt) > 10000 ||
		(in.CanvasRevision != nil && *in.CanvasRevision < 1) {
		return ImageJob{}, ErrInvalidInput
	}
	settings, err := s.repository.GetSettings(ctx, org, user)
	if err != nil {
		return ImageJob{}, err
	}
	if settings.ImageModelCode == "" {
		return ImageJob{}, ErrImageModelMissing
	}
	models, err := s.writer.AvailableModels(ctx, org, user)
	if err != nil {
		return ImageJob{}, err
	}
	available := false
	for _, model := range models {
		if model.Code == settings.ImageModelCode && model.ModelType == "image" {
			available = true
		}
	}
	if !available {
		return ImageJob{}, ErrModelUnavailable
	}
	var sources []canvasGenerationSource
	if in.CanvasRevision != nil {
		scope := CanvasGenerationScope{OrganizationID: org, UserID: user, ProjectID: project, NodeID: form}
		sources, err = s.prepareAssetCanvasSources(ctx, scope, *in.CanvasRevision, settings.ImageModelCode)
		if err != nil {
			return ImageJob{}, err
		}
		in.ReferenceGuide = canvasReferenceGuide(sources)
	}
	job, err := s.visual.QueueImage(ctx, org, user, project, form, settings.ImageModelCode, in)
	if err != nil {
		return job, err
	}
	if len(sources) > 0 && job.ReferenceID != "" {
		// 父图以入队事务中读取的图片版本为准。
		sources[0].imageID = job.ReferenceID
	}
	job.canvasSources = sources
	return job, nil
}

func (s *Service) prepareAssetCanvasSources(ctx context.Context, scope CanvasGenerationScope, revision int, model string) ([]canvasGenerationSource, error) {
	canvas, err := s.Canvas(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID)
	if err != nil {
		return nil, err
	}
	if canvas.Revision != revision {
		return nil, ErrCanvasConflict
	}
	formIndex := slices.IndexFunc(canvas.Forms, func(form AssetForm) bool { return form.ID == scope.NodeID })
	if formIndex == -1 {
		return nil, ErrNotFound
	}
	form := canvas.Forms[formIndex]
	_, sources := canvasGenerationSources(canvas, scope.NodeID)
	sources = slices.DeleteFunc(sources, func(source canvasGenerationSource) bool { return source.id == form.ParentID })
	if len(sources) == 0 {
		return nil, nil
	}
	if s.canvasEngine == nil || s.canvasMedia == nil {
		return nil, ErrModelUnavailable
	}
	if err := s.resolveCanvasGenerationSources(ctx, scope, sources); err != nil {
		return nil, err
	}
	if form.ParentID != "" {
		parentIndex := slices.IndexFunc(canvas.Forms, func(parent AssetForm) bool { return parent.ID == form.ParentID })
		title := "原始形象"
		if parentIndex != -1 {
			title = canvasAssetReferenceTitle(canvas.Forms[parentIndex])
		}
		sources = append([]canvasGenerationSource{{id: form.ParentID, title: title}}, sources...)
	}
	models, err := s.ListCanvasGenerationModels(ctx, scope.OrganizationID, scope.UserID)
	if err != nil {
		return nil, err
	}
	if err := validateCanvasGenerationModel(models, CanvasGenerationRequest{Kind: "image", ModelCode: model}, len(sources)); err != nil {
		return nil, err
	}
	return sources, nil
}

func (s *Service) GenerateImage(job ImageJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	claimed, err := s.visual.ClaimImage(ctx, job.ID)
	if err != nil || !claimed {
		return err
	}
	data, mime, err := s.generateAssetImage(ctx, job)
	if err == nil {
		job.MIME = mime
		job.ByteSize = len(data)
		job.StorageKey, err = s.files.Put(ctx, job.ID, data, job.MIME)
	}
	job.Status = "succeeded"
	if err != nil {
		job.Status = "failed"
		job.Error = "图片生成或保存失败，请查看调用记录和服务日志。"
		var public *ImageFailure
		var referenceFailure *CanvasGenerationFailure
		if errors.As(err, &public) {
			job.Error = public.Message
		} else if errors.As(err, &referenceFailure) {
			job.Error = referenceFailure.Message
		}
	}
	saveCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	if saveErr := s.visual.FinishImage(saveCtx, job); saveErr != nil {
		return fmt.Errorf("保存图片任务结果失败: %w（生成结果：%v）", saveErr, err)
	}
	return err
}

func (s *Service) generateAssetImage(ctx context.Context, job ImageJob) ([]byte, string, error) {
	if len(job.canvasSources) > 0 {
		request := CanvasGenerationRequest{
			OrganizationID: job.OrganizationID, UserID: job.UserID, JobID: job.ID,
			Kind: "image", ModelCode: job.ModelCode, Prompt: job.Prompt, AspectRatio: job.AspectRatio,
		}
		scope := CanvasGenerationScope{OrganizationID: job.OrganizationID, UserID: job.UserID, ProjectID: job.ProjectID, NodeID: job.FormID}
		if err := s.loadCanvasGenerationSources(ctx, scope, &request, job.canvasSources); err != nil {
			return nil, "", err
		}
		return s.canvasEngine.GenerateCanvasMedia(ctx, request)
	}
	request := ImageRequest{OrganizationID: job.OrganizationID, UserID: job.UserID, ModelCode: job.ModelCode, Prompt: job.Prompt, AspectRatio: job.AspectRatio}
	if job.ReferenceID != "" {
		data, mime, err := s.ImageContent(ctx, job.OrganizationID, job.UserID, job.ProjectID, job.ReferenceID)
		if err != nil {
			return nil, "", err
		}
		request.ReferenceDataURI = imageDataURI(mime, data)
	}
	return s.images.Generate(ctx, request)
}
func (s *Service) ImageContent(ctx context.Context, org, user, project, id string) ([]byte, string, error) {
	if s.visual == nil || !validVisualIDs(project, id) {
		return nil, "", ErrInvalidInput
	}
	job, err := s.visual.ReadImage(ctx, org, user, project, id)
	if err != nil {
		return nil, "", err
	}
	return s.files.Get(ctx, job.StorageKey)
}

func imageDataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
