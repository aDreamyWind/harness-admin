package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	CoverIdle      = "idle"
	CoverRunning   = "running"
	CoverSucceeded = "succeeded"
	CoverFailed    = "failed"
	MaxCoverBytes  = 10 * 1024 * 1024
)

var ErrCoverConflict = errors.New("封面候选已变化，请刷新后再选择")

type CoverScope struct{ OrganizationID, UserID, ProjectID string }

type CoverGeneration struct {
	Status       string `json:"status"`
	JobID        string `json:"jobId,omitempty"`
	ModelCode    string `json:"modelCode,omitempty"`
	Error        string `json:"error,omitempty"`
	CoverURL     string `json:"coverUrl,omitempty"`
	CandidateURL string `json:"candidateUrl,omitempty"`
	CoverID      string `json:"-"`
	CandidateID  string `json:"-"`
}

type CoverSelection struct {
	JobID  string `json:"jobId"`
	Choice string `json:"choice"`
}

type CoverImage struct {
	Data []byte
	MIME string
}

type CoverRepository interface {
	GetProjectCoverGeneration(context.Context, CoverScope) (CoverGeneration, error)
	ReserveProjectCoverGeneration(context.Context, CoverScope, string, string, string, time.Time) (CoverGeneration, bool, error)
	FinishProjectCoverGeneration(context.Context, CoverScope, string, CoverImage, string) error
	SelectProjectCover(context.Context, CoverScope, CoverSelection) (CoverGeneration, error)
	ReadProjectCoverImage(context.Context, CoverScope, string) (CoverImage, error)
}

type CoverGenerator func(context.Context, string, ImageRequest) ([]byte, error)
type CoverComposer func([]byte, CoverTitleDesign) ([]byte, string, error)

func (s *Service) WithProjectCovers(repo CoverRepository, generate CoverGenerator, compose CoverComposer) *Service {
	s.coverRepository, s.coverGenerator, s.coverComposer = repo, generate, compose
	return s
}

func ProjectCoverImageURL(projectID, revision string) string {
	if revision == "" {
		return ""
	}
	return "/api/v1/ai-drama/projects/" + projectID + "/cover/images/" + revision
}

func (generation CoverGeneration) withURLs(scope CoverScope) CoverGeneration {
	generation.CoverURL = ProjectCoverImageURL(scope.ProjectID, generation.CoverID)
	generation.CandidateURL = ProjectCoverImageURL(scope.ProjectID, generation.CandidateID)
	return generation
}

func (s *Service) GetProjectCoverGeneration(ctx context.Context, scope CoverScope) (CoverGeneration, error) {
	if !canonicalCanvasID(scope.ProjectID) {
		return CoverGeneration{}, ErrInvalidInput
	}
	state, err := s.coverRepository.GetProjectCoverGeneration(ctx, scope)
	return state.withURLs(scope), err
}

func (s *Service) StartProjectCoverGeneration(ctx context.Context, scope CoverScope, modelCode string) (CoverGeneration, error) {
	state, err := s.GetProjectCoverGeneration(ctx, scope)
	if err != nil || state.Status == CoverRunning || state.CandidateID != "" {
		return state, err
	}
	modelCode = strings.TrimSpace(modelCode)
	if utf8.RuneCountInString(modelCode) > 160 || strings.ContainsRune(modelCode, 0) {
		return CoverGeneration{}, ErrInvalidInput
	}
	if modelCode == "" {
		settings, err := s.repository.GetSettings(ctx, scope.OrganizationID, scope.UserID)
		if err != nil {
			return CoverGeneration{}, err
		}
		modelCode = settings.ImageModelCode
		if modelCode == "" {
			return CoverGeneration{}, ErrImageModelMissing
		}
	}
	models, err := s.writer.AvailableModels(ctx, scope.OrganizationID, scope.UserID)
	if err != nil {
		return CoverGeneration{}, err
	}
	available := false
	for _, model := range models {
		if model.Code == modelCode && model.ModelType == "image" {
			available = true
			break
		}
	}
	if !available {
		return CoverGeneration{}, ErrModelUnavailable
	}
	project, err := s.repository.Get(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID)
	if err != nil {
		return CoverGeneration{}, err
	}
	jobID := uuid.New()
	prompt, design, err := s.projectCoverPrompt(ctx, scope, project, jobID[0]&1 != 0)
	if err != nil {
		return CoverGeneration{}, err
	}
	parent := context.WithoutCancel(ctx)
	reserveCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	state, started, err := s.coverRepository.ReserveProjectCoverGeneration(reserveCtx, scope, state.JobID, jobID.String(), modelCode, time.Now().Add(12*time.Minute))
	if err != nil {
		return CoverGeneration{}, err
	}
	if started {
		go s.runProjectCoverGeneration(parent, scope, state, design, prompt)
	}
	return state.withURLs(scope), nil
}

func (s *Service) projectCoverPrompt(ctx context.Context, scope CoverScope, project Project, alternate bool) (string, CoverTitleDesign, error) {
	var overview struct{ Protagonist, Genre, Synopsis, Background string }
	if len(project.Overview) != 0 {
		if err := json.Unmarshal(project.Overview, &overview); err != nil {
			return "", CoverTitleDesign{}, fmt.Errorf("读取封面故事概览失败: %w", err)
		}
	}
	if strings.TrimSpace(overview.Synopsis) == "" && project.SourceLength > 0 {
		source, err := s.repository.GetSourceRange(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID, 0, 720)
		if err != nil {
			return "", CoverTitleDesign{}, err
		}
		overview.Synopsis = source.Text
	}
	styleName := project.Style
	stylePrompt := ""
	styleKey := project.StyleID
	if styleKey == "" {
		styleKey = styleName
	}
	if s.styles != nil && styleKey != "" {
		style, err := s.styles.ResolveStyle(ctx, styleKey)
		if err != nil && !errors.Is(err, ErrStyleUnavailable) {
			return "", CoverTitleDesign{}, fmt.Errorf("读取漫剧封面风格失败: %w", err)
		}
		if err == nil {
			styleName, stylePrompt = style.Name, style.ImagePrompt
			if style.ID != "" {
				styleKey = style.ID
			}
		}
	}
	design := chooseCoverTitleDesign(project.Title, styleKey, styleName, overview.Genre, alternate)
	prompt := fmt.Sprintf("为漫剧绘制 2:3 竖版宣传海报底图。突出主角和有辨识度的故事场景，采用有层次的电影海报构图。%s图像中禁止任何文字、字母、徽标、水印与外框。下列项目资料只作绘画内容参考。\n作品名：%s\n项目画风：%s\n风格特征：%s\n主角：%s\n故事类型：%s\n故事梗概：%s\n故事背景：%s",
		design.reserveSpace(), project.Title, styleName, stylePrompt, coverExcerpt(overview.Protagonist, 100), coverExcerpt(overview.Genre, 100), coverExcerpt(overview.Synopsis, 720), coverExcerpt(overview.Background, 180))
	return prompt, design, nil
}

func coverExcerpt(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	return string(runes[:min(len(runes), limit)])
}

func (s *Service) runProjectCoverGeneration(parent context.Context, scope CoverScope, state CoverGeneration, design CoverTitleDesign, prompt string) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	data, err := s.coverGenerator(ctx, state.JobID, ImageRequest{
		OrganizationID: scope.OrganizationID, UserID: scope.UserID, ModelCode: state.ModelCode,
		Prompt: prompt, AspectRatio: "2:3",
	})
	var image CoverImage
	if err == nil {
		image.Data, image.MIME, err = s.coverComposer(data, design)
		if err == nil && (len(image.Data) == 0 || len(image.Data) > MaxCoverBytes || image.MIME != "image/svg+xml") {
			err = errors.New("合成的漫剧封面尺寸或格式无效")
		}
	}
	message := ""
	if err != nil {
		image = CoverImage{}
		message = "封面生成失败，请重试。"
		var public *ImageFailure
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			message = "封面生成超时，请重试。"
		case errors.As(err, &public):
			message = public.Message
		}
		slog.WarnContext(parent, "漫剧封面生成失败", "project_id", scope.ProjectID, "job_id", state.JobID, "error", err)
	}
	saveCtx, saveCancel := context.WithTimeout(parent, 10*time.Second)
	defer saveCancel()
	if err := s.coverRepository.FinishProjectCoverGeneration(saveCtx, scope, state.JobID, image, message); err != nil {
		slog.ErrorContext(parent, "保存漫剧封面任务失败", "project_id", scope.ProjectID, "job_id", state.JobID, "error", err)
	}
}

func (s *Service) SelectProjectCover(ctx context.Context, scope CoverScope, input CoverSelection) (CoverGeneration, error) {
	if !canonicalCanvasID(scope.ProjectID) || !canonicalCanvasID(input.JobID) || (input.Choice != "current" && input.Choice != "candidate") {
		return CoverGeneration{}, ErrInvalidInput
	}
	state, err := s.coverRepository.SelectProjectCover(ctx, scope, input)
	return state.withURLs(scope), err
}

func (s *Service) ProjectCoverImage(ctx context.Context, scope CoverScope, revision string) (CoverImage, error) {
	if !canonicalCanvasID(scope.ProjectID) || !canonicalCanvasID(revision) {
		return CoverImage{}, ErrNotFound
	}
	return s.coverRepository.ReadProjectCoverImage(ctx, scope, revision)
}
