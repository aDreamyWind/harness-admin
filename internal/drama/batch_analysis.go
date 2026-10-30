package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const BatchAnalysisChunkLimit = 5000

var ErrBatchRunning = errors.New("当前批次正在解析")

type BatchReference struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Number int    `json:"number"`
}

type Asset struct {
	SourceBatch           *BatchReference  `json:"sourceBatch"`
	UsedInBatches         []BatchReference `json:"usedInBatches"`
	ID                    string           `json:"id"`
	Kind                  string           `json:"kind"`
	Name                  string           `json:"name"`
	Description           string           `json:"description"`
	AppearanceDescription string           `json:"appearanceDescription"`
	VoiceDescription      string           `json:"voiceDescription"`
}

type EpisodeAsset struct {
	ID                    string `json:"id"`
	AssetID               string `json:"assetId"`
	Kind                  string `json:"kind"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	AppearanceDescription string `json:"appearanceDescription"`
	VoiceDescription      string `json:"voiceDescription"`
	StateLabel            string `json:"stateLabel"`
}

type ScriptEpisode struct {
	ID         string         `json:"id"`
	Number     int            `json:"number"`
	Title      string         `json:"title"`
	Synopsis   string         `json:"synopsis"`
	ScriptText string         `json:"scriptText"`
	Characters []EpisodeAsset `json:"characters"`
	Scenes     []EpisodeAsset `json:"scenes"`
	Props      []EpisodeAsset `json:"props"`
}

type BatchAssets struct {
	Characters []Asset `json:"characters"`
	Scenes     []Asset `json:"scenes"`
	Props      []Asset `json:"props"`
}

type BatchStep struct {
	InputHash    string    `json:"-"`
	Stage        string    `json:"stage"`
	ChunkNumber  int       `json:"chunkNumber"`
	ChunkTotal   int       `json:"chunkTotal"`
	Status       string    `json:"status"`
	ResultText   string    `json:"-"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	DurationMS   int64     `json:"durationMs"`
	UpdatedAt    time.Time `json:"updatedAt"`
	StartedAt    time.Time `json:"startedAt"`
}

type BatchProgress struct {
	Context      *GroundedContext `json:"context,omitempty"`
	ReviewIssues []ReviewIssue    `json:"reviewIssues"`
	Steps        []BatchStep      `json:"steps"`
	Assets       BatchAssets      `json:"assets"`
	Episodes     []EpisodeDraft   `json:"episodes"`
	Plans        []EpisodePlan    `json:"plans"`
}

type BatchResult struct {
	Batch    Batch           `json:"batch"`
	Assets   BatchAssets     `json:"assets"`
	Episodes []ScriptEpisode `json:"episodes"`
}

type AssetDraft struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	AppearanceDescription string `json:"appearanceDescription"`
	VoiceDescription      string `json:"voiceDescription"`
}

type EpisodeAssetDraft struct {
	SourceQuote string `json:"sourceQuote,omitempty"`
	Name        string `json:"name"`
	StateLabel  string `json:"stateLabel"`
}

type EpisodeDraft struct {
	Title      string              `json:"title"`
	Synopsis   string              `json:"synopsis"`
	ScriptText string              `json:"scriptText"`
	Characters []EpisodeAssetDraft `json:"characters"`
	Scenes     []EpisodeAssetDraft `json:"scenes"`
	Props      []EpisodeAssetDraft `json:"props"`
}

type BatchDraft struct {
	Characters []AssetDraft   `json:"characters"`
	Scenes     []AssetDraft   `json:"scenes"`
	Props      []AssetDraft   `json:"props"`
	Episodes   []EpisodeDraft `json:"episodes"`
}

type BatchAnalysisJob struct {
	Batch           Batch
	OrganizationID  string
	OwnerID         string
	ProjectTitle    string
	Overview        json.RawMessage
	ModelCode       string
	WorkflowVersion int
}

type BatchRunOptions struct {
	Resume      bool
	ContextText string
}
type batchRunContext struct {
	Version  int             `json:"version"`
	Title    string          `json:"title"`
	Model    string          `json:"model"`
	Overview json.RawMessage `json:"overview"`
}

var ErrResumeNotAllowed = errors.New("当前批次没有可继续的失败任务，或原文范围已变化，请重新解析")

func (s *Service) StartBatchAnalysis(ctx context.Context, org, owner, projectID, batchID string) (BatchAnalysisJob, error) {
	return s.startBatchRun(ctx, org, owner, projectID, batchID, false)
}
func (s *Service) ResumeBatchAnalysis(ctx context.Context, org, owner, projectID, batchID string) (BatchAnalysisJob, error) {
	return s.startBatchRun(ctx, org, owner, projectID, batchID, true)
}
func (s *Service) startBatchRun(ctx context.Context, org, owner, projectID, batchID string, resume bool) (BatchAnalysisJob, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return BatchAnalysisJob{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(batchID); err != nil {
		return BatchAnalysisJob{}, ErrInvalidInput
	}
	project, err := s.Get(ctx, org, owner, projectID)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	if project.Status != "ready" {
		return BatchAnalysisJob{}, ErrBatchNotReady
	}
	settings, err := s.repository.GetSettings(ctx, org, owner)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	snapshot := batchRunContext{Version: 3, Title: project.Title, Model: settings.TextModelCode, Overview: project.Overview}
	if resume {
		previous, err := s.repository.GetBatch(ctx, org, owner, projectID, batchID)
		if err != nil {
			return BatchAnalysisJob{}, err
		}
		if previous.AnalysisContext != "" {
			if err = json.Unmarshal([]byte(previous.AnalysisContext), &snapshot); err != nil {
				return BatchAnalysisJob{}, fmt.Errorf("读取解析上下文失败: %w", err)
			}
		} else {
			snapshot.Version = 1
		}
	}
	if snapshot.Version == 2 {
		snapshot.Version = 3
	}
	if snapshot.Version >= 3 {
		var creative map[string]any
		_ = json.Unmarshal(formatOnly(snapshot.Overview), &creative)
		if creative == nil {
			creative = map[string]any{}
		}
		if creative["videoStyle"] == nil {
			creative["videoStyle"] = project.Style
		}
		if creative["aspectRatio"] == nil {
			creative["aspectRatio"] = project.AspectRatio
		}
		snapshot.Overview, _ = json.Marshal(creative)
	}
	if snapshot.Model == "" {
		return BatchAnalysisJob{}, ErrModelNotConfigured
	}
	models, err := s.writer.AvailableModels(ctx, org, owner)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	available := false
	for _, m := range models {
		if m.Code == snapshot.Model && m.ModelType == "chat" {
			available = true
		}
	}
	if !available {
		return BatchAnalysisJob{}, ErrModelUnavailable
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	batch, err := s.repository.QueueBatchAnalysis(ctx, org, owner, projectID, batchID, BatchRunOptions{Resume: resume, ContextText: string(raw)})
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	return BatchAnalysisJob{Batch: batch, OrganizationID: org, OwnerID: owner, ProjectTitle: snapshot.Title, Overview: snapshot.Overview, ModelCode: snapshot.Model, WorkflowVersion: snapshot.Version}, nil
}

func (s *Service) AnalyzeBatch(job BatchAnalysisJob) error {
	if job.WorkflowVersion >= 3 {
		return s.analyzeGroundedWorkflow(job)
	}
	if job.WorkflowVersion >= 2 {
		return s.analyzeWorkflowV2(job)
	}
	return s.analyzeLegacyBatch(job)
}

func (s *Service) analyzeLegacyBatch(job BatchAnalysisJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()
	batch := job.Batch
	length := batch.EndOffset - batch.StartOffset
	source, err := s.repository.GetSourceRange(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.StartOffset, length)
	if err != nil {
		return s.failBatch(job, err)
	}
	runes := []rune(source.Text)
	if len(runes) != length {
		return s.failBatch(job, errors.New("批次原文读取不完整"))
	}
	chunks := splitBatchSource(runes)
	savedSteps, err := s.repository.ListBatchSteps(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID)
	if err != nil {
		return s.failBatch(job, err)
	}
	completed := make(map[string]string)
	for _, step := range savedSteps {
		if step.Status == "completed" && step.ChunkTotal == len(chunks) {
			completed[fmt.Sprintf("%s:%d", step.Stage, step.ChunkNumber)] = step.ResultText
		}
	}
	result := BatchDraft{Characters: []AssetDraft{}, Scenes: []AssetDraft{}, Props: []AssetDraft{}, Episodes: []EpisodeDraft{}}
	artResults := make([]BatchDraft, len(chunks))
	for index, chunk := range chunks {
		prompt := fmt.Sprintf(`你是漫剧美术指导。只依据当前原文和已确认的项目概览，提取后续绘制与配音需要保持一致的角色、场景和关键道具。不要编造原文没有的身份、外貌、音色或物件；原文没有明确线索的字段填空字符串。不要生成剧集或剧本。
返回严格 JSON 对象，仅有 characters、scenes、props 三个数组。角色每项含 name、description、appearanceDescription、voiceDescription：description 写身份、性格和人物关系；appearanceDescription 写原文可证实的年龄感、身形、面貌、发型、服装等视觉特征；voiceDescription 写原文可证实的音色、语气及说话习惯，不能把性格猜成音色。场景和道具每项含 name、description，场景描述地点、时代、光线及可见环境，道具描述外观和剧情用途。名称要简短且前后一致；仅收录确实出现、对剧情或制作有用的资产，不列泛泛提到的物品。缺失的数组返回空数组，不要输出 Markdown。
作品：%s
项目概览：%s
本批原文片段 %d/%d：
%s`, job.ProjectTitle, string(job.Overview), index+1, len(chunks), string(chunk))
		key := fmt.Sprintf("art:%d", index+1)
		response, wasSaved := completed[key]
		started := time.Now()
		if !wasSaved {
			step := BatchStep{Stage: "art", ChunkNumber: index + 1, ChunkTotal: len(chunks)}
			if err := s.repository.StartBatchStep(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID, batch.AnalysisRevision, step); err != nil {
				return s.failBatch(job, err)
			}
			var callErr error
			response, callErr = s.completeBatchPrompt(ctx, job, prompt, "art", index+1)
			if callErr != nil {
				return s.failBatch(job, fmt.Errorf("美术指导第 %d 段模型调用失败: %w", index+1, callErr))
			}
		}
		data, parseErr := parseObject(response)
		if parseErr != nil {
			return s.failBatch(job, fmt.Errorf("美术指导第 %d 段返回格式无效: %w", index+1, parseErr))
		}
		var art BatchDraft
		if parseErr := json.Unmarshal(data, &art); parseErr != nil {
			return s.failBatch(job, fmt.Errorf("美术指导第 %d 段资产结构无效: %w", index+1, parseErr))
		}
		if parseErr := validateArtDraft(data, art); parseErr != nil {
			return s.failBatch(job, fmt.Errorf("美术指导第 %d 段资产结果不完整: %w", index+1, parseErr))
		}
		if !wasSaved {
			step := BatchStep{Stage: "art", ChunkNumber: index + 1, ChunkTotal: len(chunks), ResultText: string(data), DurationMS: time.Since(started).Milliseconds()}
			if err := s.repository.CompleteBatchStep(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID, batch.AnalysisRevision, step); err != nil {
				return s.failBatch(job, err)
			}
		}
		artResults[index] = art
		result.Characters = append(result.Characters, art.Characters...)
		result.Scenes = append(result.Scenes, art.Scenes...)
		result.Props = append(result.Props, art.Props...)
	}
	previousSynopsis := ""
	for index, chunk := range chunks {
		assetsJSON, marshalErr := json.Marshal(struct {
			Characters []AssetDraft `json:"characters"`
			Scenes     []AssetDraft `json:"scenes"`
			Props      []AssetDraft `json:"props"`
		}{artResults[index].Characters, artResults[index].Scenes, artResults[index].Props})
		if marshalErr != nil {
			return s.failBatch(job, fmt.Errorf("整理第 %d 段美术资产失败: %w", index+1, marshalErr))
		}
		prompt := fmt.Sprintf(`你是漫剧导演兼短剧编剧。根据当前原文片段进行分集拆分，并写出可以拍摄的完整中文剧本。只改编原文已经发生的事件，不增加新人物、因果或结局。原文有“第N集”时尊重原集界；没有时按完整剧情弧自然拆集，不按固定字数或固定集数机械拆分。短剧节奏要求：开场迅速建立人物目标或悬念，每场推动冲突与信息变化，结尾尽量留下转折、悬念或下一步行动；不要为制造反转而编造原文事实。剧本文本包含清楚的场景标题（地点、内外、时间）、可视化动作和对白，少用无法拍摄的心理叙述。
返回严格 JSON 对象，仅有 episodes 数组。每集包含 title、synopsis、scriptText、characters、scenes、props；后三项是关联资产数组，每项含 name、stateLabel。名称只能从下方美术资产清单选择；stateLabel 记录本集形象、时段或状态，无法确定时填空字符串。scriptText 必须是完整剧本正文，不是大纲或剧情摘要。不要重复改写上一段已经完成的剧情，不要输出 Markdown。
作品：%s
项目概览：%s
上一段末集梗概（仅用于衔接）：%s
本段美术资产清单：%s
本批原文片段 %d/%d：
%s`, job.ProjectTitle, string(job.Overview), previousSynopsis, string(assetsJSON), index+1, len(chunks), string(chunk))
		key := fmt.Sprintf("director:%d", index+1)
		response, wasSaved := completed[key]
		started := time.Now()
		if !wasSaved {
			step := BatchStep{Stage: "director", ChunkNumber: index + 1, ChunkTotal: len(chunks)}
			if err := s.repository.StartBatchStep(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID, batch.AnalysisRevision, step); err != nil {
				return s.failBatch(job, err)
			}
			var callErr error
			response, callErr = s.completeBatchPrompt(ctx, job, prompt, "director", index+1)
			if callErr != nil {
				return s.failBatch(job, fmt.Errorf("导演第 %d 段模型调用失败: %w", index+1, callErr))
			}
		}
		data, parseErr := parseObject(response)
		if parseErr != nil {
			return s.failBatch(job, fmt.Errorf("导演第 %d 段返回格式无效: %w", index+1, parseErr))
		}
		var directed struct {
			Episodes []EpisodeDraft `json:"episodes"`
		}
		if parseErr := json.Unmarshal(data, &directed); parseErr != nil {
			return s.failBatch(job, fmt.Errorf("导演第 %d 段剧本结构无效: %w", index+1, parseErr))
		}
		part := BatchDraft{Characters: artResults[index].Characters, Scenes: artResults[index].Scenes, Props: artResults[index].Props, Episodes: directed.Episodes}
		if parseErr := validateBatchDraft(part); parseErr != nil {
			return s.failBatch(job, fmt.Errorf("导演第 %d 段解析结果不完整: %w", index+1, parseErr))
		}
		if !wasSaved {
			step := BatchStep{Stage: "director", ChunkNumber: index + 1, ChunkTotal: len(chunks), ResultText: string(data), DurationMS: time.Since(started).Milliseconds()}
			if err := s.repository.CompleteBatchStep(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID, batch.AnalysisRevision, step); err != nil {
				return s.failBatch(job, err)
			}
		}
		result.Episodes = append(result.Episodes, directed.Episodes...)
		previousSynopsis = directed.Episodes[len(directed.Episodes)-1].Synopsis
		if utf8.RuneCountInString(previousSynopsis) > 500 {
			previousSynopsis = string([]rune(previousSynopsis)[:500])
		}
	}
	if err := s.repository.SaveBatchResult(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID, batch.AnalysisRevision, result); err != nil {
		return s.failBatch(job, err)
	}
	return nil
}

func (s *Service) BatchProgress(ctx context.Context, organizationID, ownerID, projectID, batchID string) (BatchProgress, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return BatchProgress{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(batchID); err != nil {
		return BatchProgress{}, ErrInvalidInput
	}
	steps, err := s.repository.ListBatchSteps(ctx, organizationID, ownerID, projectID, batchID)
	if err != nil {
		return BatchProgress{}, err
	}
	progress := BatchProgress{ReviewIssues: []ReviewIssue{}, Steps: steps, Assets: BatchAssets{Characters: []Asset{}, Scenes: []Asset{}, Props: []Asset{}}, Episodes: []EpisodeDraft{}, Plans: []EpisodePlan{}}
	seen := map[string]bool{}
	for _, step := range steps {
		if step.Status != "completed" {
			continue
		}
		data, err := parseObject(step.ResultText)
		if err != nil {
			return BatchProgress{}, fmt.Errorf("读取已保存解析步骤失败: %w", err)
		}
		if step.Stage == "art" {
			var art BatchDraft
			if err := json.Unmarshal(data, &art); err != nil {
				return BatchProgress{}, err
			}
			for _, group := range []struct {
				kind  string
				items []AssetDraft
			}{{"character", art.Characters}, {"scene", art.Scenes}, {"prop", art.Props}} {
				for _, item := range group.items {
					key := group.kind + "\x00" + item.Name
					if seen[key] {
						continue
					}
					seen[key] = true
					asset := Asset{UsedInBatches: []BatchReference{}, Kind: group.kind, Name: item.Name, Description: item.Description, AppearanceDescription: item.AppearanceDescription, VoiceDescription: item.VoiceDescription}
					switch group.kind {
					case "character":
						progress.Assets.Characters = append(progress.Assets.Characters, asset)
					case "scene":
						progress.Assets.Scenes = append(progress.Assets.Scenes, asset)
					case "prop":
						progress.Assets.Props = append(progress.Assets.Props, asset)
					}
				}
			}
		} else if step.Stage == "context" {
			var value GroundedContext
			if err := json.Unmarshal(data, &value); err != nil {
				return BatchProgress{}, err
			}
			progress.Context = &value
		} else if step.Stage == "plan" {
			var plan struct {
				Episodes []EpisodePlan `json:"episodes"`
			}
			if err := json.Unmarshal(data, &plan); err != nil {
				return BatchProgress{}, err
			}
			progress.Plans = plan.Episodes
		} else if step.Stage == "director" {
			var directed struct {
				Episodes []EpisodeDraft `json:"episodes"`
			}
			if err := json.Unmarshal(data, &directed); err != nil {
				return BatchProgress{}, err
			}
			progress.Episodes = append(progress.Episodes, directed.Episodes...)
		}
	}
	progress.ReviewIssues, err = s.repository.ListReviewIssues(ctx, organizationID, ownerID, projectID, batchID)
	if err != nil {
		return BatchProgress{}, err
	}
	return progress, nil
}

func (s *Service) completeBatchPrompt(ctx context.Context, job BatchAnalysisJob, prompt string, stage string, number int) (string, error) {
	w := &batchWorkflow{service: s, job: job, ctx: ctx}
	response, callErr := w.complete(stage, number, prompt)
	if (errors.Is(callErr, ErrModelTimeout) || errors.Is(callErr, ErrEmptyModelContent)) && ctx.Err() == nil {
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-timer.C:
			response, callErr = w.complete(stage, number, prompt)
		case <-ctx.Done():
			timer.Stop()
			callErr = ctx.Err()
		}
	}
	return response, callErr
}

func validateArtDraft(raw json.RawMessage, art BatchDraft) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"characters", "scenes", "props"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("缺少 %s 列表", key)
		}
	}
	for _, group := range [][]AssetDraft{art.Characters, art.Scenes, art.Props} {
		for _, asset := range group {
			if strings.TrimSpace(asset.Name) == "" || utf8.RuneCountInString(asset.Name) > 120 {
				return errors.New("资产名称无效")
			}
		}
	}
	return nil
}

func (s *Service) failBatch(job BatchAnalysisJob, reason error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	message := string([]rune(reason.Error())[:min(500, utf8.RuneCountInString(reason.Error()))])
	if err := s.repository.FailBatchAnalysis(ctx, job.OrganizationID, job.OwnerID, job.Batch.ProjectID,
		job.Batch.ID, job.Batch.AnalysisRevision, message); err != nil && !errors.Is(err, ErrBatchConflict) {
		return fmt.Errorf("%w；保存批次失败状态失败: %v", reason, err)
	}
	return reason
}

func validateBatchDraft(draft BatchDraft) error {
	if len(draft.Episodes) == 0 || len(draft.Episodes) > 12 {
		return errors.New("剧集数量应为 1 至 12 集")
	}
	for _, episode := range draft.Episodes {
		if strings.TrimSpace(episode.Title) == "" || utf8.RuneCountInString(episode.Title) > 120 || strings.TrimSpace(episode.ScriptText) == "" {
			return errors.New("剧集缺少标题或剧本正文")
		}
	}
	for _, group := range [][]AssetDraft{draft.Characters, draft.Scenes, draft.Props} {
		for _, asset := range group {
			if strings.TrimSpace(asset.Name) == "" || utf8.RuneCountInString(asset.Name) > 120 {
				return errors.New("资产名称无效")
			}
		}
	}
	return nil
}

func splitBatchSource(source []rune) [][]rune {
	chunks := make([][]rune, 0, (len(source)+BatchAnalysisChunkLimit-1)/BatchAnalysisChunkLimit)
	for start := 0; start < len(source); {
		end := min(start+BatchAnalysisChunkLimit, len(source))
		if end < len(source) {
			bestHeading, bestParagraph := 0, 0
			for offset := start + BatchAnalysisChunkLimit/2; offset < end; offset++ {
				if source[offset-1] != '\n' {
					continue
				}
				lineEnd := offset
				for lineEnd < len(source) && lineEnd < offset+100 && source[lineEnd] != '\n' {
					lineEnd++
				}
				heading := normalizedHeading(string(source[offset:lineEnd]))
				if episodeHeading.MatchString(heading) || chapterHeading.MatchString(heading) {
					bestHeading = offset
				} else if offset >= 2 && source[offset-2] == '\n' {
					bestParagraph = offset
				}
			}
			if bestHeading > 0 {
				end = bestHeading
			} else if bestParagraph > 0 {
				end = bestParagraph
			}
		}
		chunks = append(chunks, source[start:end])
		start = end
	}
	return chunks
}

func (s *Service) BatchResult(ctx context.Context, organizationID, ownerID, projectID, batchID string) (BatchResult, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return BatchResult{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(batchID); err != nil {
		return BatchResult{}, ErrInvalidInput
	}
	return s.repository.GetBatchResult(ctx, organizationID, ownerID, projectID, batchID)
}

func (s *Service) ProjectAssets(ctx context.Context, organizationID, ownerID, projectID, kind, query string) ([]Asset, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, ErrInvalidInput
	}
	if kind != "character" && kind != "scene" && kind != "prop" {
		return nil, ErrInvalidInput
	}
	if utf8.RuneCountInString(query) > 100 {
		return nil, ErrInvalidInput
	}
	return s.repository.ListProjectAssets(ctx, organizationID, ownerID, projectID, kind, query)
}

func (s *Service) AddEpisodeAsset(ctx context.Context, organizationID, ownerID, projectID, episodeID, assetID, stateLabel string) (EpisodeAsset, error) {
	for _, id := range []string{projectID, episodeID, assetID} {
		if _, err := uuid.Parse(id); err != nil {
			return EpisodeAsset{}, ErrInvalidInput
		}
	}
	if utf8.RuneCountInString(stateLabel) > 120 {
		return EpisodeAsset{}, ErrInvalidInput
	}
	return s.repository.AddEpisodeAsset(ctx, organizationID, ownerID, projectID, episodeID, assetID, strings.TrimSpace(stateLabel))
}

func (s *Service) RemoveEpisodeAsset(ctx context.Context, organizationID, ownerID, projectID, episodeID, linkID string) error {
	for _, id := range []string{projectID, episodeID, linkID} {
		if _, err := uuid.Parse(id); err != nil {
			return ErrInvalidInput
		}
	}
	return s.repository.RemoveEpisodeAsset(ctx, organizationID, ownerID, projectID, episodeID, linkID)
}
