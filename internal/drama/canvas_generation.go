package drama

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxCanvasReferenceImageBytes = 10 << 20
	maxCanvasReferenceBytes      = 40 << 20
	maxCanvasGenerationRunes     = 20000
	CanvasResolution1K           = "1K"
	CanvasResolution2K           = "2K"
)

type CanvasGenerationScope struct{ OrganizationID, UserID, ProjectID, NodeID string }

type CanvasGenerationInput struct {
	ModelCode   string `json:"modelCode"`
	Revision    int    `json:"revision"`
	Prompt      string `json:"prompt"`
	StyleID     string `json:"styleId"`
	AspectRatio string `json:"aspectRatio"`
	Resolution  string `json:"resolution"`
	Duration    int    `json:"duration"`
}

type CanvasGenerationJob struct {
	ID            string     `json:"id"`
	NodeID        string     `json:"nodeId"`
	Kind          string     `json:"kind"`
	ModelCode     string     `json:"modelCode"`
	Status        string     `json:"status"`
	ResultURL     string     `json:"resultUrl,omitempty"`
	ResultMediaID string     `json:"-"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
}

type CanvasGenerationModel struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	MinReferences int      `json:"minReferences"`
	MaxReferences int      `json:"maxReferences"`
	Resolutions   []string `json:"resolutions,omitempty"`
	MinDuration   int      `json:"minDuration,omitempty"`
	MaxDuration   int      `json:"maxDuration,omitempty"`
}

type CanvasGenerationRequest struct {
	OrganizationID, UserID, JobID, ModelCode, Kind, Prompt, AspectRatio string
	Resolution                                                          string
	Duration                                                            int
	ReferenceImages, ReferenceIDs                                       []string
}

type CanvasGenerationEngine interface {
	GenerateCanvasMedia(context.Context, CanvasGenerationRequest) ([]byte, string, error)
	CanvasGenerationModels(context.Context, string, string) ([]CanvasGenerationModel, error)
}

type CanvasGenerationRepository interface {
	ReserveCanvasGeneration(context.Context, CanvasGenerationScope, int, CanvasGenerationJob, time.Time) (CanvasGenerationJob, bool, error)
	ReadCanvasGeneration(context.Context, CanvasGenerationScope) (*CanvasGenerationJob, error)
	ListCanvasGenerations(context.Context, CanvasGenerationScope) ([]CanvasGenerationJob, error)
	FinishCanvasGeneration(context.Context, CanvasGenerationScope, string, string) error
}

type CanvasGenerationFailure struct {
	Message string
	Cause   error
}

func (e *CanvasGenerationFailure) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s：%v", e.Message, e.Cause)
}

func (e *CanvasGenerationFailure) Unwrap() error { return e.Cause }

func (s *Service) WithCanvasGeneration(repo CanvasGenerationRepository, engine CanvasGenerationEngine) *Service {
	s.canvasJobs, s.canvasEngine = repo, engine
	return s
}

func (s *Service) ListCanvasGenerationModels(ctx context.Context, org, user string) ([]CanvasGenerationModel, error) {
	if s.canvasEngine == nil {
		return []CanvasGenerationModel{}, nil
	}
	return s.canvasEngine.CanvasGenerationModels(ctx, org, user)
}

func (s *Service) GetCanvasGeneration(ctx context.Context, org, user, project, node string) (*CanvasGenerationJob, error) {
	if s.canvasJobs == nil || !canonicalCanvasID(project) || !canonicalCanvasID(node) {
		return nil, ErrInvalidInput
	}
	job, err := s.canvasJobs.ReadCanvasGeneration(ctx, CanvasGenerationScope{org, user, project, node})
	if job != nil {
		job.ResultURL = canvasGenerationResultURL(project, job.ResultMediaID)
	}
	return job, err
}

func (s *Service) ListCanvasGenerations(ctx context.Context, org, user, project string) ([]CanvasGenerationJob, error) {
	if s.canvasJobs == nil || !canonicalCanvasID(project) {
		return nil, ErrInvalidInput
	}
	jobs, err := s.canvasJobs.ListCanvasGenerations(ctx, CanvasGenerationScope{OrganizationID: org, UserID: user, ProjectID: project})
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i].ResultURL = canvasGenerationResultURL(project, jobs[i].ResultMediaID)
	}
	if jobs == nil {
		return []CanvasGenerationJob{}, nil
	}
	return jobs, nil
}

func canvasGenerationResultURL(project, media string) string {
	if media == "" {
		return ""
	}
	return "/api/v1/ai-drama/projects/" + project + "/canvas/media/" + media + "/content"
}

type canvasGenerationSource struct {
	id, title, url, imageID string
}

func (s *Service) StartCanvasGeneration(ctx context.Context, org, user, project, node string, in CanvasGenerationInput) (CanvasGenerationJob, error) {
	if s.canvasJobs == nil || s.canvasEngine == nil || s.canvasMedia == nil || !canonicalCanvasID(project) || !canonicalCanvasID(node) || in.Revision < 1 {
		return CanvasGenerationJob{}, ErrInvalidInput
	}
	scope := CanvasGenerationScope{org, user, project, node}
	current, err := s.canvasJobs.ReadCanvasGeneration(ctx, scope)
	if err != nil {
		return CanvasGenerationJob{}, err
	}
	if current != nil && current.Status == "running" {
		current.ResultURL = canvasGenerationResultURL(project, current.ResultMediaID)
		return *current, nil
	}
	canvas, err := s.Canvas(ctx, org, user, project)
	if err != nil {
		return CanvasGenerationJob{}, err
	}
	if canvas.Revision != in.Revision {
		return CanvasGenerationJob{}, ErrCanvasConflict
	}
	request, sources, err := planCanvasGeneration(canvas, node, in)
	if err != nil {
		return CanvasGenerationJob{}, err
	}
	models, err := s.ListCanvasGenerationModels(ctx, org, user)
	if err != nil {
		return CanvasGenerationJob{}, err
	}
	if err := validateCanvasGenerationModel(models, request, len(sources)); err != nil {
		return CanvasGenerationJob{}, err
	}
	if styleID := strings.TrimSpace(in.StyleID); styleID != "" {
		if s.styles == nil {
			return CanvasGenerationJob{}, ErrStyleUnavailable
		}
		style, err := s.styles.ResolveCatalogStyle(ctx, styleID)
		if err != nil {
			return CanvasGenerationJob{}, err
		}
		stylePrompt := style.ImagePrompt
		if request.Kind == "video" {
			stylePrompt = style.VideoPrompt
		}
		request.Prompt += fmt.Sprintf("\n\n画面风格：%s\n风格要求：%s", style.Name, stylePrompt)
		if utf8.RuneCountInString(request.Prompt) > maxCanvasGenerationRunes {
			return CanvasGenerationJob{}, fmt.Errorf("%w：提示词、参考素材和风格要求合计不能超过 %d 字", ErrInvalidInput, maxCanvasGenerationRunes)
		}
	}
	if err := s.resolveCanvasGenerationSources(ctx, scope, sources); err != nil {
		return CanvasGenerationJob{}, err
	}
	request.OrganizationID, request.UserID, request.JobID = org, user, uuid.NewString()
	timeout := 12 * time.Minute
	if request.Kind == "video" {
		timeout = 25 * time.Minute
	}
	parent := context.WithoutCancel(ctx)
	reserveCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	job, started, err := s.canvasJobs.ReserveCanvasGeneration(reserveCtx, scope, canvas.Revision, CanvasGenerationJob{
		ID: request.JobID, NodeID: node, Kind: request.Kind, ModelCode: request.ModelCode, Status: "running",
	}, time.Now().Add(timeout+30*time.Second))
	if err != nil {
		return CanvasGenerationJob{}, err
	}
	if started {
		go s.runCanvasGeneration(parent, scope, request, sources, timeout)
	}
	job.ResultURL = canvasGenerationResultURL(project, job.ResultMediaID)
	return job, nil
}

func planCanvasGeneration(canvas Canvas, node string, in CanvasGenerationInput) (CanvasGenerationRequest, []canvasGenerationSource, error) {
	var request CanvasGenerationRequest
	if canvas.Extras == nil {
		return request, nil, ErrNotFound
	}
	var target CanvasCard
	for _, card := range canvas.Extras.Cards {
		if card.ID == node {
			target = card
			break
		}
	}
	if target.ID == "" {
		return request, nil, ErrNotFound
	}
	if target.Kind != "image" && target.Kind != "video" {
		return request, nil, fmt.Errorf("%w：只有图片和视频节点支持生成", ErrInvalidInput)
	}
	request.Kind = target.Kind
	request.ModelCode = strings.TrimSpace(in.ModelCode)
	request.AspectRatio = strings.TrimSpace(in.AspectRatio)
	request.Resolution = strings.TrimSpace(in.Resolution)
	request.Duration = in.Duration
	if request.AspectRatio == "" {
		request.AspectRatio = "16:9"
	}
	if !validCanvasText(request.ModelCode, 160) || (request.AspectRatio != "16:9" && request.AspectRatio != "9:16" && request.AspectRatio != "1:1") ||
		(request.Kind == "image" && request.Duration != 0) || strings.ContainsRune(in.Prompt, 0) || utf8.RuneCountInString(in.Prompt) > 10000 ||
		(in.StyleID != "" && !validCanvasText(in.StyleID, 80)) {
		return request, nil, ErrInvalidInput
	}
	if request.Resolution != "" && (request.Kind != "image" || (request.Resolution != CanvasResolution1K && request.Resolution != CanvasResolution2K)) {
		return request, nil, fmt.Errorf("%w：分辨率只适用于图片节点，可选 1K 或 2K", ErrInvalidInput)
	}
	if request.Kind == "video" && request.Duration == 0 {
		request.Duration = 5
	}
	texts, sources := canvasGenerationSources(canvas, node)
	var parts []string
	if own := strings.TrimSpace(in.Prompt); own != "" {
		parts = append(parts, own)
	}
	parts = append(parts, texts...)
	if len(parts) == 0 {
		if len(sources) == 0 {
			return request, nil, fmt.Errorf("%w：请填写提示词或连入文本、图片节点", ErrInvalidInput)
		}
		if request.Kind == "image" {
			parts = append(parts, "根据参考图生成一张画面，保留主要人物与场景特征。")
		} else {
			parts = append(parts, "根据参考图生成一段连贯的视频，保留主要人物和场景特征。")
		}
	}
	request.Prompt = strings.Join(parts, "\n\n") + canvasReferenceGuide(sources)
	if utf8.RuneCountInString(request.Prompt) > maxCanvasGenerationRunes {
		return request, nil, fmt.Errorf("%w：提示词与参考素材合计不能超过 %d 字", ErrInvalidInput, maxCanvasGenerationRunes)
	}
	return request, sources, nil
}

func canvasGenerationSources(canvas Canvas, node string) ([]string, []canvasGenerationSource) {
	if canvas.Extras == nil {
		return nil, nil
	}
	cards := make(map[string]CanvasCard, len(canvas.Extras.Cards))
	for _, card := range canvas.Extras.Cards {
		cards[card.ID] = card
	}
	hidden := make(map[string]bool, len(canvas.Extras.Hidden))
	for _, id := range canvas.Extras.Hidden {
		hidden[id] = true
	}
	forms := make(map[string]AssetForm, len(canvas.Forms))
	for _, form := range canvas.Forms {
		if !hidden[form.ID] {
			forms[form.ID] = form
		}
	}
	var texts []string
	var sources []canvasGenerationSource
	seen := make(map[string]bool)
	// 画布以数组保存手工边。按连入顺序固定参考编号，同一节点只引用一次。
	for _, link := range canvas.Extras.Links {
		if link.Target != node || seen[link.Source] {
			continue
		}
		seen[link.Source] = true
		if card, ok := cards[link.Source]; ok {
			switch card.Kind {
			case "text":
				if content := strings.TrimSpace(card.Content); content != "" {
					texts = append(texts, fmt.Sprintf("参考文案「%s」\n%s", card.Title, content))
				}
			case "image":
				sources = append(sources, canvasGenerationSource{id: card.ID, title: card.Title, url: card.Content})
			}
		} else if form, ok := forms[link.Source]; ok && form.CurrentImageID != "" {
			sources = append(sources, canvasGenerationSource{id: form.ID, title: canvasAssetReferenceTitle(form), imageID: form.CurrentImageID})
		}
	}
	return texts, sources
}

func canvasAssetReferenceTitle(form AssetForm) string {
	if form.ParentID != "" && form.Name != "" && form.Name != form.AssetName {
		return form.AssetName + " · " + form.Name
	}
	return form.AssetName
}

func canvasReferenceGuide(sources []canvasGenerationSource) string {
	if len(sources) == 0 {
		return ""
	}
	var guide strings.Builder
	guide.WriteString("\n\n参考图顺序：")
	for i, source := range sources {
		fmt.Fprintf(&guide, "\n图%d（Image %d）：%s", i+1, i+1, strings.Join(strings.Fields(source.title), " "))
	}
	return guide.String()
}

func (s *Service) resolveCanvasGenerationSources(ctx context.Context, scope CanvasGenerationScope, sources []canvasGenerationSource) error {
	for i := range sources {
		source := &sources[i]
		if source.url != "" {
			if _, local := canvasMediaReferenceID(scope.ProjectID, source.url); !local && !validCanvasExternalImage(source.url) {
				return fmt.Errorf("%w：参考图「%s」请使用项目内的图片或公网 HTTPS 链接", ErrInvalidInput, source.title)
			}
			continue
		}
		if source.imageID != "" {
			continue
		}
		if s.canvasJobs == nil {
			return ErrModelUnavailable
		}
		sourceScope := scope
		sourceScope.NodeID = source.id
		previous, err := s.canvasJobs.ReadCanvasGeneration(ctx, sourceScope)
		if err != nil {
			return err
		}
		if previous == nil || previous.ResultMediaID == "" {
			return fmt.Errorf("%w：参考图「%s」还没有图片，请先上传或生成", ErrInvalidInput, source.title)
		}
		source.url = canvasGenerationResultURL(scope.ProjectID, previous.ResultMediaID)
	}
	return nil
}

func validateCanvasGenerationModel(models []CanvasGenerationModel, request CanvasGenerationRequest, count int) error {
	for _, model := range models {
		if model.Code != request.ModelCode || model.Kind != request.Kind {
			continue
		}
		if count < model.MinReferences || count > model.MaxReferences {
			if model.MaxReferences == 0 {
				return fmt.Errorf("%w：所选模型只支持文字生成，请更换模型或断开参考图", ErrInvalidInput)
			}
			return fmt.Errorf("%w：所选模型支持 %d 至 %d 张参考图，当前已连入 %d 张", ErrInvalidInput, model.MinReferences, model.MaxReferences, count)
		}
		if request.Kind == "video" && (request.Duration < model.MinDuration || request.Duration > model.MaxDuration) {
			return fmt.Errorf("%w：所选模型支持 %d 至 %d 秒", ErrInvalidInput, model.MinDuration, model.MaxDuration)
		}
		if request.Resolution != "" && !slices.Contains(model.Resolutions, request.Resolution) {
			return fmt.Errorf("%w：所选模型不支持 %s 分辨率，请调整尺寸或更换模型", ErrInvalidInput, request.Resolution)
		}
		return nil
	}
	return ErrModelUnavailable
}

func (s *Service) runCanvasGeneration(parent context.Context, scope CanvasGenerationScope, req CanvasGenerationRequest, sources []canvasGenerationSource, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := s.loadCanvasGenerationSources(ctx, scope, &req, sources)
	if err == nil {
		var data []byte
		data, _, err = s.canvasEngine.GenerateCanvasMedia(ctx, req)
		if err == nil {
			header := data[:min(len(data), 512)]
			kind, _, limit, mediaErr := identifyCanvasMedia(header)
			switch {
			case mediaErr != nil || kind != req.Kind:
				err = &CanvasGenerationFailure{Message: "模型返回的素材格式不受支持"}
			case int64(len(data)) > limit:
				err = ErrCanvasMediaSize
			default:
				err = s.canvasMedia.PutCanvasMedia(ctx, scope.ProjectID, req.JobID, bytes.NewReader(data), limit)
			}
		}
	}
	var message string
	if err != nil {
		message = "画布素材生成失败，请查看调用记录或重试"
		var failure *CanvasGenerationFailure
		switch {
		case errors.As(err, &failure) && failure.Message != "":
			message = failure.Message
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			message = "等待模型超时，请查看调用记录后重试"
		case errors.Is(err, ErrCanvasMediaSize):
			message = ErrCanvasMediaSize.Error()
		}
		slog.WarnContext(parent, "画布素材生成失败", "job_id", req.JobID, "error", err)
	}
	finishCtx, finish := context.WithTimeout(parent, 10*time.Second)
	defer finish()
	if err := s.canvasJobs.FinishCanvasGeneration(finishCtx, scope, req.JobID, message); err != nil {
		slog.ErrorContext(parent, "保存画布素材生成状态失败", "job_id", req.JobID, "error", err)
	}
}

func (s *Service) loadCanvasGenerationSources(ctx context.Context, scope CanvasGenerationScope, req *CanvasGenerationRequest, sources []canvasGenerationSource) error {
	total := 0
	for _, source := range sources {
		var data []byte
		var mime string
		var err error
		var reference string
		if source.imageID != "" {
			data, mime, err = s.ImageContent(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID, source.imageID)
		} else if id, ok := canvasMediaReferenceID(scope.ProjectID, source.url); ok {
			var file io.ReadSeekCloser
			file, _, err = s.canvasMedia.OpenCanvasMedia(ctx, scope.ProjectID, id)
			if err == nil {
				data, err = io.ReadAll(io.LimitReader(file, maxCanvasReferenceImageBytes+1))
				err = errors.Join(err, file.Close())
				if err == nil {
					_, mime, _, err = identifyCanvasMedia(data[:min(len(data), 512)])
				}
			}
		} else if validCanvasExternalImage(source.url) {
			reference = source.url
		} else {
			err = ErrInvalidInput
		}
		if err != nil {
			return &CanvasGenerationFailure{Message: fmt.Sprintf("无法读取参考图「%s」，请检查图片链接或重新上传", source.title), Cause: err}
		}
		if reference == "" {
			total += len(data)
			if total > maxCanvasReferenceBytes || len(data) > maxCanvasReferenceImageBytes {
				return &CanvasGenerationFailure{Message: "参考图单张不能超过 10 MiB，合计不能超过 40 MiB"}
			}
			if len(data) == 0 || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" && (req.Kind != "image" || mime != "image/gif")) {
				return &CanvasGenerationFailure{Message: fmt.Sprintf("参考图「%s」的格式不受当前节点支持，请使用 PNG、JPEG 或 WebP 图片", source.title)}
			}
			reference = imageDataURI(mime, data)
		}
		req.ReferenceImages = append(req.ReferenceImages, reference)
		req.ReferenceIDs = append(req.ReferenceIDs, source.id)
	}
	return nil
}

func canvasMediaReferenceID(project, reference string) (string, bool) {
	media, ok := strings.CutPrefix(reference, "/api/v1/ai-drama/projects/"+project+"/canvas/media/")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(media, "/content")
	return id, ok && canonicalCanvasID(id)
}

func validCanvasExternalImage(reference string) bool {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.User != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate()
	}
	return strings.Contains(host, ".") && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local")
}
