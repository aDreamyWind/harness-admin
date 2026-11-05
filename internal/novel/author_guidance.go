package novel

import (
	"fmt"
	"strings"
)

const maxAuthorGuidanceRunes = 2000

func applyAuthorGuidance(p *Project, guidance *string) error {
	if guidance == nil {
		return fmt.Errorf("%w，请提交写作约定，清空时请提交空文本", ErrInvalid)
	}
	text := strings.TrimSpace(*guidance)
	if !validOptional(text, maxAuthorGuidanceRunes) {
		return fmt.Errorf("%w，写作约定最多 %d 字", ErrInvalid, maxAuthorGuidanceRunes)
	}
	if p.AuthorGuidance != text {
		p.AuthorGuidance = text
		for i := range p.Chapters {
			if !p.Chapters[i].Final {
				p.Chapters[i].ReviewPassed = false
			}
		}
	}
	return nil
}
