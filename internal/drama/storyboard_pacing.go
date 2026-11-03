package drama

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

type pacingIssue struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Action string `json:"action"`
	Shots  []int  `json:"shots"`
	Reason string `json:"reason"`
	Fix    string `json:"fix"`
}
type pacingCheck struct {
	ID       string `json:"id"`
	Resolved bool   `json:"resolved"`
	Reason   string `json:"reason"`
}
type pacingEdit struct {
	StartShot int              `json:"startShot"`
	EndShot   int              `json:"endShot"`
	Shots     []StoryboardShot `json:"shots"`
}
type pacingPatch struct {
	Edits []pacingEdit `json:"edits"`
}

func storyboardSeconds(s StoryboardSegment) float64 {
	total := 0.0
	for _, shot := range s.Shots {
		total += shot.Duration
	}
	return total
}
func storyboardPacingStats(s StoryboardSegment) string {
	silent, lead := 0.0, 0.0
	heard := false
	for _, shot := range s.Shots {
		if !storyboardHasSpeech(shot) {
			silent += shot.Duration
			if !heard {
				lead += shot.Duration
			}
		} else {
			heard = true
		}
	}
	return fmt.Sprintf("实际规划：%d镜头，合计%.1f秒，无口播镜头%.1f秒，首次口播前%.1f秒。无口播不等于无剧情，须结合动作和叙事职责判断。", len(s.Shots), storyboardSeconds(s), silent, lead)
}

// 把跨片段的无口播时长交给审阅，是否拖沓仍由实际动作和叙事变化决定。
func storyboardOpeningGap(previous []StoryboardSegment, current StoryboardSegment) string {
	lead := 0.0
	for _, shot := range current.Shots {
		if storyboardHasSpeech(shot) {
			break
		}
		lead += shot.Duration
	}
	if lead == 0 {
		return ""
	}
	tail := 0.0
	for i := len(previous) - 1; i >= 0; i-- {
		seg := previous[i]
		if seg.Status != "ready" || len(seg.Shots) == 0 {
			break
		}
		for j := len(seg.Shots) - 1; j >= 0; j-- {
			shot := seg.Shots[j]
			if storyboardHasSpeech(shot) {
				return fmt.Sprintf("跨片段连续无口播%.1f秒，其中前段累计%.1f秒，本段开头%.1f秒。", tail+lead, tail, lead)
			}
			tail += shot.Duration
		}
	}
	return fmt.Sprintf("跨片段连续无口播%.1f秒，其中前段累计%.1f秒，本段开头%.1f秒。", tail+lead, tail, lead)
}

// 手动优化时补查审阅漏掉的长停留和旁白，交给局部改写及事实复核。
func supplementManualPacingIssues(issues []pacingIssue, segment StoryboardSegment, lines []string, forms []AssetForm) []pacingIssue {
	// 先检查整组停留，避免几个各自不长的镜头叠成一段重复铺垫。
	start, quiet := 0, 0.0
	for index, shot := range segment.Shots {
		if !storyboardHasSpeech(shot) {
			quiet += shot.Duration
			if index+1 < len(segment.Shots) {
				continue
			}
		}
		if len(issues) < 4 && index > start && quiet >= 7 {
			shots := make([]int, 0, index-start+1)
			for n := start; n <= index; n++ {
				shots = append(shots, n+1)
			}
			issues = append(issues, pacingIssue{
				ID: fmt.Sprintf("P%d", len(issues)+1), Kind: "idle_hold", Action: "merge", Shots: shots,
				Reason: fmt.Sprintf("相邻镜头累计铺垫 %.1f 秒无口播，需整体检查是否重复呈现同一动作或情绪", quiet),
				Fix:    "如为同一动作或反应，合成一到两个镜头。心声可在抬眼、回望或反应时响起，保留原台词、必要动作和新信息。按并行发生的画面和口播估时，删掉重复停顿。若每镜都有不可替代的叙事推进，保留其所需时长。",
			})
		}
		start, quiet = index+1, 0
	}

	for index, shot := range segment.Shots {
		if len(issues) >= 4 {
			break
		}
		number := index + 1
		covered := false
		for _, issue := range issues {
			for _, assigned := range issue.Shots {
				covered = covered || assigned == number
			}
		}
		if covered {
			continue
		}
		issue := pacingIssue{ID: fmt.Sprintf("P%d", len(issues)+1), Action: "shorten", Shots: []int{number}}
		for _, voice := range shot.Utterances {
			if voice.Delivery != "visual" && narrationSpeaker(voice) && voice.SourceLine > 0 &&
				voice.SourceLine <= len(lines) && narrationSource(lines[voice.SourceLine-1], forms) {
				issue.Kind = "repeated_exposition"
				issue.Reason = "仍有叙述者口播"
				issue.Fix = "优先用一个可见动作或道具交代原句信息，改为画面后按实际动作缩短停留。"
				break
			}
		}
		if issue.Kind == "" && shot.Duration > 5 && storyboardSpeechSeconds(shot) <= 3 {
			issue.Kind = "idle_hold"
			issue.Reason = fmt.Sprintf("镜头 %.1f 秒，口播占用约 %.1f 秒", shot.Duration, storyboardSpeechSeconds(shot))
			issue.Fix = "检查是否有必须演完的动作。普通交代目标 1～3 秒，保留关键动作和完整角色台词。"
		}
		if issue.Kind != "" {
			issues = append(issues, issue)
		}
	}
	return issues
}

func validatePacingIssues(issues []pacingIssue, s StoryboardSegment) error {
	if len(issues) > 4 {
		return errors.New("单次节奏审阅最多列4项需执行的改动")
	}
	for i := range issues {
		p := &issues[i]
		p.ID = fmt.Sprintf("P%d", i+1)
		if (p.Kind != "repeated_exposition" && p.Kind != "idle_hold") || (p.Action != "merge" && p.Action != "shorten") || len(p.Shots) == 0 || strings.TrimSpace(p.Reason) == "" || strings.TrimSpace(p.Fix) == "" {
			return errors.New("节奏问题须明确类型、合并或缩短动作、镜头编号、拖沓原因与最小修改要求")
		}
		sort.Ints(p.Shots)
		for j, n := range p.Shots {
			if n < 1 || n > len(s.Shots) || (j > 0 && p.Shots[j-1] == n) {
				return errors.New("节奏问题引用了不存在或重复的镜头")
			}
		}
		if p.Action == "merge" {
			if len(p.Shots) < 2 {
				return errors.New("合并至少引用两个相邻镜头")
			}
			for j := 1; j < len(p.Shots); j++ {
				if p.Shots[j] != p.Shots[j-1]+1 {
					return errors.New("只能合并连续镜头，不能跨过未选镜头")
				}
			}
		}
	}
	return nil
}
func decodeStoryboardReview(raw string, s StoryboardSegment) (storyboardReviewReport, error) {
	var report storyboardReviewReport
	err := decodeStoryboard(raw, &report)
	if err == nil && (report.Blocking == nil || report.Suggestions == nil) {
		err = errors.New("审阅缺少blocking或suggestions数组")
	}
	if err == nil {
		err = validatePacingIssues(report.Pacing, s)
	}
	return report, err
}

// 按原镜头编号应用局部替换。未指定镜头直接保留，台词、口播类型和声线不能借优化改写。
func applyPacingPatch(before StoryboardSegment, issues []pacingIssue, patch pacingPatch, lines []string, forms []AssetForm) (StoryboardSegment, error) {
	if len(patch.Edits) == 0 || len(patch.Edits) > len(before.Shots) {
		return before, errors.New("节奏优化没有返回有效的局部改动")
	}
	allowed := map[int]bool{}
	for _, p := range issues {
		for _, n := range p.Shots {
			allowed[n] = true
		}
	}
	sort.Slice(patch.Edits, func(i, j int) bool { return patch.Edits[i].StartShot < patch.Edits[j].StartShot })
	candidate := before
	candidate.Shots = make([]StoryboardShot, 0, len(before.Shots))
	next := 1
	for _, edit := range patch.Edits {
		if edit.StartShot < next || edit.EndShot < edit.StartShot || edit.EndShot > len(before.Shots) || len(edit.Shots) == 0 {
			return before, errors.New("局部替换范围越界、重叠或删除了所有镜头")
		}
		sources := map[int]bool{}
		oldSpeech := []StoryboardUtterance{}
		newSpeech := []StoryboardUtterance{}
		oldTime, newTime := 0.0, 0.0
		for n := edit.StartShot; n <= edit.EndShot; n++ {
			if !allowed[n] {
				return before, errors.New("节奏优化修改了未被指派的镜头")
			}
			shot := before.Shots[n-1]
			oldTime += shot.Duration
			oldSpeech = append(oldSpeech, shot.Utterances...)
			for _, line := range shot.SourceLines {
				sources[line] = true
			}
		}
		coverage := map[int]bool{}
		for _, shot := range edit.Shots {
			newSpeech = append(newSpeech, shot.Utterances...)
			for _, line := range shot.SourceLines {
				if !sources[line] {
					return before, errors.New("替换镜头借用了范围外的剧本行")
				}
				coverage[line] = true
			}
		}
		if len(oldSpeech) != len(newSpeech) {
			return before, errors.New("口播条目不能直接删除；旁白转画面须保留visual条目及原句依据")
		}
		offset := 0
		for j := range edit.Shots {
			for k := range edit.Shots[j].Utterances {
				old := oldSpeech[offset]
				u := &edit.Shots[j].Utterances[k]
				offset++
				if old.Kind != u.Kind || old.Speaker != u.Speaker || old.SourceLine != u.SourceLine {
					return before, errors.New("节奏优化不得更换台词顺序、声音类型、说话者或来源行")
				}
				if old.Text == u.Text && old.Delivery == u.Delivery {
					*u = old
					continue
				}
				if !narrationSpeaker(old) || !narrationSource(lines[old.SourceLine-1], forms) {
					return before, errors.New("角色对白、关键心声及角色画外音必须逐字保留")
				}
				if u.Delivery == "visual" {
					if u.Text != "" {
						return before, errors.New("转画面的旁白不能继续保留口播")
					}
				} else {
					if strings.TrimSpace(u.Text) == "" || len([]rune(u.Text)) >= len([]rune(old.Text)) {
						return before, errors.New("精简旁白须实际缩短并保留非空表达")
					}
				}
				u.Mentions = nil
				u.SpeakerMentions = old.SpeakerMentions
				u.SourceText = originalStoryboardSpeech(old)
				if u.AdaptationReason == "" {
					u.AdaptationReason = "按节奏目标精简解释性旁白或改用画面表达，原句保留供事实复核"
				}
			}
		}
		for j := range edit.Shots {
			shot := &edit.Shots[j]
			shot.DescriptionMentions, shot.SoundMentions = nil, nil
			if edit.StartShot == edit.EndShot && len(edit.Shots) == 1 {
				old := before.Shots[edit.StartShot-1]
				if shot.Description == old.Description {
					shot.DescriptionMentions = old.DescriptionMentions
				}
				if shot.Sound == old.Sound {
					shot.SoundMentions = old.SoundMentions
				}
			}
		}
		// 补时后再计算实际收益，防止一处只差零点几秒就丢弃整份优化稿。
		for j := range edit.Shots {
			if err := repairStoryboardShotTiming(&edit.Shots[j]); err != nil {
				return before, err
			}
			newTime += edit.Shots[j].Duration
		}
		if len(coverage) != len(sources) {
			return before, errors.New("合并或缩短遗漏了原镜头的剧本依据")
		}
		if len(edit.Shots) > edit.EndShot-edit.StartShot+1 || newTime >= oldTime-0.25 || math.IsNaN(newTime) || math.IsInf(newTime, 0) {
			return before, errors.New("局部修改没有实际缩短停留，或增加了镜头数量")
		}
		candidate.Shots = append(candidate.Shots, before.Shots[next-1:edit.StartShot-1]...)
		candidate.Shots = append(candidate.Shots, edit.Shots...)
		next = edit.EndShot + 1
	}
	candidate.Shots = append(candidate.Shots, before.Shots[next-1:]...)
	if err := validateStoryboardSegment(&candidate, lines, forms, true); err != nil {
		return before, err
	}
	return candidate, nil
}
func verifyPacingChecks(report storyboardReviewReport, issues []pacingIssue) error {
	if len(report.Blocking) > 0 {
		return fmt.Errorf("优化稿内容复核未通过：%s", strings.Join(report.Blocking, "；"))
	}
	if len(report.Pacing) > 0 {
		return errors.New("优化稿仍有需要执行的节奏调整")
	}
	expected := map[string]bool{}
	for _, p := range issues {
		expected[p.ID] = true
	}
	seen := map[string]bool{}
	for _, c := range report.PacingChecks {
		if !expected[c.ID] || seen[c.ID] || !c.Resolved || strings.TrimSpace(c.Reason) == "" {
			return errors.New("节奏复核必须逐项确认真实改动和信息保留，不能漏查或仅口头宣布已采纳")
		}
		seen[c.ID] = true
	}
	if len(seen) != len(expected) {
		return errors.New("节奏复核缺少已派工问题的确认")
	}
	return nil
}

// 优化稿单独生成和复核，只有通过后才覆盖候选。两轮仍不通过则记录未采纳、保留已检查的原稿。
func optimizeStoryboardPacing(center *repairCenter, before StoryboardSegment, issues []pacingIssue, lines []string, forms []AssetForm, previous, source, phase string, call func(string, string) (string, error), save func(StoryboardSegment, RepairTicket, RepairResult) error) (StoryboardSegment, error) {
	pc := *center
	defer func() { center.local = pc.local }()
	n, total := len(before.Shots), storyboardSeconds(before)
	pc.scope.BeforeShotCount, pc.scope.BeforeDuration = &n, &total
	issueJSON, _ := json.Marshal(issues)
	ticket, err := pc.route("需执行的节奏调整："+string(issueJSON), "节奏复核", string(issueJSON)+encodedStoryboard(before), "pacing", "只修改已指派的镜头；保留关键事件、动作、原有完整台词和承接状态，允许精简冗余解释性旁白或改用画面；角色对白、心声和关键信息不得删改。")
	if err != nil {
		if !errors.Is(err, ErrRepairLimit) {
			return before, err
		}
		message := "节奏建议未采纳：本轮修正额度已满，保留已复核原稿。"
		if err = center.emit(message); err != nil {
			return before, err
		}
		before.Notes = strings.TrimSpace(before.Notes + "\n" + message)
		return before, nil
	}
	finish := func(status, reason string, actual StoryboardSegment) error {
		count, seconds := len(actual.Shots), storyboardSeconds(actual)
		return pc.finishResult(ticket, RepairResult{Status: status, Outcome: reason, AfterShotCount: &count, AfterDuration: &seconds})
	}
	if len(issues) == 0 {
		return before, finish("declined", "未发现可安全合并或缩短的重复铺垫；保留当前镜头和必要停顿。", before)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err = center.ctx.Err(); err != nil {
			return before, err
		}
		feedback := ""
		if lastErr != nil {
			feedback = "\n上次未采纳的原因：" + lastErr.Error() + "。按原稿重新给出最小局部改动，不要重复相同错误。"
		}
		raw, e := call(phase+"·节奏优化", `你是快节奏漫剧的分镜优化师。只按中心指派合并重复铺垫或缩短无新信息停留，保留关键动作、剧情、角色完整对白、关键心声、声音类型、说话者和每条台词来源行；不把所有无对白镜头都视为拖沓，不牺牲悬念、情绪或必要动作时长。不要新增事实或口播；角色对白与心声逐字保留。解释性旁白默认转为画面，尤其是画面已经能交代的内容；只有画面确实无法交代的关键事实才保留最短口播。普通交代镜头以1～3秒为目标，遇到七八秒的简单特写要按实际动作重新估时。同一人物的观察、反应和紧接的心声按一个叙事节拍处理；能与心声同时呈现的表情和动作，不在心声前再留一轮停顿。必须保留关键事实，不能把核心信息当冗余删掉，不跨场次或更改人物起止状态。30秒等建议只是具体目标，不是通用硬上限。
只返回局部替换JSON：{"edits":[{"startShot":1,"endShot":3,"shots":[{"description":"合并后完整动作","shotSize":"中景","camera":"跟拍","sound":"环境音","duration":4,"sourceLines":[1,2],"utterances":[]}]}]}。startShot/endShot是原稿内从1开始的包含端点镜头编号，范围必须属于指派镜头、不重叠。每处替换都应实际减少停留时间且不增加镜头数；合并必须保留范围内全部剧本行与按原顺序保留的utterances。不能直接删除任何声音条目：冗余旁白可改写为更短text，或保留同一kind/speaker/sourceLine并设置delivery=visual、text为空，将内容明确呈现在description；原句sourceText由程序自动填写。角色对白、关键心声及角色画外音必须逐字原样保留。未修改的镜头不要输出。时长仍须足够演完动作和念完台词。
中心指派：`+string(issueJSON)+"\n原稿："+encodedStoryboard(before)+"\n原稿统计："+storyboardPacingStats(before)+"\n逐镜口播下界："+storyboardSpeechBounds(before)+"\n声音契约："+storyboardVoiceContract+"\n本片段剧本："+source+feedback)
		if e != nil {
			lastErr = e
			continue
		}
		var patch pacingPatch
		if e = decodeStoryboard(raw, &patch); e != nil {
			lastErr = e
			continue
		}
		candidate, e := applyPacingPatch(before, issues, patch, lines, forms)
		if e != nil {
			lastErr = e
			continue
		}
		catalog, _ := json.Marshal(forms)
		raw, e = call(phase+"·节奏复核", `你是分镜审阅师。复核节奏优化后的实际镜头，只依据剧本、原稿与前段状态。检查是否保留关键剧情、可见动作、台词、完整因果与空间承接，是否真正消除了指派的重复铺垫/无信息停留。不能因为无对白就否定关键动作、悬念或情绪停顿；不把30秒或8～15秒当通用硬上限。任何新增事实、改写角色对白或关键心声均不可通过。解释性旁白可精简或转为画面：sourceText为程序保存的真实原句，text为实际口播，delivery=visual则不发声。必须逐项检查原句的关键身份、时间条件、因果和情绪线索是否仍在声音或画面中，不能凭空声称已保留。
返回JSON：{"blocking":[],"suggestions":[],"pacing":[],"pacingChecks":[{"id":"P1","resolved":true,"reason":"实际镜头如何合并/缩短、关键动作如何保留"}]}。逐项回答全部指派ID，尚未真正解决则resolved=false，不能只写已采纳。内容问题放blocking，审美偏好只放suggestions。pacing只有仍需执行的重复铺垫或无信息停留问题，结构沿用kind/action/shots/reason/fix。
指派：`+string(issueJSON)+"\n前段状态："+previous+"\n剧本："+source+"\n优化前："+encodedStoryboard(before)+"\n优化后实际稿："+encodedStoryboard(candidate)+storyboardDescriptionAssetContract+"\n本集真实形象目录："+string(catalog))
		if e != nil {
			lastErr = e
			continue
		}
		report, e := decodeStoryboardReview(raw, candidate)
		if e == nil {
			e = verifyPacingChecks(report, issues)
		}
		if e != nil {
			lastErr = e
			continue
		}
		applyStoryboardDescriptionAssets(&candidate, report.DescriptionAssets, forms)
		candidate.Notes = readableReviewSuggestions(report.Suggestions)
		// 实际镜头与采纳结果原子提交，崩溃/保存失败不会留下“已采纳但未修改”的记录。
		count, seconds := len(candidate.Shots), storyboardSeconds(candidate)
		result := RepairResult{Status: "resolved", Outcome: "局部镜头已实际修改并保存，剧情、关键动作及完整台词复核通过。", AfterShotCount: &count, AfterDuration: &seconds}
		if e = save(candidate, ticket, result); e != nil {
			return before, e
		}
		if e = pc.notifyResult(ticket, result); e != nil {
			return before, e
		}
		return candidate, nil
	}
	if err = center.ctx.Err(); err != nil {
		return before, err
	}
	if errors.Is(lastErr, ErrStoryboardConflict) || errors.Is(lastErr, ErrBatchConflict) {
		return before, lastErr
	}
	return before, finish("declined", fmt.Sprintf("未采纳：两轮优化未通过，已保留原稿。原因：%v", lastErr), before)
}
func encodedStoryboard(s StoryboardSegment) string { b, _ := json.Marshal(s); return string(b) }
