package drama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"harness-admin/internal/coverimage"
)

type coverJobStub struct {
	CoverRepository
	state    CoverGeneration
	result   chan CoverImage
	message  chan string
	uploaded CoverImage
}

func (r *coverJobStub) GetProjectCoverGeneration(context.Context, CoverScope) (CoverGeneration, error) {
	return r.state, nil
}

func (r *coverJobStub) ReserveProjectCoverGeneration(_ context.Context, _ CoverScope, _, id string, in CoverGenerationInput, _ time.Time) (CoverGeneration, bool, error) {
	r.state = CoverGeneration{JobID: id, ModelCode: in.ModelCode, Instruction: in.Instruction, Status: CoverRunning}
	return r.state, true, nil
}

func (r *coverJobStub) SaveProjectCoverUpload(_ context.Context, _ CoverScope, revision string, image CoverImage) (CoverGeneration, error) {
	r.uploaded = image
	return CoverGeneration{CoverID: revision, Status: CoverIdle}, nil
}

func (r *coverJobStub) FinishProjectCoverGeneration(_ context.Context, _ CoverScope, _ string, image CoverImage, message string) error {
	r.result <- image
	r.message <- message
	return nil
}

type coverStyleStub struct {
	StyleRepository
}

func (coverStyleStub) ResolveStyle(_ context.Context, id string) (VisualStyle, error) {
	if id != "dark-comic" {
		return VisualStyle{}, ErrStyleUnavailable
	}
	return VisualStyle{StyleOption: StyleOption{Name: "暗黑漫画风"}, ImagePrompt: "粗墨线和硬朗阴影"}, nil
}

func TestDramaProjectCoverRunsIndependentlyAndUsesProjectStyle(t *testing.T) {
	scope := CoverScope{OrganizationID: "org", UserID: "user", ProjectID: uuid.NewString()}
	repo := &fakeRepository{settings: Settings{ImageModelCode: "painter"}, project: Project{
		ID: scope.ProjectID, Title: "风雪城门", StyleID: "dark-comic", Style: "暗黑漫画风",
		Overview: json.RawMessage(`{"protagonist":"林川","synopsis":"侠客在雪夜守城门"}`),
	}}
	jobs := &coverJobStub{state: CoverGeneration{Status: CoverIdle}, result: make(chan CoverImage, 1), message: make(chan string, 1)}
	release := make(chan struct{})
	service := NewService(repo, &fakeWriter{}).WithStyles(coverStyleStub{}).WithProjectCovers(jobs,
		func(ctx context.Context, jobID string, in ImageRequest) ([]byte, error) {
			<-release
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if jobID != jobs.state.JobID || in.OrganizationID != scope.OrganizationID || in.UserID != scope.UserID ||
				in.ModelCode != "painter" || in.AspectRatio != "2:3" || !strings.Contains(in.Prompt, "侠客在雪夜守城门") ||
				!strings.Contains(in.Prompt, "粗墨线和硬朗阴影") || !strings.Contains(in.Prompt, "下方约三分之一") ||
				!strings.Contains(in.Prompt, "禁止任何文字") || !strings.Contains(in.Prompt, "突出提灯的火光") {
				return nil, errors.New("图片模型收到的封面参数错误")
			}
			return []byte("位图"), nil
		}, func(data []byte, design CoverTitleDesign) ([]byte, string, error) {
			if string(data) != "位图" || design.Title != repo.project.Title || design.Style != CoverTitleNoir ||
				(design.Placement != CoverTitleBottomLeft && design.Placement != CoverTitleBottomRight) {
				return nil, "", errors.New("合成未收到项目标题、标题样式或模型图片")
			}
			return []byte("<svg><text>风雪城门</text></svg>"), "image/svg+xml", nil
		})
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := service.StartProjectCoverGeneration(ctx, scope, CoverGenerationInput{ModelCode: "writer"}); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("未拒绝非图片模型: %v", err)
	}
	if _, err := service.StartProjectCoverGeneration(ctx, scope, CoverGenerationInput{Instruction: strings.Repeat("灯", 501)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("未拒绝过长的画面要求: %v", err)
	}
	job, err := service.StartProjectCoverGeneration(ctx, scope, CoverGenerationInput{Instruction: " 突出提灯的火光 "})
	if err != nil || job.Status != CoverRunning || job.JobID == "" || job.Instruction != "突出提灯的火光" {
		t.Fatalf("未建立后台任务: %+v %v", job, err)
	}
	if again, err := service.StartProjectCoverGeneration(ctx, scope, CoverGenerationInput{ModelCode: "painter"}); err != nil || again.JobID != job.JobID {
		t.Fatalf("重复请求未复用任务: %+v %v", again, err)
	}
	cancel()
	close(release)
	select {
	case result := <-jobs.result:
		if message := <-jobs.message; message != "" || !strings.Contains(string(result.Data), "风雪城门") || result.MIME != "image/svg+xml" {
			t.Fatalf("离开请求后未生成带名称的封面: %+v %q", result, message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未收到后台任务结果")
	}
}

func TestDramaCoverUploadOnlyAcceptsVerifiedRasterImages(t *testing.T) {
	ctx := context.Background()
	scope := CoverScope{ProjectID: uuid.NewString()}
	jobs := &coverJobStub{}
	service := (&Service{}).WithProjectCovers(jobs, nil, nil)
	if _, err := service.UploadProjectCover(ctx, scope, []byte(`<svg><script>alert(1)</script></svg>`)); !errors.Is(err, coverimage.ErrInvalidUpload) {
		t.Fatalf("不应允许上传 SVG: %v", err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	state, err := service.UploadProjectCover(ctx, scope, picture.Bytes())
	if err != nil || state.CoverURL != ProjectCoverImageURL(scope.ProjectID, state.CoverID) || state.CoverURL == "" ||
		jobs.uploaded.MIME != "image/png" || !bytes.Equal(jobs.uploaded.Data, picture.Bytes()) {
		t.Fatalf("上传图片没有保留已验证的媒体类型或原图: %+v %v", state, err)
	}
}

func TestDramaCoverTitleDesignUsesProjectStyleAndStoryGenre(t *testing.T) {
	tests := []struct {
		title, id, name, genre string
		alternate              bool
		style                  CoverTitleStyle
		placement              CoverTitlePlacement
		reserve                string
	}{
		{"芦苇渡", "ink-wash", "东方水墨画风", "悬疑", false, CoverTitleInk, CoverTitleRightVertical, "右侧约四分之一"},
		{"芦苇渡：潮汐灯塔往事", "ink-wash", "东方水墨画风", "悬疑", false, CoverTitleInk, CoverTitleTopRight, "重点偏右"},
		{"道爷要飞升", "chinese-mythology", "中国神话风", "修仙", false, CoverTitleMyth, CoverTitleBottomLeft, "重点偏左"},
		{"道爷要飞升", "chinese-mythology", "中国神话风", "修仙", true, CoverTitleMyth, CoverTitleTopRight, "重点偏右"},
		{"轨道以外", "", "历史画风", "科幻", false, CoverTitleNeon, CoverTitleTopLeft, "重点偏左"},
	}
	for _, tt := range tests {
		design := chooseCoverTitleDesign(tt.title, tt.id, tt.name, tt.genre, tt.alternate)
		if design.Title != tt.title || design.Style != tt.style || design.Placement != tt.placement || !strings.Contains(design.reserveSpace(), tt.reserve) {
			t.Errorf("剧名 %q 的标题设计错误：%+v，留白 %q", tt.title, design, design.reserveSpace())
		}
	}
}
