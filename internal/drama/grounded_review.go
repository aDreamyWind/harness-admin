package drama

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ReviewIssue struct {
	ID            string    `json:"id"`
	RunID         string    `json:"-"`
	EpisodeNumber int       `json:"episodeNumber"`
	ContextHash   string    `json:"-"`
	Key           string    `json:"-"`
	Category      string    `json:"category"`
	Severity      string    `json:"severity"`
	Message       string    `json:"message"`
	SourceQuote   string    `json:"sourceQuote"`
	ScriptQuote   string    `json:"scriptQuote"`
	SourceStart   int       `json:"sourceStart"`
	SourceEnd     int       `json:"sourceEnd"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
type reviewCheck struct {
	ID       string `json:"id"`
	Resolved *bool  `json:"resolved"`
}
type reviewFinding struct {
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	SourceQuote string `json:"sourceQuote"`
	ScriptQuote string `json:"scriptQuote"`
}
type groundedReview struct {
	Checks   []reviewCheck   `json:"checks"`
	Findings []reviewFinding `json:"findings"`
}
type scriptPatch struct {
	Replacements []struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	} `json:"replacements"`
	Synopsis   *string             `json:"synopsis"`
	Characters []EpisodeAssetDraft `json:"characters"`
	Scenes     []EpisodeAssetDraft `json:"scenes"`
	Props      []EpisodeAssetDraft `json:"props"`
}

func mergeReview(reply groundedReview, history []ReviewIssue, episode EpisodeDraft, original string, start, number int, job BatchAnalysisJob, hash string) ([]ReviewIssue, error) {
	if reply.Checks == nil || reply.Findings == nil {
		return nil, errors.New("审校必须返回checks和findings数组")
	}
	out := append([]ReviewIssue{}, history...)
	known := map[string]int{}
	checked := map[string]bool{}
	for i, issue := range out {
		if issue.Severity == "blocking" {
			known[issue.ID] = i
		}
	}
	for _, check := range reply.Checks {
		i, ok := known[check.ID]
		if !ok || checked[check.ID] || check.Resolved == nil {
			return nil, errors.New("必须逐项复核历史问题ID，不能遗漏、重复或编造")
		}
		checked[check.ID] = true
		out[i].Status = "open"
		if *check.Resolved {
			out[i].Status = "resolved"
		}
	}
	if len(checked) != len(known) {
		return nil, errors.New("必须复核已解决问题，避免修正后回归")
	}
	whole, _ := json.Marshal(episode)
	for _, finding := range reply.Findings {
		if finding.Category != "fact" && finding.Category != "continuity" && finding.Category != "causality" && finding.Category != "style" {
			return nil, errors.New("无效审校类别")
		}
		if finding.Severity != "blocking" && finding.Severity != "suggestion" {
			return nil, errors.New("无效审校级别")
		}
		if finding.Category == "style" {
			finding.Severity = "suggestion"
		}
		if strings.TrimSpace(finding.Message) == "" || utf8.RuneCountInString(finding.Message) > 1200 {
			return nil, errors.New("审校缺少明确问题描述")
		}
		quoteStart, quoteEnd, quoteFound := locateQuote(original, finding.SourceQuote)
		index := -1
		if quoteFound {
			index = quoteStart
			finding.SourceQuote = string([]rune(original)[quoteStart:quoteEnd])
		}
		if finding.Severity == "blocking" && (strings.TrimSpace(finding.SourceQuote) == "" || index < 0) {
			return nil, errors.New("阻断问题必须引用当前集原文的真实依据")
		}
		if finding.SourceQuote != "" && index < 0 {
			return nil, errors.New("审校引用了本集原文以外的依据")
		}
		if finding.ScriptQuote != "" {
			if start, end, ok := locateQuote(episode.ScriptText, finding.ScriptQuote); ok {
				finding.ScriptQuote = string([]rune(episode.ScriptText)[start:end])
			} else if start, end, ok := locateQuote(episode.Synopsis, finding.ScriptQuote); ok {
				finding.ScriptQuote = string([]rune(episode.Synopsis)[start:end])
			}
		}
		if finding.ScriptQuote != "" && !strings.Contains(episode.ScriptText, finding.ScriptQuote) && !strings.Contains(episode.Synopsis, finding.ScriptQuote) && !strings.Contains(string(whole), finding.ScriptQuote) {
			return nil, errors.New("审校指控的剧本文字不存在")
		}
		key := inputFingerprint(job, finding.Category+"\x00"+finding.SourceQuote+"\x00"+finding.ScriptQuote+"\x00"+finding.Message)
		duplicate := -1
		for i, issue := range out {
			if issue.Key == key {
				duplicate = i
				break
			}
		}
		if duplicate >= 0 {
			out[duplicate].Status = "open"
			continue
		}
		position := start
		if finding.SourceQuote != "" {
			position += index
		}
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(job.Batch.AnalysisRunID+hash+fmt.Sprint(number)+key)).String()
		out = append(out, ReviewIssue{ID: id, RunID: job.Batch.AnalysisRunID, EpisodeNumber: number, ContextHash: hash, Key: key, Category: finding.Category, Severity: finding.Severity, Message: finding.Message, SourceQuote: finding.SourceQuote, ScriptQuote: finding.ScriptQuote, SourceStart: position, SourceEnd: position + utf8.RuneCountInString(finding.SourceQuote), Status: "open"})
	}
	return out, nil
}
func issuePromptJSON(history []ReviewIssue) []byte {
	list := []map[string]any{}
	for _, i := range history {
		list = append(list, map[string]any{"id": i.ID, "category": i.Category, "severity": i.Severity, "message": i.Message, "sourceQuote": i.SourceQuote, "scriptQuote": i.ScriptQuote, "status": i.Status})
	}
	sort.Slice(list, func(i, j int) bool { return list[i]["id"].(string) < list[j]["id"].(string) })
	raw, _ := json.Marshal(list)
	return raw
}

// 审校逐项检查所有历史阻断约束，不依赖旧状态；状态变化不应使已通过的检查点失效。
func reviewConstraintJSON(history []ReviewIssue) []byte {
	list := []map[string]any{}
	for _, i := range history {
		if i.Severity == "blocking" {
			list = append(list, map[string]any{"id": i.ID, "category": i.Category, "severity": i.Severity, "message": i.Message, "sourceQuote": i.SourceQuote, "scriptQuote": i.ScriptQuote})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i]["id"].(string) < list[j]["id"].(string) })
	raw, _ := json.Marshal(list)
	return raw
}
func blockingIssues(issues []ReviewIssue) []string {
	out := []string{}
	for _, issue := range issues {
		if issue.Severity == "blocking" && issue.Status == "open" {
			out = append(out, issue.Message)
		}
	}
	return out
}
func patchEpisode(before EpisodeDraft, patch scriptPatch, assets BatchDraft, original string) (EpisodeDraft, error) {
	after := before
	if patch.Replacements == nil {
		return after, errors.New("局部修正必须返回replacements数组")
	}
	for _, change := range patch.Replacements {
		if change.OldText == "" || utf8.RuneCountInString(change.OldText) > 2000 {
			return before, errors.New("局部替换须提供不超过2000字的现有片段")
		}
		start, end, ok := locateQuote(after.ScriptText, change.OldText)
		source, _, _ := normalizedQuote(after.ScriptText)
		needle, _, _ := normalizedQuote(strings.TrimSpace(change.OldText))
		if !ok || needle == "" || strings.Count(source, needle) != 1 {
			return before, errors.New("局部替换须匹配唯一片段，不能匹配不存在或重复的文字")
		}
		text := []rune(after.ScriptText)
		after.ScriptText = string(text[:start]) + change.NewText + string(text[end:])
	}
	if patch.Synopsis != nil {
		after.Synopsis = *patch.Synopsis
	}
	if patch.Characters != nil {
		after.Characters = patch.Characters
	}
	if patch.Scenes != nil {
		after.Scenes = patch.Scenes
	}
	if patch.Props != nil {
		after.Props = patch.Props
	}
	if err := validateGroundedEpisode(after, assets, original); err != nil {
		return before, err
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) == string(b) {
		return before, errors.New("修正没有产生任何有效变化")
	}
	return after, nil
}
func (w *batchWorkflow) writeReviewedEpisode(number, total int, plan EpisodePlan, assets BatchDraft, original string, start int, base string, continuity ...string) (EpisodeDraft, error) {
	previous := ""
	if len(continuity) > 0 {
		previous = continuity[0]
	}
	var episode EpisodeDraft
	raw, err := w.runStep("director", number, total, base, func(raw json.RawMessage) error {
		var value struct {
			Episodes []EpisodeDraft `json:"episodes"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if len(value.Episodes) != 1 || value.Episodes[0].Title != plan.Title {
			return errors.New("只能生成规划中的当前一集，标题必须一致")
		}
		return validateGroundedEpisode(value.Episodes[0], assets, original)
	}, false)
	if err != nil {
		return episode, err
	}
	var directed struct {
		Episodes []EpisodeDraft `json:"episodes"`
	}
	if err = json.Unmarshal(raw, &directed); err != nil {
		return episode, err
	}
	episode = directed.Episodes[0]
	hash := inputFingerprint(w.job, base)
	all, err := w.service.repository.ListReviewIssues(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID)
	if err != nil {
		return episode, err
	}
	history := []ReviewIssue{}
	for _, issue := range all {
		if issue.EpisodeNumber == number && issue.ContextHash == hash {
			history = append(history, issue)
		}
	}
	currentJSON, _ := json.Marshal(episode)
	cachedPrompt := fmt.Sprintf(groundedReviewPrompt, string(reviewConstraintJSON(history)), previous, original, string(currentJSON))
	if old, ok := w.saved[fmt.Sprintf("review:%d", number)]; ok && old.Status == "completed" && old.InputHash == inputFingerprint(w.job, cachedPrompt) {
		var reply groundedReview
		if err = json.Unmarshal([]byte(old.ResultText), &reply); err != nil {
			return episode, err
		}
		history, err = mergeReview(reply, history, episode, original, start, number, w.job, hash)
		if err != nil {
			return episode, err
		}
		if err = w.service.repository.SaveReviewIssues(w.ctx, w.job, history); err != nil {
			return episode, err
		}
		if len(blockingIssues(history)) == 0 {
			return episode, nil
		}
	}
	repairs := 0
	for {
		open := blockingIssues(history)
		if len(open) > 0 {
			if repairs >= 2 {
				return episode, fmt.Errorf("第%d集仍有待修正的事实问题：%s", number, strings.Join(open, "；"))
			}
			if err = w.emit("review", number, "repair", "局部修正当前问题，并保留此前已解决约束："+strings.Join(open, "；")); err != nil {
				return episode, err
			}
			before, _ := json.Marshal(episode)
			issues := issuePromptJSON(history)
			var after EpisodeDraft
			_, err = w.runStep("repair", number, total, fmt.Sprintf(groundedPatchPrompt, string(before), string(issues), original), func(raw json.RawMessage) error {
				var patch scriptPatch
				if err := json.Unmarshal(raw, &patch); err != nil {
					return err
				}
				var err error
				after, err = patchEpisode(episode, patch, assets, original)
				return err
			}, false)
			if err != nil {
				return episode, err
			}
			episode = after
			repaired, _ := json.Marshal(map[string]any{"episodes": []EpisodeDraft{episode}})
			if err = w.service.repository.SaveRepairedEpisode(w.ctx, w.job, number, string(repaired), hash); err != nil {
				return episode, err
			}
			if err = w.emit("repair", number, "completed", "本集局部修正正文已保存，继续核对累计约束"); err != nil {
				return episode, err
			}
			repairs++
		}
		current, _ := json.Marshal(episode)
		issues := reviewConstraintJSON(history)
		var updated []ReviewIssue
		_, err = w.runStep("review", number, total, fmt.Sprintf(groundedReviewPrompt, string(issues), previous, original, string(current)), func(raw json.RawMessage) error {
			var reply groundedReview
			if err := json.Unmarshal(raw, &reply); err != nil {
				return err
			}
			var err error
			updated, err = mergeReview(reply, history, episode, original, start, number, w.job, hash)
			return err
		}, true)
		if err != nil {
			return episode, err
		}
		if err = w.service.repository.SaveReviewIssues(w.ctx, w.job, updated); err != nil {
			return episode, err
		}
		history = updated
		open = blockingIssues(history)
		if len(open) == 0 {
			return episode, nil
		}
		if err = w.service.repository.FailBatchStep(w.ctx, w.job, "review", number, strings.Join(open, "；")); err != nil {
			return episode, err
		}
	}
}

const groundedReviewPrompt = `你是剧本审校，只对当前集完整原文及已发生的事实负责。允许压缩重复描写、调整措辞和可视化表达。不要求逐句照抄，不把普通细节省略或审美偏好升级为阻断。fact=事实错，continuity=身份状态衔接错，causality=影响理解的关键因果缺失，style=表达建议。style只能suggestion。引用的sourceQuote必须逐字来自当前原文，scriptQuote须为剧本中实际存在的文字；遗漏可留空scriptQuote。
必须逐项复核全部历史阻断问题ID，无论之前是否已解决；发现问题仍存在或重新出现就resolved=false，已解决则true。不要重复新建已有问题。表达建议不要求反复复核。承接信息已发生，可作背景；不能只因当前原文没有重述承接信息就报新增事实，不能要求补演之前剧情。没有明确当前原文依据的判断只能列建议。
返回JSON：{"checks":[{"id":"历史问题ID","resolved":true或false}],"findings":[{"category":"fact|continuity|causality|style","severity":"blocking|suggestion","message":"具体问题","sourceQuote":"原文依据","scriptQuote":"问题文字或空"}]}。无历史问题时checks为空；没有新问题时findings为空。禁止用外部知识、全书概览或后续章节否定当前原文。
累计历史约束：%s
已确认承接信息：%s
当前集完整原文：%s
当前剧本：%s`
const groundedPatchPrompt = `你是剧本修订编辑。仅修正累计问题中open且blocking的错误，resolved的问题是必须保留的回归约束。优先局部修正，不重写整集，不改标题，保持无关段落原样。当前集原文是事实依据，禁止引入后续章节。
返回JSON：{"replacements":[{"oldText":"从现有scriptText逐字复制的唯一连续片段，不超过2000字","newText":"修正后的该片段"}],"synopsis":null,"characters":null,"scenes":null,"props":null}。仅在梗概或关联资产确实需要改动时提供对应新值，否则null。修改关联时须保留name、stateLabel、sourceQuote且sourceQuote必须来自当前原文。允许空newText表示删除错误片段；禁止空oldText。不能返回整集episodes。
当前剧本：%s
全部历史问题与已解决约束：%s
当前集原文：%s`
