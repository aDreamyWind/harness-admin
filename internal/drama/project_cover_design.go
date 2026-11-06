package drama

import (
	"strings"
	"unicode"
)

type CoverTitleStyle string
type CoverTitlePlacement string

const (
	CoverTitleInk       CoverTitleStyle = "ink"
	CoverTitleMyth      CoverTitleStyle = "myth"
	CoverTitleNoir      CoverTitleStyle = "noir"
	CoverTitleNeon      CoverTitleStyle = "neon"
	CoverTitleEditorial CoverTitleStyle = "editorial"
	CoverTitleAdventure CoverTitleStyle = "adventure"

	CoverTitleTopLeft       CoverTitlePlacement = "top-left"
	CoverTitleTopRight      CoverTitlePlacement = "top-right"
	CoverTitleBottomLeft    CoverTitlePlacement = "bottom-left"
	CoverTitleBottomCenter  CoverTitlePlacement = "bottom-center"
	CoverTitleBottomRight   CoverTitlePlacement = "bottom-right"
	CoverTitleRightVertical CoverTitlePlacement = "right-vertical"
)

type CoverTitleDesign struct {
	Title     string
	Style     CoverTitleStyle
	Placement CoverTitlePlacement
}

func chooseCoverTitleDesign(title, styleID, styleName, genre string, alternate bool) CoverTitleDesign {
	style := coverTitleStyle(styleID, styleName, genre)
	placement := CoverTitleBottomCenter
	switch style {
	case CoverTitleInk:
		placement = CoverTitleTopRight
		if len([]rune(strings.TrimSpace(title))) <= 9 && strings.IndexFunc(strings.TrimSpace(title), func(r rune) bool { return !unicode.Is(unicode.Han, r) }) < 0 {
			placement = CoverTitleRightVertical
		}
	case CoverTitleMyth:
		placement = CoverTitleBottomLeft
		if alternate {
			placement = CoverTitleTopRight
		}
	case CoverTitleNoir:
		placement = CoverTitleBottomRight
		if alternate {
			placement = CoverTitleBottomLeft
		}
	case CoverTitleNeon:
		placement = CoverTitleTopLeft
		if alternate {
			placement = CoverTitleBottomRight
		}
	case CoverTitleEditorial:
		placement = CoverTitleTopLeft
		if alternate {
			placement = CoverTitleTopRight
		}
	default:
		if alternate {
			placement = CoverTitleBottomLeft
		}
	}
	return CoverTitleDesign{Title: title, Style: style, Placement: placement}
}

func coverTitleStyle(styleID, styleName, genre string) CoverTitleStyle {
	style := strings.ToLower(styleID + " " + styleName)
	switch {
	case hasCoverKeyword(style, "ink-wash", "monochrome-ink", "水墨"):
		return CoverTitleInk
	case hasCoverKeyword(style, "chinese-mythology", "shadow-puppet", "shanghai-animation", "中国神话", "皮影", "上美"):
		return CoverTitleMyth
	case hasCoverKeyword(style, "dark-", "monochrome-manga", "黑暗", "暗黑", "黑白漫画", "黑白二维漫画"):
		return CoverTitleNoir
	case hasCoverKeyword(style, "cyber", "otomo", "vaporwave", "赛博", "蒸汽波", "大友"):
		return CoverTitleNeon
	case hasCoverKeyword(style, "ghibli", "minimal-illustration", "japanese-flat", "children-crayon", "治愈", "简洁", "平涂", "蜡笔"):
		return CoverTitleEditorial
	}
	switch {
	case hasCoverKeyword(genre, "悬疑", "惊悚", "犯罪", "推理"):
		return CoverTitleNoir
	case hasCoverKeyword(genre, "科幻", "赛博", "太空", "机甲"):
		return CoverTitleNeon
	case hasCoverKeyword(genre, "神话", "武侠", "仙侠", "修仙"):
		return CoverTitleMyth
	case hasCoverKeyword(genre, "日常", "治愈", "校园", "青春"):
		return CoverTitleEditorial
	default:
		return CoverTitleAdventure
	}
}

func hasCoverKeyword(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func (design CoverTitleDesign) reserveSpace() string {
	var region string
	switch design.Placement {
	case CoverTitleRightVertical:
		return "画面右侧约四分之一留出偏浅、干净的竖向留白，主角和关键景物放在中左部。"
	case CoverTitleTopLeft:
		region = "上方约三分之一、重点偏左"
	case CoverTitleTopRight:
		region = "上方约三分之一、重点偏右"
	case CoverTitleBottomLeft:
		region = "下方约三分之一、重点偏左"
	case CoverTitleBottomRight:
		region = "下方约三分之一、重点偏右"
	default:
		region = "下方约三分之一、重点居中"
	}
	tone := "暗调、低细节"
	if design.Style == CoverTitleInk || design.Style == CoverTitleEditorial {
		tone = "偏浅、低细节"
	}
	return "画面" + region + "保留" + tone + "的区域供后期排版，避免遮挡人物的脸。"
}
