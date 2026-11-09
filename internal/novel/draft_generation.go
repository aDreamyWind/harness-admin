package novel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"
)

const (
	maxDraftBodyRunes    = 8000
	maxDraftSummaryRunes = 500
)

type DraftOutputError struct{ Message string }

func (e *DraftOutputError) Error() string { return e.Message }
func (e *DraftOutputError) Unwrap() error { return ErrGeneration }

func (s *Service) writeChapterDraft(ctx context.Context, owner Owner, p *Project, index int, modelCode, instruction, review string) (Chapter, error) {
	memory := s.memoryForChapter(ctx, owner, *p, index, instruction)
	prompt := memory.guidance() + authorContinuityGuidance + writePrompt + chapterContext(*p, index, instruction, review, memory)
	if p.StoryMode == StoryModeSerial {
		prompt = serialWriteGuidance + prompt
	}
	result, err := s.generateChapterDraft(ctx, owner, p, modelCode, prompt)
	result.AuthorInstruction = instruction
	return result, err
}

func (s *Service) finishChapterDraft(ctx context.Context, owner Owner, p *Project, index int, modelCode string, previous *Chapter, feedback map[int]string) {
	chapter := &p.Chapters[index]
	var err error
	if chapter.Summary == "" {
		chapter.Summary, err = s.generateDraftSummary(ctx, owner, p, modelCode, chapter.Body)
	}
	s.reviewAfterDraftChange(ctx, owner, p, index, modelCode, previous, err, feedback)
}

func (s *Service) generateChapterDraft(ctx context.Context, owner Owner, p *Project, modelCode, prompt string) (Chapter, error) {
	requirement := fmt.Sprintf("请返回完整正文和连续性摘要，正文以 body 字符串返回，最多 %d 字。", maxDraftBodyRunes)
	return retryChapterCandidate(ctx, p.ID, prompt, requirement, func(prompt string) (Chapter, error) {
		return s.generateChapterFields(ctx, owner, p, modelCode, prompt)
	})
}

func retryChapterCandidate(ctx context.Context, projectID, prompt, requirement string, generate func(string) (Chapter, error)) (Chapter, error) {
	draft, err := generate(prompt)
	if err == nil || ctx.Err() != nil {
		return draft, err
	}
	var outputError *DraftOutputError
	var formatError *ModelOutputFormatError
	var feedback string
	switch {
	case errors.As(err, &outputError):
		feedback = outputError.Message
	case errors.As(err, &formatError):
		feedback = "请返回一个完整的 JSON 对象，闭合字符串和对象，字符串内的换行及引号需要转义。"
	default:
		return draft, err
	}
	slog.WarnContext(ctx, "小说正文结果未通过校验，将补做一次", "project_id", projectID, "error", err)
	return generate("上一轮的正文没有通过保存校验。" + feedback + " " + requirement + "\n" + prompt)
}

func (s *Service) generateChapterFields(ctx context.Context, owner Owner, p *Project, modelCode, prompt string) (Chapter, error) {
	var response struct {
		Body    json.RawMessage `json:"body"`
		Summary json.RawMessage `json:"summary"`
	}
	if err := s.generate(ctx, owner, p, modelCode, prompt, &response); err != nil {
		return Chapter{}, err
	}
	var body string
	if err := json.Unmarshal(response.Body, &body); err != nil {
		return Chapter{}, &DraftOutputError{Message: "模型没有按要求返回完整的正文。"}
	}
	body = strings.TrimSpace(body)
	if body == "" || strings.ContainsRune(body, 0) {
		return Chapter{}, &DraftOutputError{Message: "模型返回的正文为空或含无效字符。"}
	}
	switch strings.TrimRight(body, "。！") {
	case "同上", "正文同上", "正文不变", "原文不变", "保持原文":
		return Chapter{}, &DraftOutputError{Message: "模型只返回了正文占位语，没有返回完整正文。"}
	}
	if count := utf8.RuneCountInString(body); count > maxDraftBodyRunes {
		return Chapter{}, &DraftOutputError{Message: fmt.Sprintf("模型返回的正文有 %d 字，超过 %d 字上限。", count, maxDraftBodyRunes)}
	}
	// 摘要不合规时，保留已经校验过的完整正文，单独补写摘要。
	summary, _ := normalizeTextParts(response.Summary, maxDraftSummaryRunes)
	return Chapter{Body: body, Summary: summary}, nil
}

const draftSummaryPrompt = `只根据已生成的本章正文整理连续性摘要，目标 250 至 350 字，最多 500 字。按【时间】【人物】【线索】记录会影响后续章节的事实，包括时间范围、人物位置和所知信息、道具持有人及未解决的线索。说明关键资源的用途、是否已经到位和措施是否实际起效。保留明确的时间、名称和数字，不补造事实，不改写正文。只输出 {"summary":"连续性摘要"}。
本章正文：
`

func (s *Service) generateDraftSummary(ctx context.Context, owner Owner, p *Project, modelCode, body string) (string, error) {
	var response struct {
		Summary json.RawMessage `json:"summary"`
	}
	material := asJSON(struct{ Body string }{Body: body})
	if err := s.generate(ctx, owner, p, modelCode, draftSummaryPrompt+material, &response); err != nil {
		return "", err
	}
	summary, err := normalizeTextParts(response.Summary, maxDraftSummaryRunes)
	if err != nil {
		return "", fmt.Errorf("%w，连续性摘要缺失、格式错误或超过 %d 字: %w", ErrGeneration, maxDraftSummaryRunes, err)
	}
	return summary, nil
}
