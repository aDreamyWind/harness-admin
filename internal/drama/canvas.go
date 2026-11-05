package drama

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"math"
	"strings"
	"time"
	"unicode/utf8"
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
	Revision    int    `json:"revision"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Gender      string `json:"gender"`
	StyleID     string `json:"styleId"`
	Style       string `json:"style"`
	AspectRatio string `json:"aspectRatio"`
}
type ImageInput struct {
	Prompt string `json:"prompt"`
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
	if s.visual == nil || !validVisualIDs(project, form) || in.Revision < 1 || in.Name == "" || utf8.RuneCountInString(in.Name) > 80 || in.Description == "" || utf8.RuneCountInString(in.Description) > 10000 || utf8.RuneCountInString(in.Style) > 120 {
		return ErrInvalidInput
	}
	if in.Gender != "unspecified" && in.Gender != "male" && in.Gender != "female" && in.Gender != "other" {
		return ErrInvalidInput
	}
	if in.AspectRatio != "9:16" && in.AspectRatio != "16:9" && in.AspectRatio != "1:1" {
		return ErrInvalidInput
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
	if s.visual == nil || s.images == nil || !validVisualIDs(project, form) || utf8.RuneCountInString(in.Prompt) > 10000 {
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
	return s.visual.QueueImage(ctx, org, user, project, form, settings.ImageModelCode, in)
}
func (s *Service) GenerateImage(job ImageJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	claimed, err := s.visual.ClaimImage(ctx, job.ID)
	if err != nil || !claimed {
		return err
	}
	req := ImageRequest{OrganizationID: job.OrganizationID, UserID: job.UserID, ModelCode: job.ModelCode, Prompt: job.Prompt, AspectRatio: job.AspectRatio}
	if job.ReferenceID != "" {
		ref, readErr := s.visual.ReadImage(ctx, job.OrganizationID, job.UserID, job.ProjectID, job.ReferenceID)
		err = readErr
		if err == nil {
			var data []byte
			var mime string
			data, mime, err = s.files.Get(ctx, ref.StorageKey)
			if err == nil {
				req.ReferenceDataURI = imageDataURI(mime, data)
			}
		}
	}
	if err == nil {
		var data []byte
		data, job.MIME, err = s.images.Generate(ctx, req)
		if err == nil {
			job.ByteSize = len(data)
			job.StorageKey, err = s.files.Put(ctx, job.ID, data, job.MIME)
		}
	}
	job.Status = "succeeded"
	if err != nil {
		job.Status = "failed"
		job.Error = "图片生成或保存失败，请查看调用记录和服务日志。"
		var public *ImageFailure
		if errors.As(err, &public) {
			job.Error = public.Message
		}
	}
	saveCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	if saveErr := s.visual.FinishImage(saveCtx, job); saveErr != nil {
		return fmt.Errorf("保存图片任务结果失败: %w（生成结果：%v）", saveErr, err)
	}
	return err
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
