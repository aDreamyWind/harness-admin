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

const BatchAnalysisChunkLimit = 12000

var ErrBatchRunning = errors.New("当前批次正在解析")

type Asset struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type EpisodeAsset struct {
	ID          string `json:"id"`
	AssetID     string `json:"assetId"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StateLabel  string `json:"stateLabel"`
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

type BatchResult struct {
	Batch    Batch           `json:"batch"`
	Assets   BatchAssets     `json:"assets"`
	Episodes []ScriptEpisode `json:"episodes"`
}

type AssetDraft struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type EpisodeAssetDraft struct {
	Name       string `json:"name"`
	StateLabel string `json:"stateLabel"`
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
	Batch          Batch
	OrganizationID string
	OwnerID        string
	ProjectTitle   string
	Overview       json.RawMessage
	ModelCode      string
}

func (s *Service) StartBatchAnalysis(ctx context.Context, organizationID, ownerID, projectID, batchID string) (BatchAnalysisJob, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return BatchAnalysisJob{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(batchID); err != nil {
		return BatchAnalysisJob{}, ErrInvalidInput
	}
	project, err := s.Get(ctx, organizationID, ownerID, projectID)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	if project.Status != "ready" {
		return BatchAnalysisJob{}, ErrBatchNotReady
	}
	settings, err := s.repository.GetSettings(ctx, organizationID, ownerID)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	if settings.TextModelCode == "" {
		return BatchAnalysisJob{}, ErrModelNotConfigured
	}
	batch, err := s.repository.QueueBatchAnalysis(ctx, organizationID, ownerID, projectID, batchID)
	if err != nil {
		return BatchAnalysisJob{}, err
	}
	return BatchAnalysisJob{Batch: batch, OrganizationID: organizationID, OwnerID: ownerID,
		ProjectTitle: project.Title, Overview: project.Overview, ModelCode: settings.TextModelCode}, nil
}

func (s *Service) AnalyzeBatch(job BatchAnalysisJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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
	result := BatchDraft{Characters: []AssetDraft{}, Scenes: []AssetDraft{}, Props: []AssetDraft{}, Episodes: []EpisodeDraft{}}
	previousSynopsis := ""
	for index, chunk := range chunks {
		prompt := fmt.Sprintf(`你是漫剧编剧与剧本解析员。只改编提供的原文，不添加原文没有的事件或结局。原文已有“第N集”时尊重原有集界；没有分集时按这一段的完整剧情弧拆成一至数集。将小说叙述改成可拍摄的剧本，包含场景标题、动作和对白，保留关键因果与人物关系。不要把上一段的剧情重复写进本段。人物、场景、道具只能来自这段原文。
返回严格 JSON 对象，键为 characters、scenes、props、episodes。前三项均为对象数组，每项含 name、description；episodes 为非空数组，每项含 title、synopsis、scriptText、characters、scenes、props，其中后三项为对象数组，每项含 name、stateLabel，名称要和上面资产列表一致。scriptText 必须是完整的中文剧本正文，不是大纲。每个片段尽量按剧情自然拆集，不要固定集数。不要输出 Markdown。
作品：%s
项目概览：%s
上一段末集梗概（仅用于衔接）：%s
本批原文片段 %d/%d：
%s`,
			job.ProjectTitle, string(job.Overview), previousSynopsis, index+1, len(chunks), string(chunk))
		response, callErr := s.writer.Complete(ctx, job.OrganizationID, job.OwnerID, job.ModelCode, prompt)
		if callErr != nil {
			return s.failBatch(job, fmt.Errorf("第 %d 段模型调用失败: %w", index+1, callErr))
		}
		data, parseErr := parseObject(response)
		if parseErr != nil {
			return s.failBatch(job, fmt.Errorf("第 %d 段返回格式无效: %w", index+1, parseErr))
		}
		var part BatchDraft
		if parseErr := json.Unmarshal(data, &part); parseErr != nil {
			return s.failBatch(job, fmt.Errorf("第 %d 段剧本结构无效: %w", index+1, parseErr))
		}
		if parseErr := validateBatchDraft(part); parseErr != nil {
			return s.failBatch(job, fmt.Errorf("第 %d 段解析结果不完整: %w", index+1, parseErr))
		}
		result.Characters = append(result.Characters, part.Characters...)
		result.Scenes = append(result.Scenes, part.Scenes...)
		result.Props = append(result.Props, part.Props...)
		result.Episodes = append(result.Episodes, part.Episodes...)
		previousSynopsis = part.Episodes[len(part.Episodes)-1].Synopsis
		if utf8.RuneCountInString(previousSynopsis) > 500 {
			previousSynopsis = string([]rune(previousSynopsis)[:500])
		}
	}
	if err := s.repository.SaveBatchResult(ctx, job.OrganizationID, job.OwnerID, batch.ProjectID, batch.ID, batch.AnalysisRevision, result); err != nil {
		return s.failBatch(job, err)
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
