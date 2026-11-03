package drama

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrSegmentVideoBusy = errors.New("此片段正在生成视频，请等待任务结束")

type SegmentVideo struct {
	ID        string    `json:"id"`
	SegmentID string    `json:"segmentId"`
	Revision  int       `json:"revision"`
	ModelCode string    `json:"modelCode"`
	Duration  int       `json:"duration"`
	ImageIDs  []string  `json:"imageIds"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type SegmentVideoScope struct{ OrganizationID, UserID, ProjectID, EpisodeID, RunID, SegmentID string }

type SegmentVideoRepository interface {
	CreateSegmentVideo(context.Context, SegmentVideoScope, SegmentVideo) (SegmentVideo, error)
	ListSegmentVideos(context.Context, SegmentVideoScope) ([]SegmentVideo, error)
	ReadSegmentVideo(context.Context, SegmentVideoScope, string) (SegmentVideo, error)
	FinishSegmentVideo(context.Context, string, string, string) error
	FailInterruptedSegmentVideos(context.Context) error
}

type SegmentVideoMedia interface {
	PutVideo(context.Context, string, []byte) error
	OpenVideo(context.Context, string) (io.ReadSeekCloser, time.Time, error)
}

type SegmentVideoRequest struct {
	OrganizationID, UserID, ModelCode, Prompt, AspectRatio string
	Duration                                               int
	ReferenceImages                                        []string
	ImageIDs                                               []string
}

type SegmentVideoGenerator interface {
	GenerateSegmentVideo(context.Context, SegmentVideoRequest) ([]byte, error)
}

type SegmentVideoJob struct {
	Video   SegmentVideo
	Scope   SegmentVideoScope
	Request SegmentVideoRequest
}

type SegmentVideoFailure struct {
	Message string
	Cause   error
}

func (e *SegmentVideoFailure) Error() string { return fmt.Sprintf("%s：%v", e.Message, e.Cause) }
func (e *SegmentVideoFailure) Unwrap() error { return e.Cause }

func (s *Service) WithSegmentVideos(repo SegmentVideoRepository, engine SegmentVideoGenerator, media SegmentVideoMedia) *Service {
	s.segmentVideos, s.videoGenerator, s.videoMedia = repo, engine, media
	return s
}

func validVideoScope(scope SegmentVideoScope) bool {
	return validVisualIDs(scope.ProjectID, scope.EpisodeID, scope.RunID, scope.SegmentID)
}

func (s *Service) StartSegmentVideo(ctx context.Context, scope SegmentVideoScope, revision int) (SegmentVideoJob, error) {
	if !validVideoScope(scope) || revision < 1 {
		return SegmentVideoJob{}, ErrInvalidInput
	}
	page, err := s.Storyboards(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID, scope.EpisodeID, scope.RunID)
	if err != nil {
		return SegmentVideoJob{}, err
	}
	run := page.Current
	if run == nil || run.Stale || run.Revision != revision || (run.Status != "ready" && run.Status != "confirmed") || len(page.Runs) == 0 || page.Runs[0].ID != run.ID {
		return SegmentVideoJob{}, ErrStoryboardConflict
	}
	var segment *StoryboardSegment
	for i := range run.Segments {
		if run.Segments[i].ID == scope.SegmentID {
			segment = &run.Segments[i]
			break
		}
	}
	if segment == nil {
		return SegmentVideoJob{}, ErrNotFound
	}
	if segment.Status != "ready" || len(segment.Shots) == 0 {
		return SegmentVideoJob{}, ErrStoryboardConflict
	}
	settings, err := s.GetSettings(ctx, scope.OrganizationID, scope.UserID)
	if err != nil {
		return SegmentVideoJob{}, err
	}
	if settings.VideoModelCode == "" {
		return SegmentVideoJob{}, fmt.Errorf("%w：请先在 AI 漫剧设置中选择视频模型", ErrModelNotConfigured)
	}
	forms := page.CatalogForms
	if forms == nil {
		forms = page.Forms
	}
	style, err := s.styles.ResolveStyle(ctx, run.StyleID)
	if err != nil {
		return SegmentVideoJob{}, err
	}
	request, err := planSegmentVideo(*segment, forms, style.VideoPrompt, run.AspectRatio)
	if err != nil {
		return SegmentVideoJob{}, err
	}
	request.OrganizationID, request.UserID, request.ModelCode = scope.OrganizationID, scope.UserID, settings.VideoModelCode
	video, err := s.segmentVideos.CreateSegmentVideo(ctx, scope, SegmentVideo{
		ID: uuid.NewString(), SegmentID: scope.SegmentID, Revision: revision, ModelCode: request.ModelCode,
		Duration: request.Duration, ImageIDs: request.ImageIDs,
	})
	return SegmentVideoJob{Video: video, Scope: scope, Request: request}, err
}

func planSegmentVideo(seg StoryboardSegment, catalog []AssetForm, style, ratio string) (SegmentVideoRequest, error) {
	total := storyboardSeconds(seg)
	duration := int(math.Ceil(total))
	if duration < 2 || duration > 30 {
		return SegmentVideoRequest{}, fmt.Errorf("%w：片段时长为 %.1f 秒，当前视频模型支持 2–30 秒，请调整分镜后再生成", ErrInvalidInput, total)
	}
	if ratio != "16:9" && ratio != "9:16" && ratio != "1:1" {
		return SegmentVideoRequest{}, fmt.Errorf("%w：当前视频模型不支持此分镜画幅", ErrInvalidInput)
	}
	if len(seg.FormIDs) > 10 {
		return SegmentVideoRequest{}, fmt.Errorf("%w：当前视频模型每片段最多使用 10 张参考图，请调整片段的资产引用", ErrInvalidInput)
	}
	forms := make(map[string]AssetForm, len(catalog))
	for _, form := range catalog {
		forms[form.ID] = form
	}
	bindings := make(map[string]string, len(seg.Bindings))
	for _, binding := range seg.Bindings {
		bindings[binding.FormID] = binding.ImageID
	}

	req := SegmentVideoRequest{Duration: duration, AspectRatio: ratio, ImageIDs: []string{}}
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "画面风格：%s\n场次：%s\n动作和声音按镜头顺序表现，保持人物、场景与道具连续。对白按原句呈现，避免额外口播。\n", style, seg.Scene)
	missing := []string{}
	for i, id := range seg.FormIDs {
		form, ok := forms[id]
		if !ok {
			return SegmentVideoRequest{}, fmt.Errorf("%w：片段引用的形象已不可用，请重新选择资产", ErrInvalidInput)
		}
		image := bindings[id]
		if image == "" {
			image = form.CurrentImageID
		}
		if image == "" {
			missing = append(missing, form.AssetName+" · "+form.Name)
			continue
		}
		req.ImageIDs = append(req.ImageIDs, image)
		fmt.Fprintf(&prompt, "图%d（Image %d）：%s · %s（%s）\n", i+1, i+1, form.AssetName, form.Name, form.Kind)
	}
	if len(missing) > 0 {
		return SegmentVideoRequest{}, fmt.Errorf("%w：请先为以下形象生成参考图：%s", ErrInvalidInput, strings.Join(missing, "、"))
	}
	numbers := make(map[string]int, len(seg.FormIDs))
	for i, id := range seg.FormIDs {
		numbers[id] = i + 1
	}
	elapsed := 0.0
	for i, shot := range seg.Shots {
		fmt.Fprintf(&prompt, "\n镜头%d（%.1f–%.1f秒，%s，%s）：%s\n", i+1, elapsed, elapsed+shot.Duration, shot.ShotSize, shot.Camera, videoPicture(shot, numbers))
		elapsed += shot.Duration
		if shot.Sound != "" {
			fmt.Fprintf(&prompt, "音效：%s\n", shot.Sound)
		}
		for _, voice := range shot.Utterances {
			if voice.Delivery == "visual" {
				continue
			}
			label := map[string]string{"dialogue": "对白", "os": "内心独白", "vo": "画外音"}[voice.Kind]
			fmt.Fprintf(&prompt, "%s（%s）：%s\n", label, voice.Speaker, voice.Text)
		}
	}
	req.Prompt = prompt.String()
	if utf8.RuneCountInString(req.Prompt) > 20000 {
		return SegmentVideoRequest{}, fmt.Errorf("%w：片段内容超出视频模型的文本限制，请缩短片段", ErrInvalidInput)
	}
	return req, nil
}

func videoPicture(shot StoryboardShot, numbers map[string]int) string {
	var picture strings.Builder
	chars := []rune(shot.Description)
	start := 0
	for _, ref := range shot.DescriptionMentions {
		n := numbers[ref.FormID]
		if n == 0 || ref.Start < start || ref.End <= ref.Start || ref.End > len(chars) {
			continue
		}
		picture.WriteString(string(chars[start:ref.End]))
		fmt.Fprintf(&picture, "（图%d）", n)
		start = ref.End
	}
	picture.WriteString(string(chars[start:]))
	return picture.String()
}

func (s *Service) GenerateSegmentVideo(job SegmentVideoJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	request := job.Request
	err := s.loadSegmentVideoImages(ctx, job.Scope, &request)
	if err == nil {
		var data []byte
		data, err = s.videoGenerator.GenerateSegmentVideo(ctx, request)
		if err == nil {
			err = s.videoMedia.PutVideo(ctx, job.Video.ID, data)
		}
	}
	status, message := "succeeded", ""
	if err != nil {
		status, message = "failed", "片段视频生成失败，请查看模型调用记录或重试"
		var failure *SegmentVideoFailure
		if errors.As(err, &failure) {
			message = failure.Message
		}
	}
	finishCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	return errors.Join(err, s.segmentVideos.FinishSegmentVideo(finishCtx, job.Video.ID, status, message))
}

func (s *Service) loadSegmentVideoImages(ctx context.Context, scope SegmentVideoScope, request *SegmentVideoRequest) error {
	total := 0
	for _, id := range request.ImageIDs {
		image, err := s.visual.ReadImage(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID, id)
		if err != nil {
			return err
		}
		data, mime, err := s.files.Get(ctx, image.StorageKey)
		if err != nil {
			return err
		}
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
			return &SegmentVideoFailure{Message: "参考图格式不受视频模型支持，请使用 PNG、JPEG 或 WebP 图片", Cause: ErrInvalidInput}
		}
		total += len(data)
		if total > 40*1024*1024 {
			return &SegmentVideoFailure{Message: "片段参考图总大小超过 40 MB，请缩小参考图后重试", Cause: ErrInvalidInput}
		}
		request.ReferenceImages = append(request.ReferenceImages, imageDataURI(mime, data))
	}
	return nil
}

func (s *Service) SegmentVideos(ctx context.Context, scope SegmentVideoScope) ([]SegmentVideo, error) {
	if !validVideoScope(scope) {
		return nil, ErrInvalidInput
	}
	return s.segmentVideos.ListSegmentVideos(ctx, scope)
}

func (s *Service) SegmentVideoContent(ctx context.Context, scope SegmentVideoScope, id string) (io.ReadSeekCloser, time.Time, error) {
	if !validVideoScope(scope) || !validVisualIDs(id) {
		return nil, time.Time{}, ErrInvalidInput
	}
	video, err := s.segmentVideos.ReadSegmentVideo(ctx, scope, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	if video.Status != "succeeded" {
		return nil, time.Time{}, ErrNotFound
	}
	return s.videoMedia.OpenVideo(ctx, video.ID)
}
