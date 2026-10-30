package drama

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 兼容模型把整数编号写为纯数字字符串；拒绝小数、负数、空值和任意文本。
func (u *evidenceUnit) UnmarshalJSON(raw []byte) error {
	var v struct {
		ID    json.RawMessage `json:"id"`
		Event string          `json:"event"`
		Facts []SourceFact    `json:"facts"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("单元结构错误：%w", err)
	}
	id := strings.TrimSpace(string(v.ID))
	if len(id) > 1 && id[0] == '"' {
		var text string
		if err := json.Unmarshal(v.ID, &text); err != nil {
			return err
		}
		id = text
	}
	if id == "" {
		return errors.New("编号格式错误：缺少单元编号")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("编号格式错误：%s 应为正整数", id)
		}
	}
	number, err := strconv.Atoi(id)
	if err != nil || number < 1 {
		return fmt.Errorf("编号格式错误：%s 应为正整数", id)
	}
	u.ID, u.Event, u.Facts = number, v.Event, v.Facts
	return nil
}

// 只统一换行和行首缩进，不删除标点、否定词、数字或正文内部空格。
// starts/ends 将匹配后的字符位置映射回未修改的上传原文。
func normalizedQuote(text string) (string, []int, []int) {
	src := []rune(text)
	out := []rune{}
	starts, ends := []int{}, []int{}
	lineStart := true
	for i := 0; i < len(src); {
		r, start, end := src[i], i, i+1
		if r == '\r' {
			r = '\n'
			if i+1 < len(src) && src[i+1] == '\n' {
				end++
			}
		}
		i = end
		if lineStart && (r == ' ' || r == '\t' || r == '\u3000') {
			continue
		}
		out = append(out, r)
		starts = append(starts, start)
		ends = append(ends, end)
		lineStart = r == '\n'
	}
	return string(out), starts, ends
}
func locateQuote(original, quote string) (int, int, bool) {
	if strings.TrimSpace(quote) == "" {
		return 0, 0, false
	}
	if index := strings.Index(original, quote); index >= 0 {
		start := utf8.RuneCountInString(original[:index])
		return start, start + utf8.RuneCountInString(quote), true
	}
	source, starts, ends := normalizedQuote(original)
	needle, _, _ := normalizedQuote(strings.TrimSpace(quote))
	if needle == "" {
		return 0, 0, false
	}
	index := strings.Index(source, needle)
	if index < 0 {
		return 0, 0, false
	}
	start := utf8.RuneCountInString(source[:index])
	end := start + utf8.RuneCountInString(needle) - 1
	return starts[start], ends[end], true
}
func quoteExists(original, quote string) bool { _, _, ok := locateQuote(original, quote); return ok }

func unitReferences(unit sourceUnit, base int) []SourceReference {
	text := []rune(unit.Text)
	refs := []SourceReference{}
	appendPart := func(start, end int) {
		for start < end && unicode.IsSpace(text[start]) {
			start++
		}
		for end > start && unicode.IsSpace(text[end-1]) {
			end--
		}
		if start == end {
			return
		}
		refs = append(refs, SourceReference{ID: fmt.Sprintf("U%dS%d", unit.ID, len(refs)+1), Quote: string(text[start:end]), StartOffset: base + unit.Start + start, EndOffset: base + unit.Start + end})
	}
	start := 0
	for i := 0; i < len(text); i++ {
		r := text[i]
		end := i + 1
		if strings.ContainsRune("。！？!?", r) {
			for end < len(text) && strings.ContainsRune("\"'”’」』）)", text[end]) {
				end++
			}
			appendPart(start, end)
			start = end
			i = end - 1
		} else if r == '\n' || r == '\r' || end-start >= 200 {
			appendPart(start, end)
			start = end
		}
	}
	appendPart(start, len(text))
	return refs
}
func sourceEvidenceText(units []sourceUnit) string {
	var b strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&b, "\n【原文单元 %d】\n", unit.ID)
		for _, ref := range unitReferences(unit, 0) {
			fmt.Fprintf(&b, "[%s] %s\n", ref.ID, ref.Quote)
		}
	}
	return b.String()
}
func normalizeEvidenceUnit(unit evidenceUnit, source sourceUnit, base int) (evidenceUnit, error) {
	if unit.ID != source.ID {
		return unit, fmt.Errorf("编号错误：期望单元%d，收到单元%d", source.ID, unit.ID)
	}
	unit.Event = strings.TrimSpace(unit.Event)
	if unit.Event == "" || utf8.RuneCountInString(unit.Event) > 500 {
		return unit, fmt.Errorf("单元%d摘要格式错误：摘要为空或超过500字", unit.ID)
	}
	if len(unit.Facts) < 1 || len(unit.Facts) > 6 {
		return unit, fmt.Errorf("单元%d事实数量错误：应为1至6条", unit.ID)
	}
	references := map[string]SourceReference{}
	for _, ref := range unitReferences(source, base) {
		references[ref.ID] = ref
	}
	for i := range unit.Facts {
		f := &unit.Facts[i]
		f.Claim = strings.TrimSpace(f.Claim)
		if f.Claim == "" || utf8.RuneCountInString(f.Claim) > 240 {
			return unit, fmt.Errorf("单元%d事实%d描述错误：为空或超过240字", unit.ID, i+1)
		}
		f.References = nil
		if len(f.SourceIDs) > 0 {
			if len(f.SourceIDs) > 8 {
				return unit, fmt.Errorf("单元%d事实%d引用数量错误：最多8处", unit.ID, i+1)
			}
			seen := map[string]bool{}
			for _, id := range f.SourceIDs {
				ref, ok := references[id]
				if !ok {
					return unit, fmt.Errorf("单元%d事实%d引用编号不存在或超出本单元：%s", unit.ID, i+1, id)
				}
				if seen[id] {
					return unit, fmt.Errorf("单元%d事实%d引用编号重复：%s", unit.ID, i+1, id)
				}
				seen[id] = true
				f.References = append(f.References, ref)
			}
			sort.Slice(f.References, func(a, b int) bool { return f.References[a].StartOffset < f.References[b].StartOffset })
			f.SourceIDs = nil
			for _, ref := range f.References {
				f.SourceIDs = append(f.SourceIDs, ref.ID)
			}
		} else {
			// 旧检查点及少数未遵循编号格式的模型回复仍须核对真实出处。
			if strings.TrimSpace(f.Quote) == "" {
				return unit, fmt.Errorf("单元%d事实%d缺少引用编号", unit.ID, i+1)
			}
			if utf8.RuneCountInString(f.Quote) > 300 {
				return unit, fmt.Errorf("单元%d事实%d旧格式引文超过300字，请改为引用编号", unit.ID, i+1)
			}
			start, end, ok := locateQuote(source.Text, f.Quote)
			if !ok {
				return unit, fmt.Errorf("单元%d事实%d引文内容不匹配：可能删改了文字或拼接了非连续段落，请返回本单元引用编号", unit.ID, i+1)
			}
			f.References = []SourceReference{{Quote: string([]rune(source.Text)[start:end]), StartOffset: base + source.Start + start, EndOffset: base + source.Start + end}}
		}
		f.ID = fmt.Sprintf("U%dF%d", unit.ID, i+1)
		f.UnitID = unit.ID
		first := f.References[0]
		f.Quote, f.StartOffset, f.EndOffset = first.Quote, first.StartOffset, first.EndOffset
	}
	return unit, nil
}
func normalizeEvidence(raw json.RawMessage, units []sourceUnit, base int) (json.RawMessage, error) {
	var digest evidenceDigest
	if err := json.Unmarshal(raw, &digest); err != nil {
		return nil, fmt.Errorf("证据JSON结构错误：%w", err)
	}
	if len(digest.Units) != len(units) {
		return nil, errors.New("证据单元数量与原文不一致")
	}
	for i, unit := range units {
		normalized, err := normalizeEvidenceUnit(digest.Units[i], unit, base)
		if err != nil {
			return nil, err
		}
		digest.Units[i] = normalized
	}
	return json.Marshal(digest)
}
