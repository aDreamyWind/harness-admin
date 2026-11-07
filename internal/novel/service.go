package novel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxInitialChapters       = 30
	MaxChapters              = 500
	MaxAppendChapters        = 30
	StoryModeSerial          = "serial"
	minSerialOpeningChapters = 3
	maxSerialOpeningChapters = 8
	untitledNovel            = "未命名小说"
)

var (
	ErrInvalid    = errors.New("小说参数无效")
	ErrNotFound   = errors.New("小说作品不存在")
	ErrConflict   = errors.New("作品已被更新，请重新加载后重试")
	ErrState      = errors.New("当前创作阶段不支持该操作")
	ErrModel      = errors.New("请选择当前账号可用的文本模型")
	ErrGeneration = errors.New("模型生成失败，请重试或更换文本模型")
)

type SerialOpeningCountError struct {
	InitialCount int
	ActualCount  int
	TargetCount  int
}

func (e *SerialOpeningCountError) Error() string {
	return fmt.Sprintf("模型首次生成 %d 章，调整后返回 %d 章，预期 %d 章。请重试或更换文本模型", e.InitialCount, e.ActualCount, e.TargetCount)
}

func (e *SerialOpeningCountError) Unwrap() error { return ErrGeneration }

type Owner struct {
	OrganizationID string
	UserID         string
}

type Model struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Book struct {
	ID                    string    `json:"id"`
	Title                 string    `json:"title"`
	ChapterCount          int       `json:"chapterCount"`
	FinalizedChapterCount int       `json:"finalizedChapterCount"`
	OpeningPending        bool      `json:"openingPending,omitempty"`
	UpdatedAt             time.Time `json:"updatedAt"`
	Cover                 *Cover    `json:"cover,omitempty"`
}

type Project struct {
	Book
	Brief   string `json:"brief"`
	Version int    `json:"version"`
	Workspace
}

type Workspace struct {
	AuthorGuidance string        `json:"authorGuidance,omitempty"`
	StoryMode      string        `json:"storyMode,omitempty"`
	ModelCode      string        `json:"modelCode"`
	Outline        *Outline      `json:"outline"`
	OutlineCheck   *OutlineCheck `json:"outlineCheck,omitempty"`
	Confirmed      bool          `json:"confirmed"`
	Chapters       []Chapter     `json:"chapters"`
	NeedsTitle     bool          `json:"needsTitle,omitempty"`
}

type Outline struct {
	Synopsis   string `json:"synopsis"`
	World      string `json:"world"`
	Characters string `json:"characters"`
	Beats      []Beat `json:"beats"`
}

type Beat struct {
	Title string `json:"title"`
	Goal  string `json:"goal"`
}

type Chapter struct {
	AuthorInstruction string `json:"authorInstruction,omitempty"`
	Body              string `json:"body"`
	Summary           string `json:"summary"`
	Review            string `json:"review"`
	ReviewFailed      bool   `json:"reviewFailed,omitempty"`
	ReviewPassed      bool   `json:"reviewPassed,omitempty"`
	Final             bool   `json:"final"`
}

type CreateInput struct {
	Title        string `json:"title"`
	Brief        string `json:"brief"`
	ChapterCount int    `json:"chapterCount"`
	SuggestTitle bool   `json:"suggestTitle"`
	StoryMode    string `json:"storyMode,omitempty"`
}

type RefineIdeaInput struct {
	Brief               string   `json:"brief"`
	Instruction         string   `json:"instruction"`
	ModelCode           string   `json:"modelCode"`
	PreviousSuggestions []string `json:"previousSuggestions,omitempty"`
}

type RefineIdeaResult struct {
	Brief           string `json:"brief"`
	ProtagonistName string `json:"protagonistName,omitempty"`
	SystemName      string `json:"systemName,omitempty"`
}

type Action struct {
	AuthorGuidance *string  `json:"authorGuidance,omitempty"`
	Kind           string   `json:"kind"`
	Version        int      `json:"version"`
	Title          string   `json:"title"`
	Chapter        int      `json:"chapter"`
	ModelCode      string   `json:"modelCode"`
	Outline        *Outline `json:"outline"`
	Beats          []Beat   `json:"beats"`
	Body           string   `json:"body"`
	Summary        string   `json:"summary"`
	Instruction    string   `json:"instruction"`
}

type Repository interface {
	List(context.Context, Owner) ([]Book, error)
	Create(context.Context, Owner, Project) (Project, error)
	Get(context.Context, Owner, string) (Project, error)
	Save(context.Context, Owner, Project) (Project, error)
	GetCoverRevision(context.Context, Owner, string) (string, error)
	GetCover(context.Context, Owner, string, string) (CoverImage, error)
	SaveCover(context.Context, Owner, string, string, Cover, CoverImage) (Cover, error)
	SaveCoverVersion(context.Context, Owner, string, Cover, CoverImage) error
	ListCoverHistory(context.Context, Owner, string) ([]CoverHistoryItem, error)
	SelectCoverVersion(context.Context, Owner, string, string) (Cover, error)
}

type Writer interface {
	Models(context.Context, Owner) ([]Model, error)
	Complete(context.Context, Owner, string, string) (string, error)
}

type Service struct {
	repo           Repository
	writer         Writer
	coverGenerator CoverGenerator
	coverPreviews  CoverPreviewStore
	coverStyles    CoverStyleResolver
	coverJobs      CoverGenerationStore
}

func NewService(repo Repository, writer Writer, coverGenerator CoverGenerator, coverPreviews CoverPreviewStore) *Service {
	return &Service{repo: repo, writer: writer, coverGenerator: coverGenerator, coverPreviews: coverPreviews}
}

func (s *Service) List(ctx context.Context, owner Owner) ([]Book, error) {
	return s.repo.List(ctx, owner)
}

func (s *Service) Models(ctx context.Context, owner Owner) ([]Model, error) {
	return s.writer.Models(ctx, owner)
}

func (s *Service) Get(ctx context.Context, owner Owner, id string) (Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Project{}, ErrNotFound
	}
	p, err := s.repo.Get(ctx, owner, id)
	if err != nil {
		return Project{}, err
	}
	p.FinalizedChapterCount = p.finalizedCount()
	p.OpeningPending = p.StoryMode == StoryModeSerial && p.Outline == nil
	return p, nil
}

func (s *Service) Create(ctx context.Context, owner Owner, input CreateInput) (Project, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Brief = strings.TrimSpace(input.Brief)
	if input.StoryMode != "" && input.StoryMode != StoryModeSerial {
		return Project{}, ErrInvalid
	}
	serial := input.StoryMode == StoryModeSerial
	if serial && input.ChapterCount == 0 {
		// 项目在大纲生成前需要满足章节数的存储约束，AI 生成后再替换这个占位值。
		input.ChapterCount = minSerialOpeningChapters
	}
	needsTitle := (input.SuggestTitle || serial) && input.Title == ""
	if needsTitle {
		input.Title = untitledNovel
	}
	if !valid(input.Title, 120) || !valid(input.Brief, 4000) || input.ChapterCount < 1 || input.ChapterCount > MaxInitialChapters {
		return Project{}, ErrInvalid
	}
	return s.repo.Create(ctx, owner, Project{
		Book:  Book{ID: uuid.NewString(), Title: input.Title, ChapterCount: input.ChapterCount, OpeningPending: serial},
		Brief: input.Brief, Version: 1,
		Workspace: Workspace{StoryMode: input.StoryMode, Chapters: make([]Chapter, input.ChapterCount), NeedsTitle: needsTitle},
	})
}

func (s *Service) Apply(ctx context.Context, owner Owner, id string, action Action) (Project, error) {
	if action.Version < 1 {
		return Project{}, ErrInvalid
	}
	p, err := s.Get(ctx, owner, id)
	if err != nil {
		return Project{}, err
	}
	if p.Version != action.Version {
		return Project{}, ErrConflict
	}
	switch action.Kind {
	case "rename", "suggest_title":
		var changed bool
		changed, err = s.applyTitle(ctx, owner, &p, action)
		if err == nil && !changed {
			// 拟题期间作品仍可能在其他页面更新。同名结果也不能返回过期版本。
			current, getErr := s.Get(ctx, owner, id)
			if getErr != nil {
				return Project{}, getErr
			}
			if current.Version != action.Version {
				return Project{}, ErrConflict
			}
			return current, nil
		}
	case "generate_outline", "check_outline", "save_outline", "confirm_outline", "reopen_outline":
		err = s.applyOutline(ctx, owner, &p, action)
	case "save_author_guidance":
		err = applyAuthorGuidance(&p, action.AuthorGuidance)
	case "append_chapters":
		err = appendChapters(&p, action.Beats)
	case "write", "revise_review", "save_draft", "review", "finalize", "reopen_chapter":
		err = s.applyChapter(ctx, owner, &p, action)
	default:
		err = ErrInvalid
	}
	if err != nil {
		return Project{}, err
	}
	p.FinalizedChapterCount = p.finalizedCount()
	p.OpeningPending = p.StoryMode == StoryModeSerial && p.Outline == nil
	return s.repo.Save(ctx, owner, p)
}

func (s *Service) applyOutline(ctx context.Context, owner Owner, p *Project, action Action) error {
	if action.Kind == "reopen_outline" {
		if !p.Confirmed || p.Outline == nil {
			return fmt.Errorf("%w，当前大纲可以直接修改", ErrState)
		}
		p.Confirmed = false
		return nil
	}
	if p.Confirmed {
		return fmt.Errorf("%w，请先撤回大纲确认", ErrState)
	}
	previousOutline := p.Outline
	switch action.Kind {
	case "generate_outline":
		prompt, err := outlineGenerationPrompt(p, action)
		if err != nil {
			return err
		}
		var result struct {
			Title      json.RawMessage `json:"title"`
			Synopsis   string          `json:"synopsis"`
			World      string          `json:"world"`
			Characters json.RawMessage `json:"characters"`
			Beats      []Beat          `json:"beats"`
		}
		if err := s.generate(ctx, owner, p, action.ModelCode, prompt, &result); err != nil {
			return err
		}
		characters, err := normalizeTextParts(result.Characters, 4000)
		if err != nil {
			return fmt.Errorf("%w，主要人物格式无效: %w", ErrGeneration, err)
		}
		outline := Outline{Synopsis: result.Synopsis, World: result.World, Characters: characters, Beats: result.Beats}
		count := p.ChapterCount
		if p.StoryMode == StoryModeSerial && p.Outline == nil {
			count = len(outline.Beats)
			if count < minSerialOpeningChapters || count > maxSerialOpeningChapters {
				target := min(max(count, minSerialOpeningChapters), maxSerialOpeningChapters)
				var repaired struct {
					Beats []Beat `json:"beats"`
				}
				if err := s.generate(ctx, owner, p, action.ModelCode, serialOpeningRepairPrompt(p.Brief, outline, target), &repaired); err != nil {
					return fmt.Errorf("模型首次给出了 %d 章，调整为 %d 章失败: %w", count, target, err)
				}
				if actual := len(repaired.Beats); actual != target {
					return &SerialOpeningCountError{InitialCount: count, ActualCount: actual, TargetCount: target}
				}
				outline.Beats = repaired.Beats
				count = target
			}
		}
		if !normalizeOutline(&outline, count) {
			return ErrGeneration
		}
		if count != p.ChapterCount {
			p.ChapterCount = count
			p.Chapters = make([]Chapter, count)
		}
		if p.NeedsTitle {
			var title string
			// 书名是可选的生成结果，模型未返回有效书名时保留当前名称。
			err := json.Unmarshal(result.Title, &title)
			title = strings.TrimSpace(title)
			if err == nil && title != untitledNovel && valid(title, 120) {
				p.Title = title
				p.NeedsTitle = false
			}
		}
		p.OutlineCheck = s.checkOutline(ctx, owner, p, action.ModelCode, action.Instruction, &outline)
		p.Outline = &outline
	case "check_outline":
		modelCode := strings.TrimSpace(action.ModelCode)
		if !valid(modelCode, 160) {
			return ErrModel
		}
		source := action.Outline
		if source == nil {
			source = p.Outline
		}
		if source == nil {
			return fmt.Errorf("%w，请先准备完整大纲，再检查剧情因果", ErrInvalid)
		}
		outline := *source
		outline.Beats = slices.Clone(source.Beats)
		if !normalizeOutline(&outline, p.ChapterCount) {
			return fmt.Errorf("%w，请先准备完整大纲，再检查剧情因果", ErrInvalid)
		}
		p.OutlineCheck = s.checkOutline(ctx, owner, p, modelCode, "", &outline)
		p.Outline = &outline
	case "save_outline", "confirm_outline":
		if !normalizeOutline(action.Outline, p.ChapterCount) {
			return fmt.Errorf("%w，请填写梗概、设定、人物及每章标题和目标", ErrInvalid)
		}
		if !equalOutlines(p.Outline, action.Outline) {
			p.OutlineCheck = nil
		}
		p.Outline = action.Outline
		p.Confirmed = action.Kind == "confirm_outline"
	}
	if !equalOutlines(previousOutline, p.Outline) {
		// 草稿审阅依赖本章目标和全书设定，大纲改变后应基于新大纲复核。
		for i := range p.Chapters {
			if !p.Chapters[i].Final {
				clearChapterReview(&p.Chapters[i])
			}
		}
	}
	return nil
}

func equalOutlines(a, b *Outline) bool {
	return a != nil && b != nil && a.Synopsis == b.Synopsis && a.World == b.World &&
		a.Characters == b.Characters && slices.Equal(a.Beats, b.Beats)
}

func (s *Service) applyChapter(ctx context.Context, owner Owner, p *Project, action Action) error {
	if action.Chapter < 1 || action.Chapter > len(p.Chapters) {
		return ErrInvalid
	}
	index := action.Chapter - 1
	if !p.Confirmed || p.Outline == nil || index >= len(p.Outline.Beats) {
		return fmt.Errorf("%w，请先确认作品大纲", ErrState)
	}
	if index > p.finalizedCount() {
		return fmt.Errorf("%w，请先定稿前文", ErrState)
	}
	chapter := &p.Chapters[index]
	if action.Kind == "reopen_chapter" {
		if !chapter.Final {
			return fmt.Errorf("%w，本章可以直接修改", ErrState)
		}
		chapter.Final = false
		// 后续正文和摘要留作草稿，在新的前文基础上重新确认。
		for i := index + 1; i < len(p.Chapters); i++ {
			p.Chapters[i].Final = false
			clearChapterReview(&p.Chapters[i])
		}
		return nil
	}
	if chapter.Final {
		return fmt.Errorf("%w，请先撤回本章定稿", ErrState)
	}
	switch action.Kind {
	case "write", "revise_review":
		instruction := strings.TrimSpace(action.Instruction)
		if !validOptional(instruction, 1200) {
			return ErrInvalid
		}
		review := chapter.Review
		var previous *Chapter
		if action.Kind == "revise_review" {
			if chapter.Body == "" || chapter.Review == "" || chapter.ReviewFailed || chapter.ReviewPassed {
				return fmt.Errorf("%w，请先获取本章有效的审稿意见", ErrState)
			}
			saved := *chapter
			previous = &saved
			instruction = saved.AuthorInstruction
			review = reviewText(reviewIssuesToVerify(saved.Review))
			if review == "" {
				chapter.Review, chapter.ReviewPassed = "", true
				return nil
			}
		}
		prompt := authorContinuityGuidance + writePrompt + chapterContext(*p, index, instruction, review)
		if previous != nil {
			prompt = reviseFromReviewPrompt + prompt
		}
		if p.StoryMode == StoryModeSerial {
			prompt = serialWriteGuidance + prompt
		}
		result, err := s.generateChapterDraft(ctx, owner, p, action.ModelCode, prompt)
		if err != nil {
			return err
		}
		if result.Summary == "" {
			result.Summary, err = s.generateDraftSummary(ctx, owner, p, action.ModelCode, result.Body)
		}
		*chapter = Chapter{Body: result.Body, Summary: result.Summary, AuthorInstruction: instruction}
		s.reviewAfterDraftChange(ctx, owner, p, index, action.ModelCode, previous, err)
	case "save_draft", "finalize":
		result := Chapter{Body: action.Body, Summary: action.Summary, Final: action.Kind == "finalize", AuthorInstruction: chapter.AuthorInstruction}
		if !validDraft(&result, result.Final) {
			return fmt.Errorf("%w，请填写正文，定稿时需同时填写连续性摘要", ErrInvalid)
		}
		if result.Body == chapter.Body && result.Summary == chapter.Summary {
			result.Review = chapter.Review
			result.ReviewFailed = chapter.ReviewFailed && !result.Final
			result.ReviewPassed = chapter.ReviewPassed
		}
		*chapter = result
	case "review":
		if chapter.Body == "" {
			return fmt.Errorf("%w，请先保存本章草稿", ErrState)
		}
		var previous *Chapter
		if chapter.ReviewFailed && chapter.Review != "" {
			pending := *chapter
			previous = &pending
		}
		if chapter.Summary == "" {
			summary, err := s.generateDraftSummary(ctx, owner, p, action.ModelCode, chapter.Body)
			if err != nil {
				return fmt.Errorf("%w: %w", &DraftOutputError{Message: "模型未能补全连续性摘要，已保存的正文仍在。"}, err)
			}
			chapter.Summary = summary
			// 重试时原始改稿前的正文已不可用，补齐摘要后只复核已有意见。
			if previous != nil {
				previous.Summary = summary
			}
			s.reviewAfterDraftChange(ctx, owner, p, index, action.ModelCode, previous, nil)
			return nil
		}
		return s.reviewChapter(ctx, owner, p, index, action.ModelCode, previous)
	}
	return nil
}

func clearChapterReview(chapter *Chapter) {
	chapter.Review = ""
	chapter.ReviewFailed = false
	chapter.ReviewPassed = false
}

func (s *Service) reviewAfterDraftChange(ctx context.Context, owner Owner, p *Project, index int, modelCode string, previous *Chapter, summaryErr error) {
	err := summaryErr
	if err == nil {
		err = s.reviewChapter(ctx, owner, p, index, modelCode, previous)
	}
	if err != nil {
		chapter := &p.Chapters[index]
		chapter.ReviewPassed, chapter.ReviewFailed = false, true
		if previous != nil {
			chapter.Review = previous.Review
		} else {
			chapter.Review = ""
		}
		slog.WarnContext(ctx, "AI 小说审稿流程未完成", "project_id", p.ID, "chapter", index+1, "error", err)
	}
}

func (s *Service) reviewChapter(ctx context.Context, owner Owner, p *Project, index int, modelCode string, previous *Chapter) error {
	chapter := &p.Chapters[index]
	var review string
	var err error
	if previous != nil {
		review, err = s.verifyChapterReview(ctx, owner, p, index, modelCode, *previous)
	} else {
		prompt := reviewPrompt + chapterContext(*p, index, chapter.AuthorInstruction, "")
		var result json.RawMessage
		if err := s.generate(ctx, owner, p, modelCode, prompt, &result); err != nil {
			return err
		}
		review, err = parseReview(result)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGeneration, err)
	}
	chapter.Review = review
	chapter.ReviewFailed = false
	chapter.ReviewPassed = review == ""
	return nil
}

func (s *Service) generate(ctx context.Context, owner Owner, p *Project, code, prompt string, output any) error {
	code = strings.TrimSpace(code)
	if err := s.completeJSON(ctx, owner, code, prompt, output); err != nil {
		return err
	}
	p.ModelCode = code
	return nil
}

func (s *Service) completeJSON(ctx context.Context, owner Owner, code, prompt string, output any) error {
	code = strings.TrimSpace(code)
	if !valid(code, 160) {
		return ErrModel
	}
	raw, err := s.writer.Complete(ctx, owner, code, prompt)
	if err != nil {
		if errors.Is(err, ErrModel) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrGeneration, err)
	}
	if err := decodeModelOutput(raw, output); err != nil {
		return fmt.Errorf("%w: %w", ErrGeneration, err)
	}
	return nil
}

func normalizeOutline(outline *Outline, count int) bool {
	if outline == nil || len(outline.Beats) != count {
		return false
	}
	outline.Synopsis = strings.TrimSpace(outline.Synopsis)
	outline.World = strings.TrimSpace(outline.World)
	outline.Characters = strings.TrimSpace(outline.Characters)
	if !valid(outline.Synopsis, 4000) || !valid(outline.World, 4000) || !valid(outline.Characters, 4000) {
		return false
	}
	return normalizeBeats(outline.Beats)
}

func normalizeBeats(beats []Beat) bool {
	for i := range beats {
		beat := &beats[i]
		beat.Title = strings.TrimSpace(beat.Title)
		beat.Goal = strings.TrimSpace(beat.Goal)
		if !valid(beat.Title, 120) || !valid(beat.Goal, 1200) {
			return false
		}
	}
	return true
}

func validDraft(chapter *Chapter, requireSummary bool) bool {
	chapter.Body = strings.TrimSpace(chapter.Body)
	chapter.Summary = strings.TrimSpace(chapter.Summary)
	return valid(chapter.Body, maxDraftBodyRunes) && validOptional(chapter.Summary, maxDraftSummaryRunes) && (!requireSummary || chapter.Summary != "")
}

func valid(value string, limit int) bool {
	return value != "" && validOptional(value, limit)
}

func validOptional(value string, limit int) bool {
	return !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}

func asJSON(value any) string {
	// 提示词只使用固定结构的字符串和整数，可直接编码。
	data, _ := json.Marshal(value)
	return string(data)
}
