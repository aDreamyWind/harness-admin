package novel

import (
	"context"
	"fmt"
	"strings"
)

type ChapterPlanInput struct {
	Version     int    `json:"version"`
	Count       int    `json:"count"`
	ModelCode   string `json:"modelCode"`
	Instruction string `json:"instruction"`
}

type ChapterPlan struct {
	Beats []Beat `json:"beats"`
}

const appendChapterPrompt = `为这部中文小说追加恰好 %d 章的蓝图，从第 %d 章开始。现有章节均已列入作品计划，新增章节应从最后一个已规划章节的章末继续。已定稿摘要是已经发生的事实，其余旧章目标是待写计划，区分两者。尊重已确定的人物、世界规则、时间线、人物所知及关键线索，不回改原计划。如果上一阶段已经收尾，从其结果自然引出后续矛盾，不推翻已经解决的事件。优先遵循作者给出的续写方向，在新章节中明确行动的必要条件、转折和承接任务。
只输出 JSON 对象，beats 是按新增章节顺序排列的数组，每项含简体中文字符串 title（最多 120 字）与 goal（最多 1200 字）。仅返回新增章节，不在标题中添加章号。
作品资料：
`

func (s *Service) PlanChapters(ctx context.Context, owner Owner, id string, input ChapterPlanInput) (ChapterPlan, error) {
	if input.Version < 1 || !validOptional(strings.TrimSpace(input.Instruction), 1200) {
		return ChapterPlan{}, ErrInvalid
	}
	p, err := s.Get(ctx, owner, id)
	if err != nil {
		return ChapterPlan{}, err
	}
	if p.Version != input.Version {
		return ChapterPlan{}, ErrConflict
	}
	if err := validateAppend(&p, input.Count); err != nil {
		return ChapterPlan{}, err
	}
	var result ChapterPlan
	if err := s.completeJSON(ctx, owner, input.ModelCode, chapterPlanPrompt(p, input), &result); err != nil {
		return ChapterPlan{}, err
	}
	if len(result.Beats) != input.Count || !normalizeBeats(result.Beats) {
		return ChapterPlan{}, ErrGeneration
	}
	return result, nil
}

func appendChapters(p *Project, beats []Beat) error {
	if err := validateAppend(p, len(beats)); err != nil {
		return err
	}
	if !normalizeBeats(beats) {
		return fmt.Errorf("%w，请填写每个新增章节的标题和目标", ErrInvalid)
	}
	outline := *p.Outline
	outline.Beats = append(append([]Beat(nil), p.Outline.Beats...), beats...)
	p.Outline = &outline
	p.Chapters = append(p.Chapters, make([]Chapter, len(beats))...)
	p.ChapterCount += len(beats)
	return nil
}

func validateAppend(p *Project, count int) error {
	if !p.Confirmed || p.Outline == nil {
		return fmt.Errorf("%w，请先确认作品大纲", ErrState)
	}
	if p.ChapterCount != len(p.Outline.Beats) || p.ChapterCount != len(p.Chapters) {
		return fmt.Errorf("%w，现有章节与大纲不一致", ErrState)
	}
	if count < 1 || count > MaxAppendChapters || count > MaxChapters-p.ChapterCount {
		return fmt.Errorf("%w，每次追加 1 至 %d 章，作品最多 %d 章", ErrInvalid, MaxAppendChapters, MaxChapters)
	}
	return nil
}

func chapterPlanPrompt(p Project, input ChapterPlanInput) string {
	const recentPlans = 10
	start := max(0, p.ChapterCount-recentPlans)
	material := struct {
		Title, Brief, Synopsis, World, Characters string
		Instruction                               string
		RecentPlanStart                           int
		RecentPlans                               []Beat
		FinalizedChapters                         []priorChapter
	}{
		Title: p.Title, Brief: p.Brief, Synopsis: p.Outline.Synopsis, World: p.Outline.World, Characters: p.Outline.Characters,
		Instruction: strings.TrimSpace(input.Instruction), RecentPlanStart: start + 1,
		RecentPlans: p.Outline.Beats[start:], FinalizedChapters: priorChapters(p, p.ChapterCount, false),
	}
	return fmt.Sprintf(appendChapterPrompt, input.Count, p.ChapterCount+1) + asJSON(material)
}
