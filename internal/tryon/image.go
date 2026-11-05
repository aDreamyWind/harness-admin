package tryon

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var imageMIME = map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}

func imageInfo(data []byte, maxBytes int) (string, error) {
	if len(data) == 0 || len(data) > maxBytes {
		return "", invalid("图片为空或超过大小限制")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageMIME[format] == "" {
		return "", invalid("请上传可读取的 JPG、PNG 或 WebP 图片")
	}
	if info.Width < 1 || info.Height < 1 || info.Width > 8000 || info.Height > 8000 || info.Width*info.Height > 24_000_000 {
		return "", invalid("图片尺寸过大，请缩小后再试")
	}
	return imageMIME[format], nil
}

// 预览图可以原样提交入库。过大的结果与 WebP 统一转成常用 JPEG。
func prepareGeneratedAsset(data []byte) (Image, error) {
	mime, err := imageInfo(data, MaxResultBytes)
	if err != nil {
		return Image{}, err
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, invalid("模型返回的图片内容损坏")
	}
	if len(data) <= MaxInputBytes && mime != "image/webp" {
		return Image{Data: data, MIME: mime}, nil
	}
	size := source.Bounds().Size()
	scale := math.Min(1, 2048/float64(max(size.X, size.Y)))
	canvas := image.NewRGBA(image.Rect(0, 0,
		max(1, int(math.Round(float64(size.X)*scale))),
		max(1, int(math.Round(float64(size.Y)*scale))),
	))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), source, source.Bounds(), draw.Over, nil)
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, canvas, &jpeg.Options{Quality: 92}); err != nil {
		return Image{}, err
	}
	if buffer.Len() > MaxInputBytes {
		return Image{}, invalid("生成图片超过素材大小限制")
	}
	return Image{Data: buffer.Bytes(), MIME: "image/jpeg"}, nil
}

func prepareImage(data []byte, box *Box) (Image, error) {
	mime, err := imageInfo(data, MaxInputBytes)
	if err != nil {
		return Image{}, err
	}
	if box != nil && !box.Valid() {
		return Image{}, invalid("请框选有效的衣物区域")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, invalid("图片内容损坏，请重新上传")
	}
	if box == nil {
		return Image{Data: data, MIME: mime}, nil
	}
	bounds := source.Bounds()
	x, y := float64(bounds.Dx()), float64(bounds.Dy())
	region := image.Rect(
		bounds.Min.X+int(math.Floor(box[0]*x)), bounds.Min.Y+int(math.Floor(box[1]*y)),
		bounds.Min.X+int(math.Ceil(box[2]*x)), bounds.Min.Y+int(math.Ceil(box[3]*y)),
	)
	cropped := image.NewNRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(cropped, cropped.Bounds(), source, region.Min, draw.Src)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, cropped); err != nil {
		return Image{}, err
	}
	if buffer.Len() > MaxInputBytes {
		return Image{}, invalid("裁剪后的图片超过 6 MB，请缩小原图后重试")
	}
	return Image{Data: buffer.Bytes(), MIME: "image/png"}, nil
}
