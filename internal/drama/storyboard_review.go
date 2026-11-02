package drama

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 旧字符串和问题对象统一为可读问题；对象中的镜头、字段、依据和修正建议完整保留。
// 不把合法的审阅意见误当成镜头生成格式错误。
type storyboardReviewIssues []string

func (issues *storyboardReviewIssues) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errors.New("审阅意见必须为数组，不能为null")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	result := make(storyboardReviewIssues, 0, len(items))
	for _, item := range items {
		var text string
		if json.Unmarshal(item, &text) == nil {
			if strings.TrimSpace(text) == "" {
				return errors.New("审阅意见不能为空")
			}
			result = append(result, text)
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(item, &object); err != nil || len(object) == 0 {
			return errors.New("审阅意见须为非空字符串或问题对象")
		}
		meaningful := false
		for key, value := range object {
			var s string
			if key != "shot" && key != "shotNumber" && key != "field" && json.Unmarshal(value, &s) == nil && strings.TrimSpace(s) != "" {
				meaningful = true
			}
		}
		if !meaningful {
			return errors.New("审阅问题对象缺少具体说明")
		}
		compact, err := json.Marshal(object)
		if err != nil {
			return err
		}
		result = append(result, string(compact))
	}
	*issues = result
	return nil
}

type storyboardReviewReport struct {
	Pacing       []pacingIssue          `json:"pacing"`
	PacingChecks []pacingCheck          `json:"pacingChecks"`
	Blocking     storyboardReviewIssues `json:"blocking"`
	Suggestions  storyboardReviewIssues `json:"suggestions"`
}

// 来源和类型由程序填写，模型无法修改。存入中心派单的问题文本，便于跨节点追踪。
type workflowProblem struct {
	Origin, Code, Field string
	Shot                int
	Cause               error
}

func (p *workflowProblem) Error() string {
	location := p.Field
	if p.Shot > 0 {
		location = fmt.Sprintf("镜头%d.%s", p.Shot, p.Field)
	}
	return fmt.Sprintf("来源=%s；类型=%s；字段=%s：%v", p.Origin, p.Code, location, p.Cause)
}
func (p *workflowProblem) Unwrap() error { return p.Cause }
func storyboardProblem(err error, origin, code, field string) error {
	if err == nil {
		return nil
	}
	var known *workflowProblem
	if errors.As(err, &known) {
		return err
	}
	return &workflowProblem{Origin: origin, Code: code, Field: field, Cause: err}
}

func readableReviewSuggestions(issues storyboardReviewIssues) string {
	result := make([]string, 0, len(issues))
	for _, issue := range issues {
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(issue), &object) != nil {
			result = append(result, issue)
			continue
		}
		details := []string{}
		for _, key := range []string{"message", "reason", "suggestion", "建议", "依据", "最小修正建议", "fix"} {
			var value string
			if json.Unmarshal(object[key], &value) == nil && strings.TrimSpace(value) != "" {
				details = append(details, value)
			}
		}
		if len(details) == 0 {
			result = append(result, issue)
		} else {
			result = append(result, strings.Join(details, "；"))
		}
	}
	return strings.Join(result, "；")
}
