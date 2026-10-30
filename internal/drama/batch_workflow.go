package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type BatchEvent struct {
	ID         int64     `json:"id"`
	Stage      string    `json:"stage"`
	StepNumber int       `json:"stepNumber"`
	Kind       string    `json:"kind"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"createdAt"`
}
type StreamingWriter interface {
	CompleteStream(context.Context, string, string, string, string, func(string) error) (string, error)
}
type EpisodePlan struct {
	FactIDs   []string     `json:"factIds,omitempty"`
	Sources   []SourceFact `json:"sources,omitempty"`
	Title     string       `json:"title"`
	Synopsis  string       `json:"synopsis"`
	Hook      string       `json:"hook"`
	StartUnit int          `json:"startUnit"`
	EndUnit   int          `json:"endUnit"`
}
type sourceUnit struct {
	ID, Start, End int
	Text           string
}
type unitBeat struct {
	ID    int    `json:"id"`
	Event string `json:"event"`
}
type storyDigest struct {
	Units []unitBeat `json:"units"`
}
type episodePlanResult struct {
	Episodes []EpisodePlan `json:"episodes"`
}
type episodeReview struct {
	Passed *bool    `json:"passed"`
	Issues []string `json:"issues"`
}
type workflowTask struct{ run func() error }
type batchWorkflow struct {
	service       *Service
	job           BatchAnalysisJob
	ctx           context.Context
	saved         map[string]BatchStep
	retried       atomic.Bool
	slowThreshold time.Duration
}

var stageLabels = map[string]string{"art": "美术指导", "story": "剧情整理", "plan": "分集规划", "name": "批次命名", "context": "本批上下文", "repair": "局部修正", "director": "导演", "review": "剧本审校"}

type BatchEventPage struct {
	Events []BatchEvent
	Status string
}

func (s *Service) BatchEvents(ctx context.Context, org, owner, project, batch string, after int64) (BatchEventPage, error) {
	if after < 0 {
		return BatchEventPage{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(project); err != nil {
		return BatchEventPage{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(batch); err != nil {
		return BatchEventPage{}, ErrInvalidInput
	}
	current, err := s.repository.GetBatch(ctx, org, owner, project, batch)
	if err != nil {
		return BatchEventPage{}, err
	}
	events, err := s.repository.ListBatchEvents(ctx, org, owner, project, batch, after)
	return BatchEventPage{Events: events, Status: current.Status}, err
}
func (w *batchWorkflow) emit(stage string, number int, kind, message string) error {
	return w.service.repository.AppendBatchEvent(w.ctx, w.job, BatchEvent{Stage: stage, StepNumber: number, Kind: kind, Message: message})
}
func (w *batchWorkflow) runStep(stage string, number, total int, prompt string, validate func(json.RawMessage) error, force bool, normalize ...func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	inputHash := inputFingerprint(w.job, prompt)
	key := fmt.Sprintf("%s:%d", stage, number)
	if old, ok := w.saved[key]; ok && !force && old.Status == "completed" && old.ChunkTotal == total && (w.job.WorkflowVersion < 3 || old.InputHash == inputHash) {
		data, err := parseObject(old.ResultText)
		if err != nil {
			return nil, fmt.Errorf("读取%s已保存结果失败: %w", stageLabels[stage], err)
		}
		if err = validate(data); err != nil {
			return nil, fmt.Errorf("校验%s已保存结果失败: %w", stageLabels[stage], err)
		}
		return data, nil
	}
	step := BatchStep{InputHash: inputHash, Stage: stage, ChunkNumber: number, ChunkTotal: total}
	if err := w.service.repository.StartBatchStep(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID, w.job.Batch.AnalysisRevision, step); err != nil {
		return nil, err
	}
	if err := w.emit(stage, number, "started", fmt.Sprintf("%s开始处理第 %d/%d 项", stageLabels[stage], number, total)); err != nil {
		return nil, err
	}
	started := time.Now()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			w.retried.Store(true)
			if err := w.emit(stage, number, "retry", fmt.Sprintf("%s第 %d 项正在重试：%s", stageLabels[stage], number, lastErr)); err != nil {
				return nil, err
			}
			select {
			case <-w.ctx.Done():
				return nil, w.ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		currentPrompt := prompt
		if attempt > 0 {
			currentPrompt += "\n上次未通过校验，请修正后重新输出完整 JSON：" + lastErr.Error()
		}
		response, err := w.complete(stage, number, currentPrompt)
		if err == nil {
			var data json.RawMessage
			data, err = parseObject(response)
			if err == nil {
				err = validate(data)
			}
			if err == nil && len(normalize) > 0 {
				data, err = normalize[0](data)
			}
			if err == nil {
				step.ResultText = string(data)
				step.DurationMS = time.Since(started).Milliseconds()
				if err = w.service.repository.CompleteBatchStep(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID, w.job.Batch.AnalysisRevision, step); err != nil {
					return nil, err
				}
				if err = w.emit(stage, number, "completed", fmt.Sprintf("%s第 %d/%d 项已保存，用时 %d 秒", stageLabels[stage], number, total, step.DurationMS/1000)); err != nil {
					return nil, err
				}
				return data, nil
			}
		} else if !errors.Is(err, ErrModelTimeout) && !errors.Is(err, ErrEmptyModelContent) && !errors.Is(err, ErrModelTemporary) {
			lastErr = err
			break
		}
		lastErr = err
	}
	reason := fmt.Sprintf("%s第 %d 项失败：%v", stageLabels[stage], number, lastErr)
	if err := w.service.repository.FailBatchStep(w.ctx, w.job, stage, number, reason); err != nil {
		return nil, fmt.Errorf("%s；%w", reason, err)
	}
	if err := w.emit(stage, number, "failed", reason); err != nil {
		return nil, err
	}
	return nil, errors.New(reason)
}
func (w *batchWorkflow) complete(stage string, number int, prompt string) (string, error) {
	streaming, ok := w.service.writer.(StreamingWriter)
	if !ok {
		return w.service.writer.Complete(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.ModelCode, prompt)
	}
	lastText := ""
	lastReport := time.Now()
	received := 0
	response, err := streaming.CompleteStream(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.ModelCode, prompt, func(output string) error {
		received = utf8.RuneCountInString(output)
		if stage == "director" {
			text := partialScriptText(output)
			if strings.HasPrefix(text, lastText) && len(text) > len(lastText) && (len(text)-len(lastText) >= 300 || time.Since(lastReport) > time.Second) {
				delta := text[len(lastText):]
				lastText = text
				lastReport = time.Now()
				return w.emit(stage, number, "delta", delta)
			}
		}
		if time.Since(lastReport) > 5*time.Second {
			lastReport = time.Now()
			return w.emit(stage, number, "receiving", fmt.Sprintf("%s正在生成，已收到 %d 字符", stageLabels[stage], received))
		}
		return nil
	})
	if err == nil && stage == "director" {
		finalText := partialScriptText(response)
		if strings.HasPrefix(finalText, lastText) && len(finalText) > len(lastText) {
			if eventErr := w.emit(stage, number, "delta", finalText[len(lastText):]); eventErr != nil {
				return "", eventErr
			}
		}
	}
	return response, err
}

// 只抽取模型已生成的剧本正文字段，供临时预览；最终结果仍须完整 JSON 校验。
func partialScriptText(output string) string {
	i := strings.Index(output, `"scriptText"`)
	if i < 0 {
		return ""
	}
	tail := output[i+len(`"scriptText"`):]
	colon := strings.IndexByte(tail, ':')
	if colon < 0 {
		return ""
	}
	tail = strings.TrimSpace(tail[colon+1:])
	if len(tail) == 0 || tail[0] != '"' {
		return ""
	}
	escaped := false
	end := len(tail)
	for i := 1; i < len(tail); i++ {
		if tail[i] == '"' && !escaped {
			end = i + 1
			break
		}
		if tail[i] == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
	}
	raw := tail[:end]
	if end == len(tail) && (len(raw) < 2 || raw[len(raw)-1] != '"') {
		raw += `"`
	}
	for n := 0; n < 8 && len(raw) > 1; n++ {
		if value, err := strconv.Unquote(raw); err == nil {
			return value
		}
		raw = raw[:len(raw)-2] + `"`
	}
	return ""
}
func (w *batchWorkflow) schedule(tasks []workflowTask) error {
	width := 1
	threshold := w.slowThreshold
	if threshold <= 0 {
		threshold = 45 * time.Second
	}
	for offset := 0; offset < len(tasks); {
		count := min(width, len(tasks)-offset)
		errs := make([]error, count)
		durations := make([]time.Duration, count)
		var group sync.WaitGroup
		for i := 0; i < count; i++ {
			group.Add(1)
			go func(i int) {
				defer group.Done()
				start := time.Now()
				errs[i] = tasks[offset+i].run()
				durations[i] = time.Since(start)
			}(i)
		}
		group.Wait()
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
		offset += count
		next := width
		if w.retried.Swap(false) {
			next = 1
		} else {
			for _, d := range durations {
				if d >= threshold {
					next = 2
				}
			}
		}
		if next != width && offset < len(tasks) {
			width = next
			if err := w.emit("scheduler", 0, "schedule", fmt.Sprintf("根据已完成请求的耗时和重试情况，后续独立任务并行数调整为 %d", width)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) analyzeWorkflowV2(job BatchAnalysisJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()
	source, err := s.repository.GetSourceRange(ctx, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.StartOffset, job.Batch.EndOffset-job.Batch.StartOffset)
	if err != nil {
		return s.failBatch(job, err)
	}
	if utf8.RuneCountInString(source.Text) != job.Batch.EndOffset-job.Batch.StartOffset {
		return s.failBatch(job, errors.New("批次原文读取不完整"))
	}
	units := makeSourceUnits(source.Text)
	groups := groupSourceUnits(units)
	saved, err := s.repository.ListBatchSteps(ctx, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID)
	if err != nil {
		return s.failBatch(job, err)
	}
	w := &batchWorkflow{service: s, job: job, ctx: ctx, saved: map[string]BatchStep{}}
	for _, step := range saved {
		w.saved[fmt.Sprintf("%s:%d", step.Stage, step.ChunkNumber)] = step
	}
	arts := make([]BatchDraft, len(groups))
	stories := make([]storyDigest, len(groups))
	tasks := make([]workflowTask, 0, len(groups)*2)
	for i, group := range groups {
		i, group := i, group
		tasks = append(tasks, workflowTask{run: func() error {
			var art BatchDraft
			prompt := fmt.Sprintf(artWorkflowPrompt, job.ProjectTitle, string(job.Overview), sourceUnitsText(group))
			data, err := w.runStep("art", i+1, len(groups), prompt, func(raw json.RawMessage) error {
				if err := json.Unmarshal(raw, &art); err != nil {
					return err
				}
				if art.Characters == nil || art.Scenes == nil || art.Props == nil {
					return errors.New("角色、场景、道具必须明确返回数组，不能为 null")
				}
				return validateArtDraft(raw, art)
			}, false)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &arts[i]); err != nil {
				return err
			}
			names := []string{}
			for _, list := range [][]AssetDraft{arts[i].Characters, arts[i].Scenes, arts[i].Props} {
				for _, a := range list {
					names = append(names, a.Name)
				}
			}
			if len(names) > 0 && w.saved[fmt.Sprintf("art:%d", i+1)].Status != "completed" {
				return w.emit("art", i+1, "assets", "已整理资产："+strings.Join(names, "、"))
			}
			return nil
		}}, workflowTask{run: func() error {
			var story storyDigest
			data, err := w.runStep("story", i+1, len(groups), fmt.Sprintf(storyWorkflowPrompt, string(job.Overview), sourceUnitsText(group)), func(raw json.RawMessage) error {
				story = storyDigest{}
				if err := json.Unmarshal(raw, &story); err != nil {
					return err
				}
				if len(story.Units) != len(group) {
					return errors.New("剧情单元数量与原文不一致")
				}
				for n, u := range group {
					if story.Units[n].ID != u.ID || strings.TrimSpace(story.Units[n].Event) == "" || utf8.RuneCountInString(story.Units[n].Event) > 500 {
						return errors.New("剧情单元编号或事件摘要无效")
					}
				}
				return nil
			}, false)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, &stories[i])
		}})
	}
	if err = w.schedule(tasks); err != nil {
		return s.failBatch(job, err)
	}
	assets := mergeArtDrafts(arts)
	allBeats := []unitBeat{}
	for _, d := range stories {
		allBeats = append(allBeats, d.Units...)
	}
	beatJSON, _ := json.Marshal(allBeats)
	var plan episodePlanResult
	raw, err := w.runStep("plan", 1, 1, fmt.Sprintf(planWorkflowPrompt, string(job.Overview), string(beatJSON), unitLengths(units)), func(raw json.RawMessage) error {
		plan = episodePlanResult{}
		if err := json.Unmarshal(raw, &plan); err != nil {
			return err
		}
		return validateEpisodePlan(plan.Episodes, units)
	}, false)
	if err != nil {
		return s.failBatch(job, err)
	}
	if err = json.Unmarshal(raw, &plan); err != nil {
		return s.failBatch(job, err)
	}
	if err = w.nameBatch(string(raw)); err != nil {
		return s.failBatch(job, err)
	}
	catalog, _ := json.Marshal(assets)
	result := assets
	result.Episodes = []EpisodeDraft{}
	for i, p := range plan.Episodes {
		original := string([]rune(source.Text)[units[p.StartUnit-1].Start:units[p.EndUnit-1].End])
		pJSON, _ := json.Marshal(p)
		previous := ""
		if i > 0 {
			previous = result.Episodes[i-1].Synopsis
		}
		base := fmt.Sprintf(directorWorkflowPrompt, job.ProjectTitle, string(job.Overview), string(pJSON), previous, string(catalog), original)
		var episode EpisodeDraft
		generate := func(force bool, feedback string) error {
			raw, err := w.runStep("director", i+1, len(plan.Episodes), base+feedback, func(raw json.RawMessage) error {
				var directed struct {
					Episodes []EpisodeDraft `json:"episodes"`
				}
				if err := json.Unmarshal(raw, &directed); err != nil {
					return err
				}
				if len(directed.Episodes) != 1 {
					return errors.New("本步骤必须只生成规划中的一集")
				}
				episode = directed.Episodes[0]
				if episode.Title != p.Title {
					return errors.New("本集标题必须与分集规划一致")
				}
				return validateEpisodeDraft(episode, assets)
			}, force)
			if err != nil {
				return err
			}
			var directed struct {
				Episodes []EpisodeDraft `json:"episodes"`
			}
			if err = json.Unmarshal(raw, &directed); err != nil {
				return err
			}
			episode = directed.Episodes[0]
			return nil
		}
		if err = generate(false, ""); err != nil {
			return s.failBatch(job, err)
		}
		approved := false
		for reviewAttempt := 0; reviewAttempt < 2; reviewAttempt++ {
			var review episodeReview
			episodeJSON, _ := json.Marshal(episode)
			_, err = w.runStep("review", i+1, len(plan.Episodes), fmt.Sprintf(reviewWorkflowPrompt, string(pJSON), previous, original, string(episodeJSON)), func(raw json.RawMessage) error {
				review = episodeReview{}
				if err := json.Unmarshal(raw, &review); err != nil {
					return err
				}
				if review.Passed == nil || review.Issues == nil {
					return errors.New("审校必须返回 passed 和 issues")
				}
				if !*review.Passed && len(review.Issues) == 0 {
					return errors.New("审校未通过时必须列出具体问题")
				}
				if *review.Passed && len(review.Issues) > 0 {
					return errors.New("审校结论与问题列表矛盾")
				}
				return nil
			}, reviewAttempt > 0)
			if err != nil {
				return s.failBatch(job, err)
			}
			if *review.Passed {
				approved = true
				break
			}
			if err = s.repository.FailBatchStep(ctx, job, "review", i+1, strings.Join(review.Issues, "；")); err != nil {
				return s.failBatch(job, err)
			}
			feedback := "\n审校发现以下问题，请仅修正本集，不改变原文事实：" + strings.Join(review.Issues, "；")
			if reviewAttempt == 0 {
				if err = w.emit("review", i+1, "repair", "审校要求修正本集："+strings.Join(review.Issues, "；")); err != nil {
					return s.failBatch(job, err)
				}
				if err = generate(true, feedback); err != nil {
					return s.failBatch(job, err)
				}
			} else {
				if err = s.repository.FailBatchStep(ctx, job, "review", i+1, feedback); err != nil {
					return s.failBatch(job, err)
				}
				return s.failBatch(job, fmt.Errorf("第 %d 集审校仍有问题：%s", i+1, strings.Join(review.Issues, "；")))
			}
		}
		if !approved {
			return s.failBatch(job, errors.New("剧本尚未通过审校"))
		}
		for _, old := range result.Episodes {
			if strings.TrimSpace(old.ScriptText) == strings.TrimSpace(episode.ScriptText) {
				if err = s.repository.FailBatchStep(ctx, job, "director", i+1, "与已完成剧集正文重复"); err != nil {
					return s.failBatch(job, err)
				}
				if err = s.repository.FailBatchStep(ctx, job, "review", i+1, "需重新检查重复剧本"); err != nil {
					return s.failBatch(job, err)
				}
				return s.failBatch(job, errors.New("不同剧集出现完全重复的正文"))
			}
		}
		result.Episodes = append(result.Episodes, episode)
	}
	if err = s.repository.SaveBatchResult(ctx, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID, job.Batch.AnalysisRevision, result); err != nil {
		return s.failBatch(job, err)
	}
	return w.emit("workflow", 0, "completed", fmt.Sprintf("本批 %d 集已通过审校并保存", len(result.Episodes)))
}

func makeSourceUnits(text string) []sourceUnit {
	runes := []rune(text)
	units := []sourceUnit{}
	for start := 0; start < len(runes); {
		end := min(start+1000, len(runes))
		boundary := 0
		if end < len(runes) {
			for i := start + 1; i < end; i++ {
				if runes[i-1] == '\n' {
					lineEnd := i
					for lineEnd < end && runes[lineEnd] != '\n' {
						lineEnd++
					}
					heading := normalizedHeading(string(runes[i:lineEnd]))
					if episodeHeading.MatchString(heading) || chapterHeading.MatchString(heading) {
						boundary = i
						break
					}
					if i-start >= 400 {
						boundary = i
					}
				}
			}
			if boundary > start {
				end = boundary
			}
		}
		units = append(units, sourceUnit{ID: len(units) + 1, Start: start, End: end, Text: string(runes[start:end])})
		start = end
	}
	return units
}
func groupSourceUnits(units []sourceUnit) [][]sourceUnit {
	groups := [][]sourceUnit{}
	start, length := 0, 0
	for i, u := range units {
		if i > start && length+u.End-u.Start > BatchAnalysisChunkLimit {
			groups = append(groups, units[start:i])
			start = i
			length = 0
		}
		length += u.End - u.Start
	}
	if start < len(units) {
		groups = append(groups, units[start:])
	}
	return groups
}
func sourceUnitsText(units []sourceUnit) string {
	var b strings.Builder
	for _, u := range units {
		fmt.Fprintf(&b, "\n【原文单元 %d】\n%s", u.ID, u.Text)
	}
	return b.String()
}
func unitLengths(units []sourceUnit) string {
	var b strings.Builder
	for _, u := range units {
		fmt.Fprintf(&b, "%d:%d字符", u.ID, u.End-u.Start)
		heading := normalizedHeading(strings.SplitN(u.Text, "\n", 2)[0])
		if episodeHeading.MatchString(heading) {
			fmt.Fprintf(&b, "，原文集界：%s", heading)
		}
		b.WriteString("；")
	}
	return b.String()
}
func validateEpisodePlan(plans []EpisodePlan, units []sourceUnit) error {
	if len(plans) == 0 || len(plans) > 120 {
		return errors.New("分集规划数量必须为 1 至 120 集")
	}
	next := 1
	for _, p := range plans {
		if strings.TrimSpace(p.Title) == "" || utf8.RuneCountInString(p.Title) > 120 || strings.TrimSpace(p.Synopsis) == "" || p.StartUnit != next || p.EndUnit < p.StartUnit || p.EndUnit > len(units) {
			return errors.New("分集范围必须按顺序完整覆盖原文，不得遗漏、重叠或越界")
		}
		for _, u := range units[p.StartUnit:p.EndUnit] {
			if episodeHeading.MatchString(normalizedHeading(strings.SplitN(u.Text, "\n", 2)[0])) {
				return errors.New("分集规划不得跨越原文明确的分集标题，请在原集界处切分")
			}
		}
		if units[p.EndUnit-1].End-units[p.StartUnit-1].Start > 16000 {
			return errors.New("单集覆盖原文超过 16000 字符，请按剧情转折拆成更多集")
		}
		next = p.EndUnit + 1
	}
	if next != len(units)+1 {
		return errors.New("分集规划没有覆盖本批全部原文")
	}
	return nil
}
func mergeArtDrafts(parts []BatchDraft) BatchDraft {
	out := BatchDraft{Characters: []AssetDraft{}, Scenes: []AssetDraft{}, Props: []AssetDraft{}, Episodes: []EpisodeDraft{}}
	for _, kind := range []string{"character", "scene", "prop"} {
		items := []AssetDraft{}
		positions := map[string]int{}
		for _, part := range parts {
			list := part.Characters
			if kind == "scene" {
				list = part.Scenes
			} else if kind == "prop" {
				list = part.Props
			}
			for _, a := range list {
				a.Name = strings.TrimSpace(a.Name)
				if i, ok := positions[a.Name]; ok {
					old := &items[i]
					old.Description = mergeAssetText(old.Description, a.Description)
					old.AppearanceDescription = mergeAssetText(old.AppearanceDescription, a.AppearanceDescription)
					old.VoiceDescription = mergeAssetText(old.VoiceDescription, a.VoiceDescription)
				} else {
					positions[a.Name] = len(items)
					items = append(items, a)
				}
			}
		}
		switch kind {
		case "character":
			out.Characters = items
		case "scene":
			out.Scenes = items
		case "prop":
			out.Props = items
		}
	}
	return out
}
func mergeAssetText(old, new string) string {
	if new == "" || strings.Contains(old, new) {
		return old
	}
	if old == "" {
		return new
	}
	return old + "；" + new
}
func validateEpisodeDraft(e EpisodeDraft, assets BatchDraft) error {
	if e.Characters == nil || e.Scenes == nil || e.Props == nil {
		return errors.New("本集必须明确返回角色、场景、道具数组")
	}
	if strings.TrimSpace(e.Title) == "" || utf8.RuneCountInString(e.Title) > 120 || strings.TrimSpace(e.ScriptText) == "" || strings.TrimSpace(e.Synopsis) == "" {
		return errors.New("本集缺少有效标题、梗概或完整剧本")
	}
	for _, group := range []struct {
		assets []AssetDraft
		refs   []EpisodeAssetDraft
	}{{assets.Characters, e.Characters}, {assets.Scenes, e.Scenes}, {assets.Props, e.Props}} {
		names := map[string]bool{}
		for _, a := range group.assets {
			names[a.Name] = true
		}
		for _, ref := range group.refs {
			if !names[ref.Name] || utf8.RuneCountInString(ref.StateLabel) > 120 {
				return fmt.Errorf("本集引用了资产清单之外的名称或无效状态：%s", ref.Name)
			}
		}
	}
	return nil
}

const artWorkflowPrompt = `你是漫剧美术指导。根据原文提取角色、场景和关键道具，供后续绘制与配音保持一致。只依据原文证据，缺少明确线索的外貌或音色填空字符串。不要把性格猜成音色，不编造原文没有的物件。
返回 JSON：{"characters":[{"name":"名称","description":"身份、性格、人物关系","appearanceDescription":"有依据的年龄、面貌、身形、发型、服装","voiceDescription":"有依据的音色、语气、说话习惯"}],"scenes":[{"name":"名称","description":"地点、时代、光线、环境"}],"props":[{"name":"名称","description":"外观和剧情用途"}]}。名称简短一致，仅收录对剧情和制作有用的资产，不生成剧集。
作品：%s
项目概览：%s
原文：%s`
const storyWorkflowPrompt = `你是剧情统筹。按原文单元顺序提取已经发生的事件、因果、冲突、人物状态变化及场景转换，为整批分集提供依据。每个单元写一条不超过 200 字的摘要；标题或空白单元说明其用途。不得虚构，不写剧本，不提前决定集数。
返回 JSON：{"units":[{"id":原文单元编号,"event":"事件摘要"}]}，必须覆盖输入的全部单元，编号和顺序完全一致。
项目概览：%s
原文：%s`
const planWorkflowPrompt = `你是漫剧总导演。依据整批按原文顺序排列的事件，先规划整批剧集。原文单元只是阅读索引，不等于剧集；允许合并多个单元，围绕人物目标、冲突升级、信息变化和悬念切集。不要机械地每段一集。原文明确有第N集时尊重原集界，没有时按完整剧情弧拆分。短剧开场尽快建立目标或悬念，每集推进事件，结尾以已有转折、悬念或行动形成钩子，不编造反转。
返回 JSON：{"episodes":[{"title":"标题","synopsis":"本集事件与因果","hook":"原文有依据的结尾钩子","startUnit":起始单元编号,"endUnit":结束单元编号}]}。起止都包含，第一集从 1 开始，下一集从上一集 endUnit+1 开始，最后覆盖全部单元，不得遗漏、重叠或调序。每集最多覆盖 16000 字符。集数按剧情需要确定，不设固定集数。
项目概览：%s
整批剧情：%s
各单元长度：%s`
const directorWorkflowPrompt = `你是漫剧导演兼短剧编剧。按已经确定的本集规划，将分配给本集的原文改编为完整中文拍摄剧本。保留重要事件、因果与人物状态，不重复上一集、不挪用后续剧情、不增加人物或结局。每场有【地点 内/外 日/夜】标题、可视化动作与对白，减少无法拍摄的心理叙述。开场迅速建立目标或悬念，场景推动冲突，按规划落实结尾钩子；不得为了节奏虚构事实。
返回 JSON：{"episodes":[{"title":"本集标题","synopsis":"本集梗概","scriptText":"完整剧本正文","characters":[{"name":"资产名称","stateLabel":"本集形象状态"}],"scenes":[{"name":"资产名称","stateLabel":"本集时段状态"}],"props":[{"name":"资产名称","stateLabel":"状态"}]}]}，episodes 必须只有本集一项，名称只能从美术资产清单选择，无法判断的状态填空字符串。正文不能用摘要代替。
作品：%s
项目概览：%s
本集规划：%s
上一集梗概：%s
美术资产清单：%s
本集完整原文：%s`
const reviewWorkflowPrompt = `你是剧本审校。逐项核对剧本与本集原文、分集规划和上一集衔接：是否遗漏关键事件和因果，是否虚构人物、场景、道具或结局，人物身份状态是否前后一致，是否把剧本写成摘要，场景动作对白是否可拍摄。允许压缩重复叙述、调整表达，不能改变事实。只列明确问题，不能凭审美偏好反复改写。
返回 JSON：{"passed":true或false,"issues":["具体问题与原文依据"]}。通过时 issues 必须为空数组；未通过时明确列问题。
本集规划：%s
上一集梗概：%s
本集原文：%s
待审剧本：%s`

func (w *batchWorkflow) nameBatch(material string) error {
	if w.job.Batch.NameManuallyEdited {
		return nil
	}
	var result struct {
		Name string `json:"name"`
	}
	_, err := w.runStep("name", 1, 1, fmt.Sprintf(`你是漫剧编辑。请根据下方当前批次的全部分集规划，给这一批剧情起一个简短、具体的中文名称。突出本批核心事件或剧情弧，不用作品总名，不写“第N批”，不添加说明、引号或 Markdown，建议4至12个汉字，不得超过80字符。只返回 JSON：{"name":"批次名称"}。
作品：%s
本批分集规划：%s`, w.job.ProjectTitle, material), func(raw json.RawMessage) error {
		result.Name = ""
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		result.Name = strings.TrimSpace(result.Name)
		if result.Name == "" || utf8.RuneCountInString(result.Name) > 80 {
			return errors.New("批次名称不能为空，且不得超过80字符")
		}
		return nil
	}, false)
	if err != nil {
		return err
	}
	if err = w.service.repository.SaveGeneratedBatchName(w.ctx, w.job, result.Name); err != nil {
		return err
	}
	current, err := w.service.repository.GetBatch(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID)
	if err != nil {
		return err
	}
	return w.emit("name", 1, "named", "当前批次名称："+current.Name)
}
