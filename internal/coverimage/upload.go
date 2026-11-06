package coverimage

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const MaxBytes = 10 * 1024 * 1024

var ErrInvalidUpload = errors.New("请选择不超过 10 MiB、边长不超过 4096 像素的 JPEG、PNG 或 WebP 图片")

func Validate(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return "", ErrInvalidUpload
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return "", ErrInvalidUpload
	}
	switch format {
	case "jpeg", "png", "webp":
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return "", ErrInvalidUpload
		}
		return "image/" + format, nil
	default:
		return "", ErrInvalidUpload
	}
}
