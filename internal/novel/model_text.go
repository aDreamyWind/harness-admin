package novel

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// 只取完整的模型 JSON。部分模型会在结构化答案后附带自然语言说明。
func decodeModelOutput(raw string, output any) error {
	raw = strings.TrimSpace(raw)
	fenced := strings.HasPrefix(raw, "```")
	if fenced {
		_, raw, _ = strings.Cut(raw, "\n")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(output); err != nil {
		return err
	}
	tail := strings.TrimSpace(raw[decoder.InputOffset():])
	if fenced {
		tail = strings.TrimSpace(strings.TrimPrefix(tail, "```"))
	}
	if tail != "" {
		var extra json.RawMessage
		if json.NewDecoder(strings.NewReader(tail)).Decode(&extra) == nil || strings.ContainsAny(tail, "{}[]`") {
			return errors.New("模型返回了额外的结构化数据")
		}
	}
	return nil
}

func parseReview(raw json.RawMessage) (string, error) {
	if len(raw) > 0 && raw[0] == '{' {
		var wrapped struct {
			Review json.RawMessage `json:"review"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return "", err
		}
		raw = wrapped.Review
	}
	return normalizeTextParts(raw, 4000)
}

func normalizeTextParts(raw json.RawMessage, limit int) (string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		single = strings.TrimSpace(single)
		if valid(single, limit) {
			return single, nil
		}
		return "", errors.New("文本为空或超过长度限制")
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("文本须为字符串或数组: %w", err)
	}
	lines := make([]string, 0, len(items))
	for i, item := range items {
		line, err := textPart(item)
		if err != nil {
			return "", fmt.Errorf("第 %d 个文本条目格式无效: %w", i+1, err)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	review := strings.Join(lines, "\n\n")
	if !valid(review, limit) {
		return "", errors.New("文本为空或超过长度限制")
	}
	return review, nil
}

func textPart(raw json.RawMessage) (string, error) {
	var line string
	if err := json.Unmarshal(raw, &line); err == nil {
		return strings.TrimSpace(line), nil
	}

	// 模型也可能把单条意见或人物设定拆成多个字段。
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	labels := []struct{ key, label string }{
		{"name", "姓名"}, {"role", "身份"}, {"motivation", "动机"}, {"relationship", "关系"},
		{"severity", "严重程度"}, {"type", "类型"}, {"category", "类别"}, {"title", "标题"},
		{"issue", "问题"}, {"problem", "问题"}, {"description", "说明"}, {"evidence", "依据"},
		{"reason", "原因"}, {"suggestion", "建议"}, {"fix", "修法"},
	}
	parts := make([]string, 0, len(fields))
	for _, field := range labels {
		if value, ok := fields[field.key]; ok {
			parts = appendTextField(parts, field.label, value)
			delete(fields, field.key)
		}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = appendTextField(parts, key, fields[key])
	}
	return strings.Join(parts, "\n"), nil
}

func appendTextField(parts []string, label string, value json.RawMessage) []string {
	var content string
	if err := json.Unmarshal(value, &content); err != nil {
		content = string(value)
	}
	if content = strings.TrimSpace(content); content != "" {
		parts = append(parts, label+"："+content)
	}
	return parts
}
