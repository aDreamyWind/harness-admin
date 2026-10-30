package drama

import (
	"context"
	"errors"
	"testing"
)

func (r *fakeRepository) ListVideoEpisodes(context.Context, string, string, string) ([]VideoEpisode, error) {
	return []VideoEpisode{}, nil
}
func (r *fakeRepository) GetVideoEpisodeWorkspace(context.Context, string, string, string, string) (VideoEpisodeWorkspace, error) {
	return VideoEpisodeWorkspace{}, ErrNotFound
}

type videoRepositoryProbe struct {
	fakeRepository
	scope   []string
	failure error
}

func (r *videoRepositoryProbe) ListVideoEpisodes(_ context.Context, org, owner, project string) ([]VideoEpisode, error) {
	r.scope = []string{org, owner, project}
	return []VideoEpisode{{ScriptEpisodeID: "episode"}}, r.failure
}
func (r *videoRepositoryProbe) GetVideoEpisodeWorkspace(_ context.Context, org, owner, project, episode string) (VideoEpisodeWorkspace, error) {
	r.scope = []string{org, owner, project, episode}
	return VideoEpisodeWorkspace{Episode: VideoEpisode{ScriptEpisodeID: episode}}, r.failure
}

func TestVideoEpisodesValidateIDsAndForwardOwnerScope(t *testing.T) {
	const project = "b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	const episode = "3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	repo := &videoRepositoryProbe{}
	s := NewService(repo, nil)
	ctx := context.Background()
	if _, err := s.VideoEpisodes(ctx, "org", "owner", "bad"); !errors.Is(err, ErrInvalidInput) || repo.scope != nil {
		t.Fatal("非法项目不得进入仓储")
	}
	for _, ids := range [][2]string{{"bad", episode}, {project, "bad"}} {
		if _, err := s.VideoEpisodeWorkspace(ctx, "org", "owner", ids[0], ids[1]); !errors.Is(err, ErrInvalidInput) || repo.scope != nil {
			t.Fatal("非法主键不得进入仓储")
		}
	}
	items, err := s.VideoEpisodes(ctx, "org", "owner", project)
	if err != nil || len(items) != 1 || len(repo.scope) != 3 || repo.scope[0] != "org" || repo.scope[1] != "owner" || repo.scope[2] != project {
		t.Fatal("列表必须传递当前租户和用户")
	}
	item, err := s.VideoEpisodeWorkspace(ctx, "org", "owner", project, episode)
	if err != nil || item.Episode.ScriptEpisodeID != episode || len(repo.scope) != 4 || repo.scope[3] != episode {
		t.Fatal("单集必须使用剧本主键")
	}
	repo.failure = ErrNotFound
	if _, err = s.VideoEpisodes(ctx, "org", "owner", project); !errors.Is(err, ErrNotFound) {
		t.Fatal("不可吞掉仓储错误")
	}
	if _, err = s.VideoEpisodeWorkspace(ctx, "org", "owner", project, episode); !errors.Is(err, ErrNotFound) {
		t.Fatal("不可吞掉仓储错误")
	}
}
