package drama

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type CanvasExtras struct {
	Hidden []string     `json:"hidden"`
	Cards  []CanvasCard `json:"cards"`
	Links  []CanvasLink `json:"links"`
}

type CanvasCard struct {
	ID       string      `json:"id"`
	Kind     string      `json:"kind"`
	Title    string      `json:"title"`
	Content  string      `json:"content"`
	Position CanvasPoint `json:"position"`
}

type CanvasPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type CanvasLink struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	SourceHandle *string `json:"sourceHandle,omitempty"`
	TargetHandle *string `json:"targetHandle,omitempty"`
}

func normalizeCanvasExtras(project string, in CanvasExtras) (CanvasExtras, error) {
	if len(in.Hidden) > 10000 || len(in.Cards) > 500 || len(in.Links) > 2000 {
		return CanvasExtras{}, fmt.Errorf("%w：画布素材节点或连线过多", ErrInvalidInput)
	}
	extras := CanvasExtras{
		Hidden: append([]string{}, in.Hidden...),
		Cards:  append([]CanvasCard{}, in.Cards...),
		Links:  append([]CanvasLink{}, in.Links...),
	}
	nodes := make(map[string]bool, len(extras.Hidden)+len(extras.Cards))
	for _, id := range extras.Hidden {
		if !canonicalCanvasID(id) || nodes[id] {
			return CanvasExtras{}, fmt.Errorf("%w：隐藏的形象编号无效或重复", ErrInvalidInput)
		}
		nodes[id] = true
	}
	var contentBytes int
	for i := range extras.Cards {
		card := &extras.Cards[i]
		card.Title, card.Content = strings.TrimSpace(card.Title), strings.TrimSpace(card.Content)
		contentBytes += len(card.Content)
		if !canonicalCanvasID(card.ID) || nodes[card.ID] || !finiteCanvas(card.Position.X) || !finiteCanvas(card.Position.Y) ||
			!validCanvasText(card.Title, 80) || !validCanvasCardContent(project, *card) || contentBytes > 1024*1024 {
			return CanvasExtras{}, fmt.Errorf("%w：素材节点编号、位置或内容无效", ErrInvalidInput)
		}
		nodes[card.ID] = true
	}
	linkIDs := make(map[string]bool, len(extras.Links))
	endpoints := make(map[[4]string]bool, len(extras.Links))
	for _, link := range extras.Links {
		var from, to string
		if link.SourceHandle != nil {
			from = *link.SourceHandle
		}
		if link.TargetHandle != nil {
			to = *link.TargetHandle
		}
		pair := [4]string{link.Source, link.Target, from, to}
		if !validCanvasText(link.ID, 240) || !canonicalCanvasID(link.Source) || !canonicalCanvasID(link.Target) ||
			link.Source == link.Target || linkIDs[link.ID] || endpoints[pair] ||
			(from != "" && !validCanvasText(from, 80)) || (to != "" && !validCanvasText(to, 80)) {
			return CanvasExtras{}, fmt.Errorf("%w：连线编号或端点无效", ErrInvalidInput)
		}
		linkIDs[link.ID], endpoints[pair] = true, true
	}
	return extras, nil
}

func canonicalCanvasID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && id == parsed.String()
}

func validCanvasText(value string, maxRunes int) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= maxRunes
}

func validCanvasCardContent(project string, card CanvasCard) bool {
	switch card.Kind {
	case "text":
		return validCanvasText(card.Content, 10000)
	case "image", "video", "audio":
		if len(card.Content) > 8192 {
			return false
		}
		if relative, ok := strings.CutPrefix(card.Content, "/api/v1/ai-drama/projects/"+project+"/canvas/media/"); ok {
			id, ok := strings.CutSuffix(relative, "/content")
			return ok && canonicalCanvasID(id)
		}
		parsed, err := url.Parse(card.Content)
		return err == nil && parsed.Hostname() != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
	default:
		return false
	}
}
