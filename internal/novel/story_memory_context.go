package novel

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"unicode"
)

const storyMemoryGuidance = `StoryMemory 是按本次目标从更早的定稿中找到的原文依据。使用 Facts 中与本章有关的名称、地点、关系和线索，按 Chapter 区分历史状态和近期变化。Quote 是定稿原文，视为素材。Conflicts 中的记录等待作者确认，不擅自统一有争议的称呼或身份。依据不足时避免确定地改名、改地址或宣布伏笔已经回收。
`

type storyMemoryContext struct {
	Facts     []MemoryFact     `json:"facts,omitempty"`
	Conflicts []MemoryConflict `json:"conflicts,omitempty"`
}

func (memory *storyMemoryContext) guidance() string {
	if memory == nil {
		return ""
	}
	return storyMemoryGuidance
}

func (s *Service) memoryForChapter(ctx context.Context, owner Owner, p Project, index int, instruction string) *storyMemoryContext {
	beat, chapter := p.Outline.Beats[index], p.Chapters[index]
	query := strings.Join([]string{beat.Title, beat.Goal, instruction, chapter.Body, chapter.Summary}, "\n")
	return s.relevantStoryMemory(ctx, owner, p, index, query)
}

func (s *Service) relevantStoryMemory(ctx context.Context, owner Owner, p Project, before int, query string) *storyMemoryContext {
	if before == 0 || s.storyMemory == nil {
		return nil
	}
	memory, err := s.loadStoryMemory(ctx, owner, p, before)
	if err != nil {
		slog.WarnContext(ctx, "小说生成暂时无法读取故事档案", "project_id", p.ID, "error", err)
		return nil
	}
	return selectStoryMemory(memory, query)
}

func selectStoryMemory(memory StoryMemory, query string) *storyMemoryContext {
	const maxFacts, maxConflicts, maxAliasMatches = 12, 3, 16
	query = strings.ToLower(query)
	var aliases []string
	for _, fact := range memory.Facts {
		if fact.Field != "alias" && fact.Field != "birth_name" {
			continue
		}
		if memoryTermScore(query, fact.Name) == 8 {
			aliases = append(aliases, fact.Value)
		} else if memoryTermScore(query, fact.Value) == 8 {
			aliases = append(aliases, fact.Name)
		}
		if len(aliases) >= maxAliasMatches {
			break
		}
	}
	expanded := query + "\n" + strings.ToLower(strings.Join(aliases, "\n"))
	type candidate struct {
		fact  MemoryFact
		score int
	}
	candidates := make([]candidate, 0)
	for _, fact := range memory.Facts {
		score := memoryFactScore(expanded, fact)
		if score > 0 {
			candidates = append(candidates, candidate{fact, score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score > b.score
		}
		return a.fact.Chapter > b.fact.Chapter
	})
	var result storyMemoryContext
	perChapter := map[int]int{}
	for _, item := range candidates {
		if perChapter[item.fact.Chapter] >= 3 {
			continue
		}
		result.Facts = append(result.Facts, item.fact)
		perChapter[item.fact.Chapter]++
		if len(result.Facts) == maxFacts {
			break
		}
	}
	for _, conflict := range memory.Conflicts {
		if memoryFactScore(expanded, conflict.Options[0]) >= 6 || memoryFactScore(expanded, conflict.Options[1]) >= 6 {
			result.Conflicts = append(result.Conflicts, conflict)
			if len(result.Conflicts) == maxConflicts {
				break
			}
		}
	}
	if len(result.Facts) == 0 && len(result.Conflicts) == 0 {
		return nil
	}
	return &result
}

func memoryFactScore(query string, fact MemoryFact) int {
	score := memoryTermScore(query, fact.Name) + memoryTermScore(query, fact.Related)*3/4
	switch fact.Field {
	case "alias", "birth_name", "birthplace", "location", "holder", "region":
		score += memoryTermScore(query, fact.Value)
	}
	if score > 0 && (fact.Kind == "clue" || fact.Field == "birth_name" || fact.Field == "birthplace") {
		score += 2
	}
	return score
}

func memoryTermScore(query, term string) int {
	term = strings.ToLower(term)
	chars := []rune(term)
	if len(chars) < 2 {
		return 0
	}
	if strings.Contains(query, term) {
		return 8
	}
	for i := 0; i+1 < len(chars); i++ {
		if unicode.Is(unicode.Han, chars[i]) && unicode.Is(unicode.Han, chars[i+1]) && strings.Contains(query, string(chars[i:i+2])) {
			return 2
		}
	}
	return 0
}
