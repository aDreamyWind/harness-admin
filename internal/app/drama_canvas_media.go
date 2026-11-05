package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/drama"
)

func (f *dramaMediaFiles) canvasMediaPath(project, id string) (string, error) {
	projectID, projectErr := uuid.Parse(project)
	mediaID, mediaErr := uuid.Parse(id)
	if projectErr != nil || mediaErr != nil {
		return "", drama.ErrNotFound
	}
	return filepath.Join(f.directory, "canvas", projectID.String(), mediaID.String()), nil
}

func (f *dramaMediaFiles) PutCanvasMedia(ctx context.Context, project, id string, input io.Reader, limit int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := f.canvasMediaPath(project, id)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建画布素材目录失败: %w", err)
	}
	file, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	written, writeErr := io.Copy(file, io.LimitReader(input, limit+1))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if written > limit {
		return drama.ErrCanvasMediaSize
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (f *dramaMediaFiles) OpenCanvasMedia(ctx context.Context, project, id string) (io.ReadSeekCloser, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	path, err := f.canvasMediaPath(project, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, drama.ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, time.Time{}, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, time.Time{}, errors.Join(drama.ErrNotFound, file.Close())
	}
	return file, info.ModTime(), nil
}
