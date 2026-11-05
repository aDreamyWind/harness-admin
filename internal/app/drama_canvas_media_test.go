package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"

	"harness-admin/internal/drama"
)

type canvasMediaProjectStub struct {
	drama.Repository
	projects map[string]bool
}

func (s canvasMediaProjectStub) Get(_ context.Context, org, user, project string) (drama.Project, error) {
	if org != "org" || user != "author" || !s.projects[project] {
		return drama.Project{}, drama.ErrNotFound
	}
	return drama.Project{ID: project}, nil
}

func TestDramaCanvasMediaIsScopedAndValidatesContent(t *testing.T) {
	ctx := context.Background()
	project, otherProject := uuid.NewString(), uuid.NewString()
	files := &dramaMediaFiles{directory: t.TempDir()}
	service := drama.NewService(canvasMediaProjectStub{projects: map[string]bool{project: true, otherProject: true}}, nil).WithCanvasMedia(files)
	inputs := []struct {
		kind string
		mime string
		data []byte
	}{
		{kind: "image", mime: "image/png", data: []byte("\x89PNG\r\n\x1a\n")},
		{kind: "video", mime: "video/mp4", data: []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")},
		{kind: "video", mime: "video/mp4", data: []byte("\x00\x00\x00\x14ftypisom\x00\x00\x00\x00iso6")},
		{kind: "video", mime: "video/webm", data: []byte("\x1a\x45\xdf\xa3\x87\x42\x82\x84webm")},
		{kind: "audio", mime: "audio/wav", data: []byte("RIFF\x00\x00\x00\x00WAVEfmt ")},
		{kind: "audio", mime: "audio/mpeg", data: []byte("\xff\xfb\x90\x64")},
		{kind: "audio", mime: "audio/ogg", data: []byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x13OpusHead")},
	}
	for _, in := range inputs {
		t.Run(in.mime, func(t *testing.T) {
			media, err := service.UploadCanvasMedia(ctx, "org", "author", project, bytes.NewReader(in.data))
			if err != nil || media.Kind != in.kind || media.MIME != in.mime {
				t.Fatalf("素材类型未正确识别: %+v %v", media, err)
			}
			result, err := service.ReadCanvasMedia(ctx, "org", "author", project, media.ID)
			if err != nil {
				t.Fatal(err)
			}
			read, err := io.ReadAll(result.File)
			if err = errors.Join(err, result.File.Close()); err != nil || !bytes.Equal(read, in.data) || result.MIME != in.mime {
				t.Fatalf("保存后的素材内容错误: %q %s %v", read, result.MIME, err)
			}
			if _, err := service.ReadCanvasMedia(ctx, "org", "another-user", project, media.ID); !errors.Is(err, drama.ErrNotFound) {
				t.Fatalf("其他用户可访问素材: %v", err)
			}
			if _, err := service.ReadCanvasMedia(ctx, "org", "author", otherProject, media.ID); !errors.Is(err, drama.ErrNotFound) {
				t.Fatalf("同一用户的另一项目可访问素材: %v", err)
			}
		})
	}
	for _, invalid := range []string{"<svg></svg>", "\x1a\x45\xdf\xa3\x8b\x42\x82\x88matroska"} {
		if _, err := service.UploadCanvasMedia(ctx, "org", "author", project, bytes.NewBufferString(invalid)); !errors.Is(err, drama.ErrCanvasMediaType) {
			t.Fatalf("不支持的格式应被拒绝: %v", err)
		}
	}
	if _, err := service.UploadCanvasMedia(ctx, "org", "other", project, bytes.NewReader(inputs[0].data)); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("他人项目可上传素材: %v", err)
	}
	id := uuid.NewString()
	if err := files.PutCanvasMedia(ctx, project, id, bytes.NewReader(inputs[0].data), 2); !errors.Is(err, drama.ErrCanvasMediaSize) {
		t.Fatalf("超限文件未被拒绝: %v", err)
	}
	if _, _, err := files.OpenCanvasMedia(ctx, project, id); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("超限文件被保留: %v", err)
	}
}
