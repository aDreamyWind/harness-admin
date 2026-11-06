package app

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"strings"
	"unicode"

	"harness-admin/internal/drama"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	dramaCoverWidth  = 900
	dramaCoverHeight = 1350
)

func composeDramaCover(data []byte, design drama.CoverTitleDesign) ([]byte, string, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("读取漫剧封面底图失败: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || config.Width*config.Height > 16*1024*1024 {
		return nil, "", fmt.Errorf("漫剧封面底图尺寸超出支持范围")
	}
	background, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("解码漫剧封面底图失败: %w", err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, dramaCoverWidth, dramaCoverHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{R: 15, G: 19, B: 29, A: 255}), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), background, dramaCoverCrop(background.Bounds()), draw.Over, nil)
	var backdrop bytes.Buffer
	if err := jpeg.Encode(&backdrop, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return nil, "", fmt.Errorf("压缩漫剧封面底图失败: %w", err)
	}
	if strings.TrimSpace(design.Title) == "" {
		return nil, "", fmt.Errorf("漫剧封面需要剧名")
	}
	// 底图只嵌入已解码、重编码的 JPEG，所有剧名都作为 XML 文本转义。
	var cover bytes.Buffer
	fmt.Fprintf(&cover, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="cover-title"><title id="cover-title">`, dramaCoverWidth, dramaCoverHeight, dramaCoverWidth, dramaCoverHeight)
	if err := xml.EscapeText(&cover, []byte(design.Title)); err != nil {
		return nil, "", err
	}
	fmt.Fprintf(&cover, `</title><image width="%d" height="%d" xlink:href="data:image/jpeg;base64,%s"/>`, dramaCoverWidth, dramaCoverHeight, base64.StdEncoding.EncodeToString(backdrop.Bytes()))
	if err := drawDramaCoverTitle(&cover, design); err != nil {
		return nil, "", err
	}
	cover.WriteString(`</svg>`)
	return cover.Bytes(), "image/svg+xml", nil
}

type dramaCoverTitleTreatment struct {
	font, fill, stroke, shade, accent string
	weight                            int
}

func dramaCoverTreatment(style drama.CoverTitleStyle) dramaCoverTitleTreatment {
	const serif = "'Noto Serif CJK SC','Songti SC',SimSun,serif"
	const sans = "'Noto Sans CJK SC','PingFang SC','Microsoft YaHei',sans-serif"
	switch style {
	case drama.CoverTitleInk:
		return dramaCoverTitleTreatment{"KaiTi,STKaiti," + serif, "#292823", "#fff8e8", "#f7eedb", "#9b3b34", 700}
	case drama.CoverTitleMyth:
		return dramaCoverTitleTreatment{serif, "#ffde9e", "#4a251c", "#251720", "#d69443", 800}
	case drama.CoverTitleNoir:
		return dramaCoverTitleTreatment{sans, "#f8f7f2", "#080b12", "#11131d", "#df6458", 900}
	case drama.CoverTitleNeon:
		return dramaCoverTitleTreatment{sans, "#ccfff7", "#413278", "#161538", "#fa8bef", 800}
	case drama.CoverTitleEditorial:
		return dramaCoverTitleTreatment{sans, "#233757", "#f8fcff", "#eaf4fb", "#4f76a2", 650}
	default:
		return dramaCoverTitleTreatment{serif, "#fff1d5", "#182638", "#152238", "#92bccc", 800}
	}
}

func drawDramaCoverTitle(cover *bytes.Buffer, design drama.CoverTitleDesign) error {
	treatment := dramaCoverTreatment(design.Style)
	lines, size := dramaCoverTitleLayout(design.Title)
	x, anchor, lineHeight := dramaCoverWidth/2, "middle", size*6/5
	firstY := 1235 - (len(lines)-1)*lineHeight
	shadeX, shadeY, shadeWidth, shadeHeight := 0, 830, dramaCoverWidth, 520
	gradient := `x1="0" x2="0" y1="100%" y2="0"`
	switch design.Placement {
	case drama.CoverTitleTopLeft, drama.CoverTitleTopRight:
		shadeY = 0
		shadeHeight = 490
		gradient = `x1="0" x2="0" y1="0" y2="100%"`
		firstY = 90 + size
		if design.Placement == drama.CoverTitleTopLeft {
			x, anchor = 82, "start"
		} else {
			x, anchor = dramaCoverWidth-82, "end"
		}
	case drama.CoverTitleBottomLeft:
		x, anchor = 82, "start"
	case drama.CoverTitleBottomRight:
		x, anchor = dramaCoverWidth-82, "end"
	case drama.CoverTitleRightVertical:
		shadeX, shadeY, shadeWidth, shadeHeight = 606, 0, 294, dramaCoverHeight
		gradient = `x1="100%" x2="0" y1="0" y2="0"`
		lines = strings.Split(strings.TrimSpace(design.Title), "")
		x, size, lineHeight, firstY = 779, 95, 118, 205
	}
	fmt.Fprintf(cover, `<defs><linearGradient id="title-shade" %s><stop offset="0" stop-color="%s" stop-opacity=".94"/><stop offset=".58" stop-color="%s" stop-opacity=".77"/><stop offset="1" stop-color="%s" stop-opacity="0"/></linearGradient></defs><rect x="%d" y="%d" width="%d" height="%d" fill="url(#title-shade)"/>`,
		gradient, treatment.shade, treatment.shade, treatment.shade, shadeX, shadeY, shadeWidth, shadeHeight)
	drawDramaCoverAccent(cover, design.Placement, treatment.accent, x, firstY, size)
	fmt.Fprintf(cover, `<g font-family="%s" font-weight="%d" font-size="%d" text-anchor="%s" fill="%s" stroke="%s" stroke-width="%.1f" stroke-linejoin="round" paint-order="stroke fill">`,
		treatment.font, treatment.weight, size, anchor, treatment.fill, treatment.stroke, float64(size)/18)
	for i, line := range lines {
		fmt.Fprintf(cover, `<text x="%d" y="%d">`, x, firstY+i*lineHeight)
		if err := xml.EscapeText(cover, []byte(line)); err != nil {
			return err
		}
		cover.WriteString(`</text>`)
	}
	cover.WriteString(`</g>`)
	return nil
}

func drawDramaCoverAccent(cover *bytes.Buffer, placement drama.CoverTitlePlacement, fill string, x, firstY, size int) {
	width, height := 48, 4
	y := firstY - size - 21
	switch placement {
	case drama.CoverTitleTopRight, drama.CoverTitleBottomRight:
		x -= width
	case drama.CoverTitleRightVertical:
		x, y, width, height = x+59, firstY-size, 4, 54
	case drama.CoverTitleTopLeft, drama.CoverTitleBottomLeft:
	default:
		x -= width / 2
	}
	fmt.Fprintf(cover, `<rect x="%d" y="%d" width="%d" height="%d" fill="%s" rx="2"/>`, x, y, width, height, fill)
}

func dramaCoverCrop(source image.Rectangle) image.Rectangle {
	width, height := source.Dx(), source.Dy()
	if width*dramaCoverHeight > height*dramaCoverWidth {
		cropped := max(1, height*dramaCoverWidth/dramaCoverHeight)
		source.Min.X += (width - cropped) / 2
		source.Max.X = source.Min.X + cropped
	} else {
		cropped := max(1, width*dramaCoverHeight/dramaCoverWidth)
		source.Min.Y += (height - cropped) / 2
		source.Max.Y = source.Min.Y + cropped
	}
	return source
}

func dramaCoverTitleLayout(title string) ([]string, int) {
	remaining := []rune(strings.Join(strings.Fields(title), " "))
	if len(remaining) == 0 {
		return nil, 0
	}
	lineCount := min(4, (len(remaining)+8)/9)
	lines := make([]string, 0, lineCount)
	longest := 1
	for lineCount > 0 && len(remaining) > 0 {
		end := (len(remaining) + lineCount - 1) / lineCount
		for boundary := end; boundary > max(1, end-3) && boundary < len(remaining); boundary-- {
			if unicode.IsSpace(remaining[boundary]) || strings.ContainsRune("，。！？、：；·-—", remaining[boundary-1]) {
				end = boundary
				break
			}
		}
		line := strings.TrimSpace(string(remaining[:end]))
		if line != "" {
			lines = append(lines, line)
			longest = max(longest, len([]rune(line)))
		}
		remaining = remaining[end:]
		lineCount--
	}
	return lines, min(112, 752/longest)
}
