package drama

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const BatchRuneLimit = 50000
const SourcePageLimit = 12000

var ErrBatchNotReady = errors.New("请先完成剧本概览解析")
var ErrBatchConflict = errors.New("批次已被修改，请刷新后重试")

type Batch struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	Number          int       `json:"number"`
	StartOffset     int       `json:"startOffset"`
	EndOffset       int       `json:"endOffset"`
	SelectionMode   string    `json:"selectionMode"`
	PreviousBatchID string    `json:"previousBatchId,omitempty"`
	Boundary        string    `json:"boundary"`
	Status          string    `json:"status"`
	Revision        int       `json:"revision"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type SourceRange struct {
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	TotalLength int    `json:"totalLength"`
	Text        string `json:"text"`
}

type BatchSelection struct {
	Mode            string `json:"mode"`
	PreviousBatchID string `json:"previousBatchId,omitempty"`
	StartOffset     *int   `json:"startOffset,omitempty"`
	EndOffset       *int   `json:"endOffset,omitempty"`
}

type BatchSuggestion struct {
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	Boundary    string `json:"boundary"`
}

type BatchRangeChange struct {
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
	Revision    int `json:"revision"`
}

var episodeHeading = regexp.MustCompile(`(?i)^\s*(?:第[零〇一二三四五六七八九十百千万两0-9]+集|episode\s*[0-9]+)`)
var chapterHeading = regexp.MustCompile(`(?i)^\s*(?:第[零〇一二三四五六七八九十百千万两0-9]+[章节回幕]|chapter\s*[0-9]+)`)

// FindBatchEnd 只在起点后的最多五万字符内找边界，绝不跨过上限。
func FindBatchEnd(source string, start, total int) BatchSuggestion {
	runes := []rune(source)
	if len(runes) > BatchRuneLimit {
		runes = runes[:BatchRuneLimit]
	}
	limit := start + len(runes)
	if limit >= total {
		return BatchSuggestion{StartOffset: start, EndOffset: total, Boundary: "end"}
	}
	episodeEnd, chapterEnd := 0, 0
	for offset := 0; offset < len(runes); {
		lineEnd := offset
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}
		if offset > 0 && offset < len(runes) {
			line := strings.TrimSpace(string(runes[offset:lineEnd]))
			if episodeHeading.MatchString(line) {
				episodeEnd = offset
			} else if chapterHeading.MatchString(line) {
				chapterEnd = offset
			}
		}
		offset = lineEnd + 1
	}
	best, boundary := 0, "limit"
	if episodeEnd > 0 {
		best, boundary = episodeEnd, "episode"
	} else if chapterEnd > 0 {
		best, boundary = chapterEnd, "chapter"
	}
	if best == 0 {
		for offset := len(runes) - 1; offset > 0; offset-- {
			if runes[offset] == '\n' && (runes[offset-1] == '\n' || (offset > 1 && runes[offset-2] == '\n')) {
				best, boundary = offset+1, "paragraph"
				break
			}
		}
	}
	if best == 0 || best > len(runes) {
		best = len(runes)
	}
	return BatchSuggestion{StartOffset: start, EndOffset: start + best, Boundary: boundary}
}

func (s *Service) SourcePage(ctx context.Context, organizationID, ownerID, projectID string, start, limit int) (SourceRange, error) {
	if _, err := uuid.Parse(projectID); err != nil || start < 0 || limit < 1 || limit > SourcePageLimit {
		return SourceRange{}, ErrInvalidInput
	}
	result, err := s.repository.GetSourceRange(ctx, organizationID, ownerID, projectID, start, limit)
	if err != nil {
		return SourceRange{}, err
	}
	if start >= result.TotalLength {
		return SourceRange{}, ErrInvalidInput
	}
	result.StartOffset = start
	result.EndOffset = start + utf8.RuneCountInString(result.Text)
	return result, nil
}

func (s *Service) ListBatches(ctx context.Context, organizationID, ownerID, projectID string) ([]Batch, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, ErrInvalidInput
	}
	if _, err := s.Get(ctx, organizationID, ownerID, projectID); err != nil {
		return nil, err
	}
	return s.repository.ListBatches(ctx, organizationID, ownerID, projectID)
}

func (s *Service) SuggestBatch(ctx context.Context, organizationID, ownerID, projectID string, input BatchSelection) (BatchSuggestion, error) {
	project, err := s.Get(ctx, organizationID, ownerID, projectID)
	if err != nil {
		return BatchSuggestion{}, err
	}
	if project.Status != "ready" {
		return BatchSuggestion{}, ErrBatchNotReady
	}
	start, err := s.resolveBatchStart(ctx, organizationID, ownerID, projectID, project.SourceLength, input)
	if err != nil {
		return BatchSuggestion{}, err
	}
	limit := min(BatchRuneLimit, project.SourceLength-start)
	source, err := s.repository.GetSourceRange(ctx, organizationID, ownerID, projectID, start, limit)
	if err != nil {
		return BatchSuggestion{}, err
	}
	if utf8.RuneCountInString(source.Text) != limit {
		return BatchSuggestion{}, fmt.Errorf("读取批次原文范围不完整")
	}
	return FindBatchEnd(source.Text, start, project.SourceLength), nil
}

func (s *Service) resolveBatchStart(ctx context.Context, organizationID, ownerID, projectID string, total int, input BatchSelection) (int, error) {
	var start int
	switch input.Mode {
	case "beginning":
		if input.PreviousBatchID != "" || input.StartOffset != nil {
			return 0, ErrInvalidInput
		}
	case "previous":
		if _, err := uuid.Parse(input.PreviousBatchID); err != nil || input.StartOffset != nil {
			return 0, ErrInvalidInput
		}
		previous, err := s.repository.GetBatch(ctx, organizationID, ownerID, projectID, input.PreviousBatchID)
		if err != nil {
			return 0, err
		}
		start = previous.EndOffset
	case "manual":
		if input.StartOffset == nil || input.PreviousBatchID != "" {
			return 0, ErrInvalidInput
		}
		start = *input.StartOffset
	default:
		return 0, ErrInvalidInput
	}
	if start < 0 || start >= total {
		return 0, ErrInvalidInput
	}
	return start, nil
}

func (s *Service) CreateBatch(ctx context.Context, organizationID, ownerID, projectID string, input BatchSelection) (Batch, error) {
	suggestion, err := s.SuggestBatch(ctx, organizationID, ownerID, projectID, input)
	if err != nil {
		return Batch{}, err
	}
	end, boundary := suggestion.EndOffset, suggestion.Boundary
	if input.EndOffset != nil {
		end, boundary = *input.EndOffset, "manual"
	}
	project, err := s.Get(ctx, organizationID, ownerID, projectID)
	if err != nil {
		return Batch{}, err
	}
	if end <= suggestion.StartOffset || end > project.SourceLength || end-suggestion.StartOffset > BatchRuneLimit {
		return Batch{}, ErrInvalidInput
	}
	batch := Batch{ID: uuid.NewString(), ProjectID: projectID, StartOffset: suggestion.StartOffset, EndOffset: end,
		SelectionMode: input.Mode, PreviousBatchID: input.PreviousBatchID, Boundary: boundary, Status: "pending", Revision: 1}
	return s.repository.CreateBatch(ctx, organizationID, ownerID, batch)
}

func (s *Service) UpdateBatchRange(ctx context.Context, organizationID, ownerID, projectID, batchID string, input BatchRangeChange) (Batch, error) {
	if _, err := uuid.Parse(batchID); err != nil || input.Revision < 1 {
		return Batch{}, ErrInvalidInput
	}
	project, err := s.Get(ctx, organizationID, ownerID, projectID)
	if err != nil {
		return Batch{}, err
	}
	if input.StartOffset < 0 || input.EndOffset <= input.StartOffset || input.EndOffset > project.SourceLength || input.EndOffset-input.StartOffset > BatchRuneLimit {
		return Batch{}, ErrInvalidInput
	}
	return s.repository.UpdateBatchRange(ctx, organizationID, ownerID, projectID, batchID, input)
}
