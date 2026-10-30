package drama

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const BatchPrecedingRuneLimit = 6000

type SourceFact struct {
	ID          string `json:"id"`
	UnitID      int    `json:"unitId"`
	Claim       string `json:"claim"`
	Quote       string `json:"quote"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
}
type GroundedContext struct {
	StartOffset    int    `json:"startOffset"`
	EndOffset      int    `json:"endOffset"`
	PrecedingStart int    `json:"precedingStart"`
	PrecedingEnd   int    `json:"precedingEnd"`
	Summary        string `json:"summary"`
	Background     string `json:"background"`
	Setting        string `json:"setting"`
	Continuity     string `json:"continuity"`
}
type evidenceUnit struct {
	ID    int          `json:"id"`
	Event string       `json:"event"`
	Facts []SourceFact `json:"facts"`
}
type evidenceDigest struct {
	Units []evidenceUnit `json:"units"`
}

func formatOnly(overview json.RawMessage) json.RawMessage {
	var original map[string]json.RawMessage
	if json.Unmarshal(overview, &original) != nil {
		return json.RawMessage(`{}`)
	}
	selected := map[string]json.RawMessage{}
	for _, key := range []string{"videoStyle", "aspectRatio"} {
		if value, ok := original[key]; ok {
			selected[key] = value
		}
	}
	out, _ := json.Marshal(selected)
	return out
}
func groundedEvidence(raw json.RawMessage, units []sourceUnit, base int) (json.RawMessage, error) {
	var digest evidenceDigest
	if err := json.Unmarshal(raw, &digest); err != nil {
		return nil, err
	}
	if len(digest.Units) != len(units) {
		return nil, errors.New("证据单元数量与原文不一致")
	}
	for i, unit := range units {
		beat := &digest.Units[i]
		if beat.ID != unit.ID || strings.TrimSpace(beat.Event) == "" || utf8.RuneCountInString(beat.Event) > 500 || len(beat.Facts) == 0 || len(beat.Facts) > 6 {
			return nil, errors.New("每个单元必须按原顺序返回摘要及1至6条原文证据")
		}
		for j := range beat.Facts {
			f := &beat.Facts[j]
			index := strings.Index(unit.Text, f.Quote)
			if strings.TrimSpace(f.Claim) == "" || utf8.RuneCountInString(f.Claim) > 240 || strings.TrimSpace(f.Quote) == "" || utf8.RuneCountInString(f.Quote) > 300 || index < 0 {
				return nil, fmt.Errorf("单元%d的证据必须逐字引用本单元原文，不能补写事实", unit.ID)
			}
			f.ID = fmt.Sprintf("U%dF%d", unit.ID, j+1)
			f.UnitID = unit.ID
			f.StartOffset = base + unit.Start + utf8.RuneCountInString(unit.Text[:index])
			f.EndOffset = f.StartOffset + utf8.RuneCountInString(f.Quote)
		}
	}
	return json.Marshal(digest)
}
func groundPlan(raw json.RawMessage, units []sourceUnit, facts map[string]SourceFact) (json.RawMessage, error) {
	var result episodePlanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	for i := range result.Episodes {
		p := &result.Episodes[i]
		seen := map[string]bool{}
		claims := []string{}
		p.Sources = []SourceFact{}
		if len(p.FactIDs) == 0 {
			return nil, fmt.Errorf("第%d集没有引用原文事实", i+1)
		}
		for _, id := range p.FactIDs {
			f, ok := facts[id]
			if !ok || f.UnitID < p.StartUnit || f.UnitID > p.EndUnit || seen[id] {
				return nil, fmt.Errorf("第%d集引用了不存在、重复或范围外的事实：%s", i+1, id)
			}
			seen[id] = true
			claims = append(claims, f.Claim)
			p.Sources = append(p.Sources, f)
		}
		for id, f := range facts {
			if f.UnitID >= p.StartUnit && f.UnitID <= p.EndUnit && !seen[id] {
				return nil, fmt.Errorf("第%d集遗漏范围内的关键事实：%s", i+1, id)
			}
		}
		// 梗概和钩子的事实材料由范围内证据构成，舍弃总规划自由编写的梗概。
		p.Synopsis = strings.Join(claims, "；")
		p.Hook = claims[len(claims)-1]
	}
	if err := validateEpisodePlan(result.Episodes, units); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func inputFingerprint(job BatchAnalysisJob, prompt string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d\n%s\n%s\n%s", job.WorkflowVersion, job.Batch.AnalysisRunID, job.ModelCode, prompt))))
}
func namesOnly(assets BatchDraft) BatchDraft {
	out := BatchDraft{Characters: []AssetDraft{}, Scenes: []AssetDraft{}, Props: []AssetDraft{}}
	for _, group := range []struct {
		from []AssetDraft
		to   *[]AssetDraft
	}{{assets.Characters, &out.Characters}, {assets.Scenes, &out.Scenes}, {assets.Props, &out.Props}} {
		for _, asset := range group.from {
			*group.to = append(*group.to, AssetDraft{Name: asset.Name})
		}
	}
	return out
}
func validateGroundedEpisode(e EpisodeDraft, assets BatchDraft, original string) error {
	if err := validateEpisodeDraft(e, assets); err != nil {
		return err
	}
	for _, refs := range [][]EpisodeAssetDraft{e.Characters, e.Scenes, e.Props} {
		for _, ref := range refs {
			if strings.TrimSpace(ref.SourceQuote) == "" || !strings.Contains(original, ref.SourceQuote) {
				return fmt.Errorf("资产%s缺少本集原文中可核对的引用依据", ref.Name)
			}
		}
	}
	return nil
}
func (s *Service) analyzeGroundedWorkflow(job BatchAnalysisJob) error {
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
	stories := make([]evidenceDigest, len(groups))
	tasks := []workflowTask{}
	format := formatOnly(job.Overview)
	for i, group := range groups {
		i, group := i, group
		tasks = append(tasks, workflowTask{run: func() error {
			var art BatchDraft
			raw, err := w.runStep("art", i+1, len(groups), fmt.Sprintf(groundedArtPrompt, string(format), sourceUnitsText(group)), func(raw json.RawMessage) error {
				if err := json.Unmarshal(raw, &art); err != nil {
					return err
				}
				if art.Characters == nil || art.Scenes == nil || art.Props == nil {
					return errors.New("资产数组不能为null")
				}
				return validateArtDraft(raw, art)
			}, false)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, &arts[i])
		}}, workflowTask{run: func() error {
			normalize := func(raw json.RawMessage) (json.RawMessage, error) {
				return groundedEvidence(raw, group, job.Batch.StartOffset)
			}
			raw, err := w.runStep("story", i+1, len(groups), fmt.Sprintf(groundedStoryPrompt, sourceUnitsText(group)), func(raw json.RawMessage) error { _, err := normalize(raw); return err }, false, normalize)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, &stories[i])
		}})
	}
	if err = w.schedule(tasks); err != nil {
		return s.failBatch(job, err)
	}
	assets := mergeArtDrafts(arts)
	facts := map[string]SourceFact{}
	compact := []map[string]any{}
	events := []string{}
	for _, story := range stories {
		for _, unit := range story.Units {
			events = append(events, unit.Event)
			for _, f := range unit.Facts {
				facts[f.ID] = f
				compact = append(compact, map[string]any{"id": f.ID, "unitId": f.UnitID, "claim": f.Claim})
			}
		}
	}
	preceding := ""
	precedingStart := max(0, job.Batch.StartOffset-BatchPrecedingRuneLimit)
	if precedingStart < job.Batch.StartOffset {
		prior, err := s.repository.GetSourceRange(ctx, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, precedingStart, job.Batch.StartOffset-precedingStart)
		if err != nil {
			return s.failBatch(job, err)
		}
		preceding = prior.Text
		if utf8.RuneCountInString(preceding) != job.Batch.StartOffset-precedingStart {
			return s.failBatch(job, errors.New("承接原文读取不完整"))
		}
	}
	var local GroundedContext
	normalizeContext := func(raw json.RawMessage) (json.RawMessage, error) {
		if err := json.Unmarshal(raw, &local); err != nil {
			return nil, err
		}
		if strings.TrimSpace(local.Summary) == "" {
			return nil, errors.New("本批上下文缺少剧情摘要")
		}
		local.StartOffset = job.Batch.StartOffset
		local.EndOffset = job.Batch.EndOffset
		local.PrecedingStart = precedingStart
		local.PrecedingEnd = job.Batch.StartOffset
		return json.Marshal(local)
	}
	contextRaw, err := w.runStep("context", 1, 1, fmt.Sprintf(groundedContextPrompt, strings.Join(events, "\n"), preceding), func(raw json.RawMessage) error { _, err := normalizeContext(raw); return err }, false, normalizeContext)
	if err != nil {
		return s.failBatch(job, err)
	}
	if err = json.Unmarshal(contextRaw, &local); err != nil {
		return s.failBatch(job, err)
	}
	compactJSON, _ := json.Marshal(compact)
	normalizePlan := func(raw json.RawMessage) (json.RawMessage, error) { return groundPlan(raw, units, facts) }
	planRaw, err := w.runStep("plan", 1, 1, fmt.Sprintf(groundedPlanPrompt, string(compactJSON), unitLengths(units)), func(raw json.RawMessage) error { _, err := normalizePlan(raw); return err }, false, normalizePlan)
	if err != nil {
		return s.failBatch(job, err)
	}
	var plan episodePlanResult
	if err = json.Unmarshal(planRaw, &plan); err != nil {
		return s.failBatch(job, err)
	}
	if err = w.nameBatch(string(planRaw)); err != nil {
		return s.failBatch(job, err)
	}
	catalog, _ := json.Marshal(namesOnly(assets))
	result := assets
	result.Episodes = []EpisodeDraft{}
	for i, p := range plan.Episodes {
		original := string([]rune(source.Text)[units[p.StartUnit-1].Start:units[p.EndUnit-1].End])
		pJSON, _ := json.Marshal(p)
		previous := preceding
		if i > 0 {
			previous = result.Episodes[i-1].Synopsis
		}
		// 全批摘要、初始项目背景和全批资产描述都不进入本集生成，避免后文信息倒灌。
		base := fmt.Sprintf(groundedDirectorPrompt, string(format), string(pJSON), previous, string(catalog), original)
		episode, err := w.writeReviewedEpisode(i+1, len(plan.Episodes), p, assets, original, job.Batch.StartOffset+units[p.StartUnit-1].Start, base, previous)
		if err != nil {
			return s.failBatch(job, err)
		}
		for _, old := range result.Episodes {
			if strings.TrimSpace(old.ScriptText) == strings.TrimSpace(episode.ScriptText) {
				return s.failBatch(job, errors.New("不同剧集出现完全重复的正文"))
			}
		}
		result.Episodes = append(result.Episodes, episode)
	}
	if err = s.repository.SaveBatchResult(ctx, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID, job.Batch.AnalysisRevision, result); err != nil {
		return s.failBatch(job, err)
	}
	return w.emit("workflow", 0, "completed", fmt.Sprintf("本批%d集已按原文证据审校并保存", len(result.Episodes)))
}

const groundedArtPrompt = `你是漫剧美术指导。只依据当前输入原文提取角色、场景、道具。禁止借用作品其他章节知识。缺少外貌或音色依据时填空字符串。
返回JSON：{"characters":[{"name":"简短名称","description":"有原文依据的身份与关系","appearanceDescription":"外貌依据","voiceDescription":"音色依据"}],"scenes":[{"name":"名称","description":"环境"}],"props":[{"name":"名称","description":"外观用途"}]}。
创作格式（不提供剧情事实）：%s
当前原文：%s`
const groundedStoryPrompt = `你是原文事实整理员。按每个单元提取事件与关键因果，禁止引用其他单元或作品常识。返回JSON：{"units":[{"id":原单元编号,"event":"不超过200字摘要","facts":[{"claim":"不超过240字的具体事实","quote":"从当前单元逐字复制的连续原文，不超过300字"}]}]}。每单元1至6条关键证据，必须完整覆盖输入单元及顺序。标题单元也可引用标题，不能编造引文。引文保留原文字词及标点。
原文单元：%s`
const groundedContextPrompt = `只依据本批事件整理本批上下文，不沿用小说开头的概览，不推测未读章节。上下文供本批概览查阅，不能改变单集的事实范围。
返回JSON：{"summary":"本批剧情概况","background":"本批可确认的背景","setting":"本批可确认的设定","continuity":"仅依据起点前原文说明承接；没有依据则明确未知"}。
本批事件：%s
起点前原文（仅承接，不纳入改编范围；不代表此前全部剧情）：%s`
const groundedPlanPrompt = `你是漫剧总导演。为整批规划连续剧集，围绕完整事件弧切集。事实清单包含阅读索引，不等于每单元一集。
返回JSON：{"episodes":[{"title":"简短标题","startUnit":起点,"endUnit":终点,"factIds":["U1F1"]}]}。第一集从1开始，下集从上一集endUnit+1开始，直到全部单元，不遗漏重叠调序，每集最多16000字。每集factIds必须覆盖该范围的全部关键事实，不能引用范围外ID。标题只能依据本集事实，不自由编写梗概和钩子，程序按引用事实构成梗概。原文明确第N集时尊重集界。
整批事实：%s
单元长度：%s`
const groundedDirectorPrompt = `你是漫剧导演兼短剧编剧。按本集规划和本集完整原文生成可拍摄剧本。只允许压缩重复叙述、把心理转化为原文有依据的动作；保留关键身份、时间条件和因果。不能把承接信息再演一遍，也不能借助其他章节知识提前揭示事实。规划中的sources给出原文依据，不能加入范围外的事实。资产目录仅提供统一名称，不证明资产或状态在本集出现。
返回JSON：{"episodes":[{"title":"必须与规划一致","synopsis":"本集梗概","scriptText":"完整中文剧本，每场【地点 内/外 日/夜】及动作对白","characters":[{"name":"目录名称","stateLabel":"本集状态","sourceQuote":"从本集原文逐字复制的一处角色出场依据"}],"scenes":[{"name":"目录名称","stateLabel":"时段状态","sourceQuote":"本集原文场景依据"}],"props":[{"name":"目录名称","stateLabel":"状态","sourceQuote":"本集原文道具依据"}]}]}。只生成这一集，数组不能null，空数组合法。引用依据不能来自承接段或其他集。
创作格式：%s
本集规划与证据：%s
已发生的承接信息（仅背景）：%s
统一资产名称：%s
本集完整原文：%s`
