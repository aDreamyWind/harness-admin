package novel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

type MemoryFact struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Field   string `json:"field"`
	Value   string `json:"value"`
	Related string `json:"related,omitempty"`
	Quote   string `json:"quote"`
	Chapter int    `json:"chapter"`
}

type MemoryChapter struct {
	Chapter int
	Status  string
	Facts   []MemoryFact
}

type MemoryJob struct {
	ID, ProjectID, ModelCode, Body, LeaseID string
	Owner                                   Owner
	Chapter                                 int
}

type MemoryStore interface {
	ListMemory(context.Context, Owner, string) ([]MemoryChapter, error)
	EnqueueMemory(context.Context, Owner, string) error
	ClaimMemory(context.Context) (MemoryJob, bool, error)
	FinishMemory(context.Context, MemoryJob, []MemoryFact, bool) error
	MemoryDecisions(context.Context, Owner, string) (map[string]int, error)
	SaveMemoryDecision(context.Context, Owner, string, string, int) error
}

type MemoryConflict struct {
	ID      string        `json:"id"`
	Reason  string        `json:"reason"`
	Options [2]MemoryFact `json:"options"`
}

type StoryMemory struct {
	Facts     []MemoryFact     `json:"facts"`
	Conflicts []MemoryConflict `json:"conflicts"`
	Pending   int              `json:"pending"`
	Completed int              `json:"completed"`
	Failed    int              `json:"failed"`
	Missing   int              `json:"missing"`
}

type MemoryResolutionInput struct {
	ConflictID string `json:"conflictId"`
	Choice     *int   `json:"choice"`
}

func (s *Service) WithStoryMemory(store MemoryStore) *Service {
	s.storyMemory = store
	return s
}

func (s *Service) GetStoryMemory(ctx context.Context, owner Owner, id string) (StoryMemory, error) {
	p, err := s.Get(ctx, owner, id)
	if err != nil {
		return StoryMemory{}, err
	}
	return s.loadStoryMemory(ctx, owner, p, len(p.Chapters))
}

func (s *Service) BackfillStoryMemory(ctx context.Context, owner Owner, id string) (StoryMemory, error) {
	p, err := s.Get(ctx, owner, id)
	if err != nil {
		return StoryMemory{}, err
	}
	if s.storyMemory == nil {
		return StoryMemory{}, fmt.Errorf("未配置故事档案存储")
	}
	if err := s.storyMemory.EnqueueMemory(ctx, owner, p.ID); err != nil {
		return StoryMemory{}, fmt.Errorf("补齐故事档案失败: %w", err)
	}
	return s.loadStoryMemory(ctx, owner, p, len(p.Chapters))
}

func (s *Service) ResolveStoryMemory(ctx context.Context, owner Owner, id string, input MemoryResolutionInput) (StoryMemory, error) {
	if len(input.ConflictID) != 64 || input.Choice == nil || *input.Choice < 0 || *input.Choice > 1 {
		return StoryMemory{}, ErrInvalid
	}
	p, err := s.Get(ctx, owner, id)
	if err != nil {
		return StoryMemory{}, err
	}
	memory, err := s.loadStoryMemory(ctx, owner, p, len(p.Chapters))
	if err != nil {
		return StoryMemory{}, err
	}
	for _, conflict := range memory.Conflicts {
		if conflict.ID == input.ConflictID {
			if err := s.storyMemory.SaveMemoryDecision(ctx, owner, p.ID, conflict.ID, *input.Choice); err != nil {
				return StoryMemory{}, fmt.Errorf("保存故事档案取舍失败: %w", err)
			}
			return s.loadStoryMemory(ctx, owner, p, len(p.Chapters))
		}
	}
	if s.storyMemory != nil {
		decisions, err := s.storyMemory.MemoryDecisions(ctx, owner, p.ID)
		if err != nil {
			return StoryMemory{}, fmt.Errorf("读取故事档案取舍失败: %w", err)
		}
		if choice, ok := decisions[input.ConflictID]; ok && choice == *input.Choice {
			return memory, nil
		}
	}
	return StoryMemory{}, fmt.Errorf("%w，待确认的证据已经更新", ErrConflict)
}

func (s *Service) loadStoryMemory(ctx context.Context, owner Owner, p Project, before int) (StoryMemory, error) {
	memory := StoryMemory{Facts: []MemoryFact{}, Conflicts: []MemoryConflict{}}
	if s.storyMemory == nil {
		return memory, nil
	}
	rows, err := s.storyMemory.ListMemory(ctx, owner, p.ID)
	if err != nil {
		return StoryMemory{}, fmt.Errorf("读取故事档案失败: %w", err)
	}
	var facts []MemoryFact
	for _, row := range rows {
		if row.Chapter < 1 || row.Chapter > before || !p.Chapters[row.Chapter-1].Final {
			continue
		}
		switch row.Status {
		case "queued", "running":
			memory.Pending++
		case "failed":
			memory.Failed++
		case "missing":
			memory.Missing++
		case "succeeded":
			memory.Completed++
			for _, fact := range row.Facts {
				if fact.Quote != "" && strings.Contains(p.Chapters[row.Chapter-1].Body, fact.Quote) {
					fact.Chapter = row.Chapter
					facts = append(facts, fact)
				}
			}
		}
	}
	decisions, err := s.storyMemory.MemoryDecisions(ctx, owner, p.ID)
	if err != nil {
		return StoryMemory{}, fmt.Errorf("读取故事档案取舍失败: %w", err)
	}
	return resolveMemoryFacts(memory, facts, decisions), nil
}

func resolveMemoryFacts(memory StoryMemory, facts []MemoryFact, decisions map[string]int) StoryMemory {
	type factGroup struct {
		reason string
		facts  []MemoryFact
	}
	type factSignature struct {
		kind, name, field, value, related string
		chapter                           int
	}
	seen := map[factSignature]bool{}
	groups := map[string]int{}
	var review []factGroup
	for _, fact := range facts {
		signature := factSignature{kind: fact.Kind, name: fact.Name, field: fact.Field, value: fact.Value, related: fact.Related}
		switch fact.Field {
		case "location", "holder", "status", "relationship":
			// 同一人物回到旧地点或物品再次移交也是新事件。
			signature.chapter = fact.Chapter
		}
		if seen[signature] {
			continue
		}
		seen[signature] = true
		var key, reason string
		switch fact.Field {
		case "alias":
			key = fact.Kind + "\x00alias\x00" + strings.ToLower(fact.Value)
			reason = fmt.Sprintf("「%s」被用于不同的%s", fact.Value, memoryKindName(fact.Kind))
		case "birth_name", "birthplace":
			key = fact.Kind + "\x00" + strings.ToLower(fact.Name) + "\x00" + fact.Field
			label := "本名"
			if fact.Field == "birthplace" {
				label = "出生地"
			}
			reason = fmt.Sprintf("「%s」的%s有不同记载", fact.Name, label)
		default:
			memory.Facts = append(memory.Facts, fact)
			continue
		}
		index, found := groups[key]
		if !found {
			index = len(review)
			groups[key] = index
			review = append(review, factGroup{reason: reason})
		}
		review[index].facts = append(review[index].facts, fact)
	}
	for _, group := range review {
		chosen := group.facts[0]
		unresolved := false
		for _, other := range group.facts[1:] {
			if chosen.Name == other.Name && chosen.Value == other.Value {
				continue
			}
			options := [2]MemoryFact{chosen, other}
			id := fmt.Sprintf("%x", sha256.Sum256([]byte(asJSON(options))))
			choice, ok := decisions[id]
			if !ok || choice != 0 && choice != 1 {
				memory.Conflicts = append(memory.Conflicts, MemoryConflict{ID: id, Reason: group.reason, Options: options})
				unresolved = true
				break
			}
			chosen = options[choice]
		}
		if !unresolved {
			memory.Facts = append(memory.Facts, chosen)
		}
	}
	return memory
}

func memoryKindName(kind string) string {
	if kind == "place" {
		return "地点"
	}
	return "人物"
}
