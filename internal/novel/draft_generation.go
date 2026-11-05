package novel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (s *Service) generateChapterDraft(ctx context.Context, owner Owner, p *Project, modelCode, prompt string) (Chapter, error) {
	draft, err := s.generateChapterFields(ctx, owner, p, modelCode, prompt)
	var outputError *DraftOutputError
	if errors.As(err, &outputError) && ctx.Err() == nil {
		retry := fmt.Sprintf("上一轮的正文没有通过保存校验。%s 请返回完整正文和连续性摘要，正文以 body 字符串返回，最多 %d 字。即使只改摘要，也要完整返回正文。\n",
			outputError.Message, maxDraftBodyRunes)
		return s.generateChapterFields(ctx, owner, p, modelCode, retry+prompt)
	}
	return draft, err
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

const draftSummaryPrompt = `只根据已生成的本章正文整理连续性摘要，目标 250 至 350 字，最多 500 字。按【时间】【人物】【线索】记录会影响后续章节的事实，包括时间范围、人物位置和所知信息、道具持有人及未解决的线索。保留明确的时间、名称和数字，不补造事实，不改写正文。只输出 {"summary":"连续性摘要"}。
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
