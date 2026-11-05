package novel

import (
	"fmt"
	"strings"
)

type FinalizedChapter struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

func (p Project) finalizedCount() int {
	for i, chapter := range p.Chapters {
		if !chapter.Final {
			return i
		}
	}
	return len(p.Chapters)
}

func (p Project) FinalizedChapters() []FinalizedChapter {
	chapters := make([]FinalizedChapter, p.finalizedCount())
	for i := range chapters {
		chapters[i] = FinalizedChapter{Number: i + 1, Title: p.chapterTitle(i)}
	}
	return chapters
}

func (p Project) ChapterSnapshot(start, end int) (string, error) {
	if start < 1 || end < start || end > p.ChapterCount || end > len(p.Chapters) {
		return "", fmt.Errorf("%w，请选择有效的连续章节范围", ErrInvalid)
	}
	if end > p.finalizedCount() {
		return "", fmt.Errorf("%w，所选章节需要全部定稿后才能解析", ErrState)
	}
	var source strings.Builder
	for i := start - 1; i < end; i++ {
		body := p.Chapters[i].Body
		if strings.TrimSpace(body) == "" {
			return "", fmt.Errorf("%w，第 %d 章没有可解析的定稿正文", ErrState, i+1)
		}
		if i > start-1 {
			source.WriteString("\n\n")
		}
		fmt.Fprintf(&source, "第%d章", i+1)
		if title := strings.Join(strings.Fields(p.chapterTitle(i)), " "); title != "" {
			source.WriteByte(' ')
			source.WriteString(title)
		}
		source.WriteString("\n\n")
		source.WriteString(body)
	}
	return source.String(), nil
}

func (p Project) chapterTitle(index int) string {
	if p.Outline == nil || index >= len(p.Outline.Beats) {
		return ""
	}
	return p.Outline.Beats[index].Title
}
