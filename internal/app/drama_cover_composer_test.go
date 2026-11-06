package app

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"harness-admin/internal/drama"
)

func TestComposeDramaCoverEmbedsPortraitAndEscapesProjectTitle(t *testing.T) {
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 18, 12))); err != nil {
		t.Fatal(err)
	}
	const title = `道爷要飞升 <script> & 剑影江湖`
	data, mime, err := composeDramaCover(input.Bytes(), drama.CoverTitleDesign{Title: title, Style: drama.CoverTitleMyth, Placement: drama.CoverTitleBottomLeft})
	if err != nil || mime != "image/svg+xml" {
		t.Fatalf("封面合成失败，类型 %s，错误 %v", mime, err)
	}
	var cover struct {
		Title string   `xml:"title"`
		Lines []string `xml:"g>text"`
		Image struct {
			Href string `xml:"href,attr"`
		} `xml:"image"`
	}
	if err := xml.Unmarshal(data, &cover); err != nil {
		t.Fatal(err)
	}
	if cover.Title != title || len(cover.Lines) < 2 || strings.Contains(string(data), "<script>") {
		t.Fatalf("剧名没有正确排版或转义：%q，%v", cover.Title, cover.Lines)
	}
	if strings.ReplaceAll(strings.Join(cover.Lines, ""), " ", "") != strings.ReplaceAll(title, " ", "") {
		t.Fatalf("海报上的剧名缺少文字：%v", cover.Lines)
	}
	const jpegPrefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(cover.Image.Href, jpegPrefix) {
		t.Fatal("封面未嵌入 JPEG 底图")
	}
	imageBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(cover.Image.Href, jpegPrefix))
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil || config.Width != 900 || config.Height != 1350 {
		t.Fatalf("嵌入图片不符合竖版海报尺寸：%+v，%v", config, err)
	}
}

func TestDramaCoverTitleTreatmentsChangeTypesettingAndKeepTitle(t *testing.T) {
	const title = "风雪城门"
	tests := []struct {
		style     drama.CoverTitleStyle
		placement drama.CoverTitlePlacement
		anchor    string
		fill      string
		lines     int
	}{
		{drama.CoverTitleInk, drama.CoverTitleRightVertical, "middle", "#292823", 4},
		{drama.CoverTitleMyth, drama.CoverTitleBottomLeft, "start", "#ffde9e", 1},
		{drama.CoverTitleNoir, drama.CoverTitleBottomRight, "end", "#f8f7f2", 1},
		{drama.CoverTitleNeon, drama.CoverTitleTopLeft, "start", "#ccfff7", 1},
		{drama.CoverTitleEditorial, drama.CoverTitleTopRight, "end", "#233757", 1},
		{drama.CoverTitleAdventure, drama.CoverTitleBottomCenter, "middle", "#fff1d5", 1},
	}
	for _, tt := range tests {
		t.Run(string(tt.style), func(t *testing.T) {
			var body bytes.Buffer
			body.WriteString("<svg>")
			if err := drawDramaCoverTitle(&body, drama.CoverTitleDesign{Title: title, Style: tt.style, Placement: tt.placement}); err != nil {
				t.Fatal(err)
			}
			body.WriteString("</svg>")
			var markup struct {
				Title struct {
					Anchor string `xml:"text-anchor,attr"`
					Fill   string `xml:"fill,attr"`
					Lines  []struct {
						Text string `xml:",chardata"`
						Y    int    `xml:"y,attr"`
					} `xml:"text"`
				} `xml:"g"`
			}
			if err := xml.Unmarshal(body.Bytes(), &markup); err != nil {
				t.Fatal(err)
			}
			got := markup.Title
			if got.Anchor != tt.anchor || got.Fill != tt.fill || len(got.Lines) != tt.lines {
				t.Fatalf("标题样式不符：%+v", got)
			}
			var renderedTitle strings.Builder
			for _, line := range got.Lines {
				if line.Y < 0 || line.Y > dramaCoverHeight {
					t.Fatalf("标题超出封面：%+v", line)
				}
				renderedTitle.WriteString(line.Text)
			}
			if renderedTitle.String() != title {
				t.Fatalf("标题文本不符：%q", renderedTitle.String())
			}
		})
	}
}
