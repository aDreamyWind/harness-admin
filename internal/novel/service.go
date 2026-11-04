package novel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxInitialChapters = 30
	MaxChapters        = 500
	MaxAppendChapters  = 30
	untitledNovel      = "未命名小说"
)

var (
	ErrInvalid    = errors.New("小说参数无效")
	ErrNotFound   = errors.New("小说作品不存在")
	ErrConflict   = errors.New("作品已被更新，请重新加载后重试")
	ErrState      = errors.New("当前创作阶段不支持该操作")
	ErrModel      = errors.New("请选择当前账号可用的文本模型")
	ErrGeneration = errors.New("模型生成失败，请重试或更换文本模型")
)

type Owner struct {
	OrganizationID string
	UserID         string
}

type Model struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Book struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	ChapterCount int       `json:"chapterCount"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Cover        *Cover    `json:"cover,omitempty"`
}

type Project struct {
	Book
	Brief   string `json:"brief"`
	Version int    `json:"version"`
	Workspace
}

type Workspace struct {
	ModelCode  string    `json:"modelCode"`
	Outline    *Outline  `json:"outline"`
	Confirmed  bool      `json:"confirmed"`
	Chapters   []Chapter `json:"chapters"`
	NeedsTitle bool      `json:"needsTitle,omitempty"`
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
	Body    string `json:"body"`
	Summary string `json:"summary"`
	Review  string `json:"review"`
	Final   bool   `json:"final"`
}

type CreateInput struct {
	Title        string `json:"title"`
	Brief        string `json:"brief"`
	ChapterCount int    `json:"chapterCount"`
	SuggestTitle bool   `json:"suggestTitle"`
}

type RefineIdeaInput struct {
	Brief       string `json:"brief"`
	Instruction string `json:"instruction"`
	ModelCode   string `json:"modelCode"`
}

type RefineIdeaResult struct {
	Brief string `json:"brief"`
}

type Action struct {
	Kind        string   `json:"kind"`
	Version     int      `json:"version"`
	Chapter     int      `json:"chapter"`
	ModelCode   string   `json:"modelCode"`
	Outline     *Outline `json:"outline"`
	Beats       []Beat   `json:"beats"`
	Body        string   `json:"body"`
	Summary     string   `json:"summary"`
	Instruction string   `json:"instruction"`
}

type Repository interface {
	List(context.Context, Owner) ([]Book, error)
	Create(context.Context, Owner, Project) (Project, error)
	Get(context.Context, Owner, string) (Project, error)
	Save(context.Context, Owner, Project) (Project, error)
	GetCover(context.Context, Owner, string, string) (CoverImage, error)
	SaveCover(context.Context, Owner, string, string, Cover, CoverImage) (Cover, error)
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

func (s *Service) RefineIdea(ctx context.Context, owner Owner, input RefineIdeaInput) (RefineIdeaResult, error) {
	input.Brief = strings.TrimSpace(input.Brief)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if !valid(input.Brief, 4000) || !validOptional(input.Instruction, 1200) {
		return RefineIdeaResult{}, fmt.Errorf("%w，请输入有效的故事灵感，优化要求最多 1200 字", ErrInvalid)
	}
	prompt := refineIdeaPrompt + asJSON(struct{ Brief, Instruction string }{input.Brief, input.Instruction})
	var result RefineIdeaResult
	if err := s.completeJSON(ctx, owner, input.ModelCode, prompt, &result); err != nil {
		return RefineIdeaResult{}, err
	}
	result.Brief = strings.TrimSpace(result.Brief)
	if !valid(result.Brief, 4000) {
		return RefineIdeaResult{}, ErrGeneration
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, owner Owner, id string) (Project, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Project{}, ErrNotFound
	}
	return s.repo.Get(ctx, owner, id)
}

func (s *Service) Create(ctx context.Context, owner Owner, input CreateInput) (Project, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Brief = strings.TrimSpace(input.Brief)
	needsTitle := input.SuggestTitle && input.Title == ""
	if needsTitle {
		input.Title = untitledNovel
	}
	if !valid(input.Title, 120) || !valid(input.Brief, 4000) || input.ChapterCount < 1 || input.ChapterCount > MaxInitialChapters {
		return Project{}, ErrInvalid
	}
	return s.repo.Create(ctx, owner, Project{
		Book:  Book{ID: uuid.NewString(), Title: input.Title, ChapterCount: input.ChapterCount},
		Brief: input.Brief, Version: 1, Workspace: Workspace{Chapters: make([]Chapter, input.ChapterCount), NeedsTitle: needsTitle},
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
	case "generate_outline", "save_outline", "confirm_outline":
		err = s.applyOutline(ctx, owner, &p, action)
	case "append_chapters":
		err = appendChapters(&p, action.Beats)
	case "write", "save_draft", "review", "finalize":
		err = s.applyChapter(ctx, owner, &p, action)
	default:
		err = ErrInvalid
	}
	if err != nil {
		return Project{}, err
	}
	return s.repo.Save(ctx, owner, p)
}

func (s *Service) applyOutline(ctx context.Context, owner Owner, p *Project, action Action) error {
	if p.Confirmed {
		return fmt.Errorf("%w，作品大纲已确认", ErrState)
	}
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
		if !normalizeOutline(&outline, p.ChapterCount) {
			return ErrGeneration
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
		p.Outline = &outline
	case "save_outline", "confirm_outline":
		if !normalizeOutline(action.Outline, p.ChapterCount) {
			return fmt.Errorf("%w，请填写梗概、设定、人物及每章标题和目标", ErrInvalid)
		}
		p.Outline = action.Outline
		p.Confirmed = action.Kind == "confirm_outline"
	}
	return nil
}

func (s *Service) applyChapter(ctx context.Context, owner Owner, p *Project, action Action) error {
	if action.Chapter < 1 || action.Chapter > len(p.Chapters) {
		return ErrInvalid
	}
	index := action.Chapter - 1
	if !p.Confirmed || p.Chapters[index].Final || (index > 0 && !p.Chapters[index-1].Final) {
		return fmt.Errorf("%w，请先确认大纲并定稿前一章，已定稿章节只读", ErrState)
	}
	chapter := &p.Chapters[index]
	switch action.Kind {
	case "write":
		action.Instruction = strings.TrimSpace(action.Instruction)
		if !validOptional(action.Instruction, 1200) {
			return ErrInvalid
		}
		prompt := writePrompt + chapterContext(*p, index, action.Instruction, chapter.Review)
		var result Chapter
		if err := s.generate(ctx, owner, p, action.ModelCode, prompt, &result); err != nil {
			return err
		}
		if !validDraft(&result, true) {
			return ErrGeneration
		}
		*chapter = Chapter{Body: result.Body, Summary: result.Summary}
	case "save_draft", "finalize":
		result := Chapter{Body: action.Body, Summary: action.Summary, Final: action.Kind == "finalize"}
		if !validDraft(&result, result.Final) {
			return fmt.Errorf("%w，请填写正文，定稿时需同时填写连续性摘要", ErrInvalid)
		}
		if result.Body == chapter.Body && result.Summary == chapter.Summary {
			result.Review = chapter.Review
		}
		*chapter = result
	case "review":
		if chapter.Body == "" {
			return fmt.Errorf("%w，请先保存本章草稿", ErrState)
		}
		prompt := reviewPrompt + chapterContext(*p, index, "", "")
		var result json.RawMessage
		if err := s.generate(ctx, owner, p, action.ModelCode, prompt, &result); err != nil {
			return err
		}
		review, err := parseReview(result)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrGeneration, err)
		}
		chapter.Review = review
	}
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
	return valid(chapter.Body, 8000) && validOptional(chapter.Summary, 500) && (!requireSummary || chapter.Summary != "")
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
