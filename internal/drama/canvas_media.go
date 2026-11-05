package drama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const MaxCanvasMediaBytes = 100 << 20

var (
	ErrCanvasMediaType = errors.New("支持 PNG、JPG、WebP、GIF 图片，MP4、WebM 视频，或 MP3、WAV、Ogg 音频")
	ErrCanvasMediaSize = errors.New("图片不能超过 20 MiB，音频不能超过 30 MiB，视频不能超过 100 MiB")
)

type CanvasMedia struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	MIME string `json:"mime"`
}

type CanvasMediaContent struct {
	File      io.ReadSeekCloser
	MIME      string
	UpdatedAt time.Time
}

type CanvasMediaFiles interface {
	PutCanvasMedia(context.Context, string, string, io.Reader, int64) error
	OpenCanvasMedia(context.Context, string, string) (io.ReadSeekCloser, time.Time, error)
}

func (s *Service) WithCanvasMedia(files CanvasMediaFiles) *Service {
	s.canvasMedia = files
	return s
}

func (s *Service) UploadCanvasMedia(ctx context.Context, org, user, project string, input io.Reader) (CanvasMedia, error) {
	projectID, err := uuid.Parse(project)
	if err != nil || input == nil || s.canvasMedia == nil {
		return CanvasMedia{}, ErrInvalidInput
	}
	project = projectID.String()
	if _, err := s.Get(ctx, org, user, project); err != nil {
		return CanvasMedia{}, err
	}
	reader := bufio.NewReaderSize(input, 512)
	header, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
		return CanvasMedia{}, fmt.Errorf("读取画布素材头失败: %w", err)
	}
	kind, mime, limit, err := identifyCanvasMedia(header)
	if err != nil {
		return CanvasMedia{}, err
	}
	media := CanvasMedia{ID: uuid.NewString(), Kind: kind, MIME: mime}
	if err := s.canvasMedia.PutCanvasMedia(ctx, project, media.ID, reader, limit); err != nil {
		return CanvasMedia{}, err
	}
	return media, nil
}

func (s *Service) ReadCanvasMedia(ctx context.Context, org, user, project, id string) (CanvasMediaContent, error) {
	projectID, projectErr := uuid.Parse(project)
	mediaID, mediaErr := uuid.Parse(id)
	if projectErr != nil || mediaErr != nil || s.canvasMedia == nil {
		return CanvasMediaContent{}, ErrNotFound
	}
	project = projectID.String()
	if _, err := s.Get(ctx, org, user, project); err != nil {
		return CanvasMediaContent{}, err
	}
	file, updatedAt, err := s.canvasMedia.OpenCanvasMedia(ctx, project, mediaID.String())
	if err != nil {
		return CanvasMediaContent{}, err
	}
	var header [512]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return CanvasMediaContent{}, errors.Join(err, file.Close())
	}
	_, mime, _, err := identifyCanvasMedia(header[:n])
	if err != nil {
		return CanvasMediaContent{}, errors.Join(err, file.Close())
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return CanvasMediaContent{}, errors.Join(err, file.Close())
	}
	return CanvasMediaContent{File: file, MIME: mime, UpdatedAt: updatedAt}, nil
}

func identifyCanvasMedia(header []byte) (kind, mime string, limit int64, err error) {
	const imageLimit = 20 << 20
	const audioLimit = 30 << 20
	switch mime := http.DetectContentType(header); mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return "image", mime, imageLimit, nil
	case "video/mp4":
		return "video", mime, MaxCanvasMediaBytes, nil
	case "video/webm":
		// WebM 与 Matroska 共用 EBML 文件头。播放器需要 WebM 的文档类型。
		if bytes.Contains(header, []byte("\x42\x82\x84webm")) {
			return "video", mime, MaxCanvasMediaBytes, nil
		}
	case "audio/mpeg":
		return "audio", mime, audioLimit, nil
	case "audio/wave":
		return "audio", "audio/wav", audioLimit, nil
	case "application/ogg":
		if bytes.Contains(header, []byte("OpusHead")) || bytes.Contains(header, []byte("\x01vorbis")) {
			return "audio", "audio/ogg", audioLimit, nil
		}
	}
	// 一些 MP4 导出器只写 ISO BMFF 品牌，不带标准嗅探器要求的 mp4 品牌。
	if len(header) >= 16 && bytes.Equal(header[4:8], []byte("ftyp")) {
		size := binary.BigEndian.Uint32(header[:4])
		if size >= 16 && size <= uint32(len(header)) && size%4 == 0 {
			switch string(header[8:12]) {
			case "isom", "iso2", "iso5", "iso6", "avc1", "hvc1", "hev1", "dash", "M4V ":
				return "video", "video/mp4", MaxCanvasMediaBytes, nil
			}
		}
	}
	// 没有 ID3 标签的 MP3 可以直接以 MPEG 音频帧开始。
	if len(header) >= 4 && header[0] == 0xff && header[1]&0xe0 == 0xe0 && header[1]&0x18 != 0x08 &&
		header[1]&0x06 == 0x02 && header[2]&0xf0 != 0xf0 && header[2]&0x0c != 0x0c {
		return "audio", "audio/mpeg", audioLimit, nil
	}
	return "", "", 0, ErrCanvasMediaType
}
