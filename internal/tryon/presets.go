package tryon

import (
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed presets/*.jpg presets/*.png
var presetFiles embed.FS

type preset struct {
	file     string
	kind     string
	name     string
	category string
}

var presets = []preset{
	{file: "person-illustration.jpg", kind: Person2D, name: "短发眼镜模特"},
	{file: "person-studio.jpg", kind: PersonReal, name: "短发写实模特"},
	{file: "garment-blazer.png", kind: Garment, name: "焦糖色翻领外套", category: "外套"},
	{file: "garment-skirt.png", kind: Garment, name: "莓紫色 A 字半裙", category: "半裙"},
	{file: "garment-tee.png", kind: Garment, name: "灰蓝圆领上衣", category: "上衣"},
	{file: "pose-front.png", kind: Pose, name: "自然站立"},
	{file: "pose-hand-on-hip.png", kind: Pose, name: "单手叉腰"},
	{file: "pose-step.png", kind: Pose, name: "向前迈步"},
}

func (p preset) asset(owner Owner) Asset {
	key := "harness-admin/tryon/preset/" + owner.OrganizationID + "/" + owner.UserID + "/" + p.file
	return Asset{
		ID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String(),
		Kind: p.kind, Name: p.name, Category: p.category, Builtin: true,
		CreatedAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
	}
}

func (p preset) image() (Image, error) {
	data, err := presetFiles.ReadFile("presets/" + p.file)
	if err != nil {
		return Image{}, fmt.Errorf("读取内置试衣素材失败: %w", err)
	}
	mime := "image/png"
	if strings.HasSuffix(p.file, ".jpg") {
		mime = "image/jpeg"
	}
	return Image{Data: data, MIME: mime}, nil
}
