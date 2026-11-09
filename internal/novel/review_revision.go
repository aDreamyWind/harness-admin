package novel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const reviseFromReviewPrompt = `这是一次按审稿意见做的局部修订。blocks 是本章原稿的编号句段，issues 按 id 列出待处理意见。逐条处理，凡是正文里的事实、动作或衔接有问题，就修改正文对应的句子。只改摘要不能解决正文问题。
每条正文改动用 blockId 选中 blocks 中的一个编号，after 填这个编号对应句段的完整修改版。after 只替换该句段，保留其中不相关的句子。删除整个句段时 after 为空字符串。同一处改动能解决多条意见时，在 issueIds 列出全部编号。每个 blockId 只提交一次，不返回 before，也不用重抄其他句段。找不到可靠修法的意见留给复核，不凭空补事实。
缺少行动前提时，可撤回提前兑现的行动、明确行动受阻或筹备，或者补上符合定稿的资源来源和生效过程。意见同时给出大纲和正文修法时，选择正文修法，必要时交代线索与下一章任务的关系。若旧线索指向的人与下一章调查对象不同，保留旧证据能证明的关系；用本章现有的记录、证物或在场人物，补出角色当场能核对的具体依据和下一步行动，让新调查对象有来由。不能只把一个人的身份或官职换成另一个，也不能把尚待核实的线索写成定论。保留作者约定、已定稿事实、无关情节和后续伏笔。摘要应准确反映应用改动后的正文，最多 500 字。
只输出 JSON 对象 {"edits":[{"issueIds":[1],"blockId":2,"after":"编号 2 句段的完整修改版"}],"summary":"与修订后正文一致的连续性摘要"}。只有问题确实仅涉及摘要，或核对后所有意见都不适用时，才可以返回空 edits。意见全不适用时保留原摘要。每个 after 最多 1200 字，总改动最多 12 处。
修订资料：
`

const verifyReviewPrompt = `复核新稿是否解决了上一轮列出的问题。current.Draft 是唯一的本章当前正文，current.DraftSummary 是当前摘要。issues 是上次的意见，里面引用的原句可能已被删除，不能把它当成新稿仍有的证据。按 id 逐项核对当前正文：问题已解决、不影响衔接、证据不足或与作者约定冲突时，resolved 为 true；当前稿仍有明确证据且影响衔接时为 false。每个 id 恰好检查一次。
` + authorContinuityGuidance + resourceCausalityGuidance + `问题在正文里时，仅更新摘要或在不相干的段落改字不能判为解决。旧问题若涉及缺少行动前提，只有新稿交代资源确实到位，或明确把该行动改成筹备、受阻并撤回提前兑现的结果，才算解决。最多补报两个本次修订引入的严重事实矛盾。changedText 是修改后的句段或摘要，只从中逐字引用一处新文本（至少五字），说明冲突和最小修法。程序会比对旧稿。不要补报原有问题、措辞偏好或可选润色。changedText 为空时，regressions 必须为空。
对仍未解决的意见，在 reason 中简短指出新正文还缺的具体信息或证据。若意见同时指出两处缺失，要分清已修好哪处、还缺哪处。只输出 JSON 对象，格式为 {"checks":[{"id":1,"resolved":false,"reason":"新正文仍缺少的事实或衔接"}],"regressions":[{"quote":"新稿原文","issue":"有证据的矛盾","fix":"最小修法"}]}。没有新回归时 regressions 为 []。
复核资料：
`

const verifySummaryOnlyGuidance = `这次正文完全没改。每个 checks 条目额外填写 scope。原问题只存在于旧摘要且新摘要已修好时填 summary。原意见经核对确实不适用时填 not_applicable。仍需改正文时填 body，且 resolved 必须为 false。
`

type reviewEdit struct {
	IssueIDs []int   `json:"issueIds"`
	BlockID  *int    `json:"blockId"`
	Before   string  `json:"before,omitempty"`
	After    *string `json:"after"`
}

func (s *Service) reviseChapterFromReview(ctx context.Context, owner Owner, p *Project, index int, modelCode string) error {
	chapter := &p.Chapters[index]
	if chapter.Body == "" || chapter.Review == "" || chapter.ReviewFailed || chapter.ReviewPassed {
		return fmt.Errorf("%w，请先获取本章有效的审稿意见", ErrState)
	}
	previous := *chapter
	issues := reviewIssuesToVerify(previous.Review)
	if len(issues) == 0 {
		chapter.Review, chapter.ReviewPassed = "", true
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	prompt := s.chapterRevisionPrompt(ctx, owner, *p, index, issues)
	attempts := 0
	candidate, err := retryChapterCandidate(ctx, p.ID, prompt, "请返回正文的局部改动，每处选用已有的 blockId 并返回该句段的完整替换文本。", func(prompt string) (Chapter, error) {
		attempts++
		return s.generateReviewCandidate(ctx, owner, p, index, modelCode, prompt, previous, issues)
	})
	if err != nil {
		return err
	}
	*chapter = candidate
	if candidate.Body != previous.Body {
		feedback := make(map[int]string)
		s.finishChapterDraft(ctx, owner, p, index, modelCode, &previous, feedback)
		if attempts == 1 && ctx.Err() == nil && !chapter.ReviewFailed {
			s.improveReviewRevision(ctx, owner, p, index, modelCode, previous, issues, feedback)
		}
		if !chapter.ReviewFailed {
			remaining := reviewIssuesToVerify(chapter.Review)
			allRemain := !slices.ContainsFunc(issues, func(issue reviewIssueToVerify) bool {
				return !hasReviewIssue(remaining, issue.Text)
			})
			if allRemain {
				return &DraftOutputError{Message: "复核确认原意见仍未解决，已保留原稿。"}
			}
		}
	}
	chapter.LastReviewRevision = &ReviewRevisionOutcome{
		BodyChanged: chapter.Body != previous.Body, SummaryChanged: chapter.Summary != previous.Summary,
		CompletedAt: time.Now().UTC(),
	}
	return nil
}

func (s *Service) chapterRevisionPrompt(ctx context.Context, owner Owner, p Project, index int, issues []reviewIssueToVerify) string {
	instruction := p.Chapters[index].AuthorInstruction
	memory := s.memoryForChapter(ctx, owner, p, index, instruction)
	material := struct {
		Current json.RawMessage       `json:"current"`
		Blocks  []reviewBlock         `json:"blocks"`
		Issues  []reviewIssueToVerify `json:"issues"`
	}{
		Current: json.RawMessage(chapterContext(p, index, instruction, "", memory)),
		Blocks:  splitReviewBlocks(p.Chapters[index].Body),
		Issues:  issues,
	}
	return memory.guidance() + authorContinuityGuidance + resourceCausalityGuidance + reviseFromReviewPrompt + asJSON(material)
}

func (s *Service) generateReviewCandidate(ctx context.Context, owner Owner, p *Project, index int, modelCode, prompt string, previous Chapter, issues []reviewIssueToVerify) (Chapter, error) {
	candidate, err := s.generateReviewPatch(ctx, owner, p, modelCode, prompt, previous, issues)
	if err != nil || candidate.Body != previous.Body {
		return candidate, err
	}
	if candidate.Summary == "" {
		return Chapter{}, &DraftOutputError{Message: "模型没有返回可应用的正文修改，已保留原稿。"}
	}
	summaryProject := *p
	summaryProject.Chapters = slices.Clone(p.Chapters)
	summaryProject.Chapters[index] = candidate
	review, err := s.verifyChapterReview(ctx, owner, &summaryProject, index, modelCode, previous, true, nil)
	if err != nil {
		slog.WarnContext(ctx, "小说仅修改摘要的结果无法复核", "project_id", p.ID, "chapter", index+1, "error", err)
		return Chapter{}, fmt.Errorf("%w，无法确认只更新摘要是否足够", ErrGeneration)
	}
	if review != "" {
		return Chapter{}, &DraftOutputError{Message: "原意见仍未解决，只改摘要不够。请修改意见指向的正文句段。"}
	}
	candidate.ReviewPassed = true
	return candidate, nil
}

func (s *Service) generateReviewPatch(ctx context.Context, owner Owner, p *Project, modelCode, prompt string, base Chapter, issues []reviewIssueToVerify) (Chapter, error) {
	var reply struct {
		Edits   []reviewEdit    `json:"edits"`
		Summary json.RawMessage `json:"summary"`
	}
	if err := s.generate(ctx, owner, p, modelCode, prompt, &reply); err != nil {
		return Chapter{}, err
	}
	body, err := applyReviewEdits(base.Body, reply.Edits, issues)
	if err != nil {
		return Chapter{}, err
	}
	summary, _ := normalizeTextParts(reply.Summary, maxDraftSummaryRunes)
	return Chapter{Body: body, Summary: summary, AuthorInstruction: base.AuthorInstruction}, nil
}

func (s *Service) improveReviewRevision(ctx context.Context, owner Owner, p *Project, index int, modelCode string, original Chapter, issues []reviewIssueToVerify, feedback map[int]string) {
	first := p.Chapters[index]
	open := reviewIssuesToVerify(first.Review)
	pending := make([]reviewIssueToVerify, 0, len(open))
	for _, issue := range issues {
		if hasReviewIssue(open, issue.Text) {
			issue.Feedback = feedback[issue.ID]
			pending = append(pending, issue)
		}
	}
	if len(pending) == 0 || len(pending) < len(issues) && len(pending) != len(open) {
		return
	}
	base := first
	material := *p
	guidance := "第一轮已修好其他意见。以 blocks 中的第一轮新稿为底稿，仅修改复核仍未解决的以下意见，已经解决的段落保持原样。若前一轮只增加了无依据的猜测，要在正文中交代角色能核对的证据或明确可执行的下一步。\n"
	if len(pending) == len(issues) {
		base = original
		material.Chapters = slices.Clone(p.Chapters)
		material.Chapters[index] = original
		guidance = "上一版未通过复核。以 blocks 中的原稿为底稿，按照每条意见的复核反馈重新做局部改动。\n"
	}
	prompt := guidance + s.chapterRevisionPrompt(ctx, owner, material, index, pending)
	improved, err := s.generateReviewPatch(ctx, owner, p, modelCode, prompt, base, pending)
	if err != nil {
		slog.WarnContext(ctx, "小说按剩余意见补改失败，保留已修订的草稿", "project_id", p.ID, "chapter", index+1, "error", err)
		return
	}
	if improved.Body == base.Body || improved.Summary == "" {
		return
	}
	staged := *p
	staged.Chapters = slices.Clone(p.Chapters)
	staged.Chapters[index] = improved
	s.reviewAfterDraftChange(ctx, owner, &staged, index, modelCode, &original, nil, nil)
	improved = staged.Chapters[index]
	after := reviewIssuesToVerify(improved.Review)
	if improved.ReviewFailed || len(after) >= len(pending) || slices.ContainsFunc(after, func(issue reviewIssueToVerify) bool {
		return !hasReviewIssue(pending, issue.Text)
	}) {
		return
	}
	p.Chapters[index] = improved
}

func hasReviewIssue(issues []reviewIssueToVerify, text string) bool {
	return slices.ContainsFunc(issues, func(issue reviewIssueToVerify) bool { return issue.Text == text })
}

type reviewBlock struct {
	ID         int    `json:"id"`
	Text       string `json:"text"`
	start, end int
}

func splitReviewBlocks(body string) []reviewBlock {
	var blocks []reviewBlock
	offset := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		remaining := strings.TrimSpace(line)
		start := offset + strings.Index(line, remaining)
		offset += len(line)
		for remaining != "" {
			length := reviewBlockLength(remaining)
			blocks = append(blocks, reviewBlock{ID: len(blocks) + 1, Text: remaining[:length], start: start, end: start + length})
			remaining = remaining[length:]
			start += length
		}
	}
	return blocks
}

func reviewBlockLength(text string) int {
	const maxRunes = 800
	runes, boundary := 0, 0
	for index, r := range text {
		if runes == maxRunes {
			if boundary != 0 {
				return boundary
			}
			return index
		}
		runes++
		if runes >= maxRunes/2 && strings.ContainsRune("。！？；", r) {
			boundary = index + utf8.RuneLen(r)
		}
	}
	return len(text)
}

func applyReviewEdits(original string, edits []reviewEdit, issues []reviewIssueToVerify) (string, error) {
	if edits == nil || len(edits) > 12 {
		return "", &DraftOutputError{Message: "模型没有返回有效的正文局部改动。"}
	}
	if len(edits) == 0 {
		return original, nil
	}
	type replacement struct {
		start, end int
		after      string
	}
	replacements := make([]replacement, 0, len(edits))
	blocks := splitReviewBlocks(original)
	for _, edit := range edits {
		if len(edit.IssueIDs) == 0 || slices.ContainsFunc(edit.IssueIDs, func(id int) bool {
			return !slices.ContainsFunc(issues, func(issue reviewIssueToVerify) bool { return issue.ID == id })
		}) {
			return "", &DraftOutputError{Message: "有改动没有对应到有效的审稿意见。"}
		}
		if edit.After == nil || !validOptional(*edit.After, 1200) {
			return "", &DraftOutputError{Message: "模型没有返回有效的替换文本。"}
		}
		start, end, err := reviewEditRange(original, blocks, edit)
		if err != nil {
			return "", err
		}
		if reviewProseText(original[start:end]) == reviewProseText(*edit.After) {
			return "", &DraftOutputError{Message: "模型的替换内容与原稿相同或只调整了空白。"}
		}
		replacements = append(replacements, replacement{start, end, *edit.After})
	}
	slices.SortFunc(replacements, func(a, b replacement) int { return a.start - b.start })
	var body strings.Builder
	offset := 0
	for _, edit := range replacements {
		if edit.start < offset {
			return "", &DraftOutputError{Message: "多处正文改动重叠，请合并同一段的修改。"}
		}
		body.WriteString(original[offset:edit.start])
		body.WriteString(edit.after)
		offset = edit.end
	}
	body.WriteString(original[offset:])
	revised := body.String()
	if !valid(revised, maxDraftBodyRunes) || strings.TrimSpace(revised) == "" || reviewProseText(revised) == reviewProseText(original) {
		return "", &DraftOutputError{Message: "应用改动后正文为空、过长或没有实质变化，已保留原稿。"}
	}
	return revised, nil
}

func reviewEditRange(original string, blocks []reviewBlock, edit reviewEdit) (int, int, error) {
	if edit.BlockID != nil {
		id := *edit.BlockID
		if id < 1 || id > len(blocks) || edit.Before != "" {
			return 0, 0, &DraftOutputError{Message: "模型返回了无效的正文句段编号。"}
		}
		block := blocks[id-1]
		return block.start, block.end, nil
	}
	// 兼容按原句返回的结果，旧格式仍需逐字且唯一命中。
	if !valid(edit.Before, 1200) || strings.TrimSpace(edit.Before) == "" {
		return 0, 0, &DraftOutputError{Message: "模型没有返回有效的正文句段编号。"}
	}
	start := strings.Index(original, edit.Before)
	if start < 0 || strings.Contains(original[start+1:], edit.Before) {
		return 0, 0, &DraftOutputError{Message: "模型返回的改动无法定位到原稿，已保留原稿。"}
	}
	return start, start + len(edit.Before), nil
}

func reviewProseText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

type reviewIssueToVerify struct {
	ID       int    `json:"id"`
	Text     string `json:"text"`
	Feedback string `json:"feedback,omitempty"`
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
		ID       int    `json:"id"`
		Resolved *bool  `json:"resolved"`
		Scope    string `json:"scope"`
		Reason   string `json:"reason"`
	} `json:"checks"`
	Regressions []struct {
		Quote string `json:"quote"`
		Issue string `json:"issue"`
		Fix   string `json:"fix"`
	} `json:"regressions"`
}

func (s *Service) verifyChapterReview(ctx context.Context, owner Owner, p *Project, index int, modelCode string, previous Chapter, summaryOnly bool, feedback map[int]string) (string, error) {
	issues := reviewIssuesToVerify(previous.Review)
	if len(issues) == 0 {
		return "", nil
	}
	current := p.Chapters[index]
	memory := s.memoryForChapter(ctx, owner, *p, index, current.AuthorInstruction)
	material := struct {
		Issues      []reviewIssueToVerify `json:"issues"`
		ChangedText []string              `json:"changedText,omitempty"`
		Current     json.RawMessage       `json:"current"`
	}{
		Issues: issues, ChangedText: changedReviewText(previous, current),
		Current: json.RawMessage(chapterContext(*p, index, current.AuthorInstruction, "", memory)),
	}
	var reply reviewVerification
	prompt := memory.guidance() + verifyReviewPrompt + asJSON(material)
	if summaryOnly {
		prompt = verifySummaryOnlyGuidance + prompt
	}
	if err := s.generate(ctx, owner, p, modelCode, prompt, &reply); err != nil {
		return "", err
	}
	if summaryOnly {
		for _, check := range reply.Checks {
			validScope := check.Scope == "not_applicable" || check.Scope == "summary" && previous.Summary != current.Summary
			if check.Resolved != nil && !validScope {
				*check.Resolved = false
			}
		}
	}
	review, err := verifiedReviewText(issues, previous, current, reply)
	if err == nil && feedback != nil {
		for _, check := range reply.Checks {
			reason := strings.Join(strings.Fields(check.Reason), " ")
			if !*check.Resolved && valid(reason, 300) {
				feedback[check.ID] = reason
			}
		}
	}
	return review, err
}

func changedReviewText(previous, current Chapter) []string {
	var changed []string
	if previous.Body != current.Body {
		for _, block := range splitReviewBlocks(current.Body) {
			if !strings.Contains(previous.Body, block.Text) {
				changed = append(changed, block.Text)
			}
		}
	}
	if current.Summary != "" && previous.Summary != current.Summary {
		changed = append(changed, current.Summary)
	}
	return changed
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
