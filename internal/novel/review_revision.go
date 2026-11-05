package novel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const reviseFromReviewPrompt = `这是一次按审稿意见做的局部修订。以原稿为底稿，只修正有原文依据、影响剧情衔接的问题，并同步连续性摘要。作者的明确称呼、语气和设定优先于审稿意见。不要执行可选润色、无证据的推测或示例里的虚构数值。没有需要改动的问题时保留原稿和摘要，不改动其他段落。
`

const verifyReviewPrompt = `复核新稿是否解决了上一轮列出的问题。按旧意见的 id 逐项判断：问题已解决、不影响衔接、证据不足或与作者约定冲突时，resolved 为 true；仍有明确证据、确实影响衔接时为 false。每个 id 恰好检查一次。
` + authorContinuityGuidance + `这是复核，不能重审未改动的部分。最多补报两个本次修订确实引入的严重事实矛盾，每条需要在新稿正文或摘要中逐字引用一处旧稿没有的文字（至少五字），并说明冲突和最小修法。不要补报原稿已有的问题、措辞偏好或可选润色。没有原稿可比时，regressions 必须为空。
只输出 JSON 对象，格式为 {"checks":[{"id":1,"resolved":true}],"regressions":[{"quote":"新稿原文","issue":"有证据的矛盾","fix":"最小修法"}]}。没有新回归时 regressions 为 []。
复核资料：
`

type reviewIssueToVerify struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func reviewIssuesToVerify(review string) []reviewIssueToVerify {
	issues := []reviewIssueToVerify{}
	for _, part := range strings.Split(strings.ReplaceAll(review, "\r\n", "\n"), "\n\n") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "【可选润色】") || strings.HasPrefix(part, "可选润色：") {
			continue
		}
		issues = append(issues, reviewIssueToVerify{ID: len(issues) + 1, Text: part})
	}
	return issues
}

func reviewText(issues []reviewIssueToVerify) string {
	text := make([]string, 0, len(issues))
	for _, issue := range issues {
		text = append(text, issue.Text)
	}
	return strings.Join(text, "\n\n")
}

type reviewVerification struct {
	Checks []struct {
		ID       int   `json:"id"`
		Resolved *bool `json:"resolved"`
	} `json:"checks"`
	Regressions []struct {
		Quote string `json:"quote"`
		Issue string `json:"issue"`
		Fix   string `json:"fix"`
	} `json:"regressions"`
}

func (s *Service) verifyChapterReview(ctx context.Context, owner Owner, p *Project, index int, modelCode string, previous Chapter) (string, error) {
	issues := reviewIssuesToVerify(previous.Review)
	if len(issues) == 0 {
		return "", nil
	}
	current := p.Chapters[index]
	material := struct {
		Current         json.RawMessage       `json:"current"`
		Issues          []reviewIssueToVerify `json:"issues"`
		PreviousBody    string                `json:"previousBody,omitempty"`
		PreviousSummary string                `json:"previousSummary,omitempty"`
	}{Current: json.RawMessage(chapterContext(*p, index, current.AuthorInstruction, "")), Issues: issues}
	if previous.Body != current.Body || previous.Summary != current.Summary {
		material.PreviousBody, material.PreviousSummary = previous.Body, previous.Summary
	}
	var reply reviewVerification
	if err := s.generate(ctx, owner, p, modelCode, verifyReviewPrompt+asJSON(material), &reply); err != nil {
		return "", err
	}
	return verifiedReviewText(issues, previous, current, reply)
}

func verifiedReviewText(issues []reviewIssueToVerify, previous, current Chapter, reply reviewVerification) (string, error) {
	if len(reply.Checks) != len(issues) {
		return "", fmt.Errorf("%w，复核未覆盖全部旧问题", ErrGeneration)
	}
	checked := make([]*bool, len(issues))
	for _, check := range reply.Checks {
		if check.ID < 1 || check.ID > len(issues) || check.Resolved == nil || checked[check.ID-1] != nil {
			return "", fmt.Errorf("%w，复核返回了无效的问题编号或结果", ErrGeneration)
		}
		checked[check.ID-1] = check.Resolved
	}
	remaining := []string{}
	for i, issue := range issues {
		if !*checked[i] {
			remaining = append(remaining, issue.Text)
		}
	}
	seen := map[string]bool{}
	for _, finding := range reply.Regressions {
		quote, issue, fix := strings.TrimSpace(finding.Quote), strings.TrimSpace(finding.Issue), strings.TrimSpace(finding.Fix)
		if utf8.RuneCountInString(quote) < 5 || !valid(quote, 160) || !valid(issue, 300) || !valid(fix, 300) || seen[quote] ||
			(strings.Contains(previous.Body, quote) || strings.Contains(previous.Summary, quote)) ||
			(!strings.Contains(current.Body, quote) && !strings.Contains(current.Summary, quote)) {
			continue
		}
		line := fmt.Sprintf("【本次修订引入】%s\n依据：「%s」\n修法：%s", issue, quote, fix)
		if !valid(strings.Join(append(remaining, line), "\n\n"), 4000) {
			break
		}
		remaining = append(remaining, line)
		seen[quote] = true
		if len(seen) == 2 {
			break
		}
	}
	return strings.Join(remaining, "\n\n"), nil
}
