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
	{file: "person-anime-woman.jpg", kind: Person2D, name: "紫发·动漫女角色"},
	{file: "person-studio-woman.jpg", kind: PersonReal, name: "深色长发·写实女模特"},
	{file: "person-anime-man-dark.jpg", kind: Person2D, name: "动漫男·黑发和风"},
	{file: "person-studio-man-wave.jpg", kind: PersonReal, name: "写实男·棕色卷发"},
	{file: "person-anime-woman-teal.jpg", kind: Person2D, name: "动漫女·青绿短发"},
	{file: "person-studio-woman-bob.jpg", kind: PersonReal, name: "写实女·复古金发短卷"},
	{file: "person-anime-man.jpg", kind: Person2D, name: "银发·动漫男角色"},
	{file: "person-studio-man.jpg", kind: PersonReal, name: "黑色短发·写实男模特"},
	{file: "person-anime-woman-red.jpg", kind: Person2D, name: "动漫女·赤发马尾"},
	{file: "person-studio-woman-curls.jpg", kind: PersonReal, name: "写实女·自然黑色卷发"},
	{file: "person-anime-man-sun.jpg", kind: Person2D, name: "动漫男·金发学院风"},
	{file: "person-studio-man-crop.jpg", kind: PersonReal, name: "写实男·利落短寸"},
	{file: "garment-blazer-photo.jpg", kind: Garment, name: "焦糖色翻领外套", category: "外套"},
	{file: "garment-skirt-photo.jpg", kind: Garment, name: "莓紫色 A 字半裙", category: "半裙"},
	{file: "garment-tee-photo.jpg", kind: Garment, name: "灰蓝圆领上衣", category: "上衣"},
	{file: "pose-front.png", kind: Pose, name: "自然站立"},
	{file: "pose-hand-on-hip.png", kind: Pose, name: "单手叉腰"},
	{file: "pose-step.png", kind: Pose, name: "向前迈步"},
}

// 旧版样例不再作为新选择，已保存的图片仍可随试穿记录查看。
var retiredPresetFiles = []string{
	"person-illustration.jpg", "person-studio.jpg",
	"garment-blazer.png", "garment-skirt.png", "garment-tee.png",
}

func presetID(owner Owner, file string) string {
	key := "harness-admin/tryon/preset/" + owner.OrganizationID + "/" + owner.UserID + "/" + file
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String()
}

func (p preset) asset(owner Owner) Asset {
	return Asset{
		ID:   presetID(owner, p.file),
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
