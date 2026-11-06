package drama

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrStyleUnavailable = errors.New("所选风格不存在或已停用，请从风格库重新选择")

type StyleOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	PreviewURL string `json:"previewUrl"`
	PreviewX   int    `json:"previewX"`
	PreviewY   int    `json:"previewY"`
}

// 私有模板即使被意外编码也不会暴露；公开接口仅返回 StyleOption。
type VisualStyle struct {
	StyleOption
	ImagePrompt string `json:"-"`
	VideoPrompt string `json:"-"`
}
type StyleRepository interface {
	ListStyles(context.Context) ([]StyleOption, error)
	ResolveStyle(context.Context, string) (VisualStyle, error)
	ResolveCatalogStyle(context.Context, string) (VisualStyle, error)
}

func (s *Service) WithStyles(repo StyleRepository) *Service { s.styles = repo; return s }
func (s *Service) Styles(ctx context.Context) ([]StyleOption, error) {
	if s.styles == nil {
		return []StyleOption{}, nil
	}
	return s.styles.ListStyles(ctx)
}
func (s *Service) resolveStyle(ctx context.Context, id, name string) (StyleOption, error) {
	if s.styles == nil {
		return StyleOption{ID: id, Name: name}, nil
	} // 旧业务测试及未启用视觉模块的组合。
	key := strings.TrimSpace(id)
	if key == "" {
		key = strings.TrimSpace(name)
		if alias, ok := map[string]string{"国风": "国风水墨", "水墨": "国风水墨", "二维动漫": "日系动漫"}[key]; ok {
			key = alias
		}
	}
	if key == "" {
		return StyleOption{}, ErrStyleUnavailable
	}
	style, err := s.styles.ResolveStyle(ctx, key)
	return style.StyleOption, err
}
func BuildAssetImagePrompt(form AssetForm, description string, style VisualStyle, hasReference bool) string {
	composition := map[string]string{"character": "单个人物，全身形象，清晰完整，简洁背景，无文字无水印。", "scene": "完整场景环境概念图，空间关系清晰，无文字无水印。", "prop": "单个道具，完整物体，简洁背景，无文字无水印。"}[form.Kind]
	prompt := fmt.Sprintf("%s\n资产：%s\n形象：%s\n描述：%s\n视觉风格：%s\n风格要求：%s", composition, form.AssetName, form.Name, description, style.Name, style.ImagePrompt)
	if form.Gender != "unspecified" && form.Kind == "character" {
		prompt += "\n性别：" + map[string]string{"male": "男", "female": "女", "other": "其他"}[form.Gender]
	}
	if hasReference {
		prompt += "\n参考原始形象保持身份一致，仅按照当前形象描述调整状态、服装或外貌。"
	}
	return prompt
}
