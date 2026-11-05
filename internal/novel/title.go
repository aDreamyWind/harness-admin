package novel

import (
	"context"
	"fmt"
	"strings"
)

const suggestTitlePrompt = `根据作品资料拟定一个贴合故事的简体中文小说书名。参考故事灵感和已定大纲，突出核心人物、冲突或独特设定，换一个与现名不同的书名。书名最多 120 字，不要添加书名号或解释。
只输出 JSON 对象，字段 title 为书名字符串。
作品资料：
`

func (s *Service) applyTitle(ctx context.Context, owner Owner, p *Project, action Action) (bool, error) {
	title := strings.TrimSpace(action.Title)
	if action.Kind == "suggest_title" {
		material := struct {
			CurrentTitle string `json:"currentTitle"`
			Brief        string `json:"brief"`
			Synopsis     string `json:"synopsis,omitempty"`
		}{CurrentTitle: p.Title, Brief: p.Brief}
		if p.Outline != nil {
			material.Synopsis = p.Outline.Synopsis
		}
		var result struct {
			Title string `json:"title"`
		}
		if err := s.completeJSON(ctx, owner, action.ModelCode, suggestTitlePrompt+asJSON(material), &result); err != nil {
			return false, err
		}
		title = strings.TrimSpace(result.Title)
		if title == untitledNovel || !valid(title, 120) {
			return false, ErrGeneration
		}
	} else if !valid(title, 120) {
		return false, fmt.Errorf("%w，书名须填写 1 至 120 字", ErrInvalid)
	}

	if title == p.Title && !p.NeedsTitle {
		return false, nil
	}
	p.Title = title
	p.NeedsTitle = false
	return true, nil
}
