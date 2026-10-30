package drama

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// VideoEpisode 是正式分集剧本的制作入口，主键始终使用分集剧本 ID。
// 分镜、片段和成片尚未接入，当前所有正式剧集均处于待拆分状态。
type VideoEpisode struct {
	ScriptEpisodeID    string         `json:"scriptEpisodeId"`
	ProjectID          string         `json:"projectId"`
	Number             int            `json:"number"`
	BatchEpisodeNumber int            `json:"batchEpisodeNumber"`
	Batch              BatchReference `json:"batch"`
	Title              string         `json:"title"`
	Synopsis           string         `json:"synopsis"`
	StoryboardStatus   string         `json:"storyboardStatus"`
	CharacterCount     int            `json:"characterCount"`
	SceneCount         int            `json:"sceneCount"`
	PropCount          int            `json:"propCount"`
	ScriptNeedsReview  bool           `json:"scriptNeedsReview"`
	ScriptUpdatedAt    time.Time      `json:"scriptUpdatedAt"`
}

type VideoEpisodeWorkspace struct {
	Episode    VideoEpisode   `json:"episode"`
	ScriptText string         `json:"scriptText"`
	Characters []EpisodeAsset `json:"characters"`
	Scenes     []EpisodeAsset `json:"scenes"`
	Props      []EpisodeAsset `json:"props"`
}

func (s *Service) VideoEpisodes(ctx context.Context, org, owner, project string) ([]VideoEpisode, error) {
	if _, err := uuid.Parse(project); err != nil {
		return nil, ErrInvalidInput
	}
	return s.repository.ListVideoEpisodes(ctx, org, owner, project)
}

func (s *Service) VideoEpisodeWorkspace(ctx context.Context, org, owner, project, episode string) (VideoEpisodeWorkspace, error) {
	for _, id := range []string{project, episode} {
		if _, err := uuid.Parse(id); err != nil {
			return VideoEpisodeWorkspace{}, ErrInvalidInput
		}
	}
	return s.repository.GetVideoEpisodeWorkspace(ctx, org, owner, project, episode)
}
