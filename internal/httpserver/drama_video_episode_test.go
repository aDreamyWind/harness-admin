package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"harness-admin/internal/drama"
)

func (s *dramaServiceStub) VideoEpisodes(_ context.Context, org, owner, project string) ([]drama.VideoEpisode, error) {
	s.videoScope = []string{org, owner, project}
	return []drama.VideoEpisode{{ScriptEpisodeID: "3a364763-e0df-4cb4-9c55-88ab0a11ff94", Title: "雨夜来信", StoryboardStatus: "pending"}}, s.videoError
}
func (s *dramaServiceStub) VideoEpisodeWorkspace(_ context.Context, org, owner, project, episode string) (drama.VideoEpisodeWorkspace, error) {
	s.videoScope = []string{org, owner, project, episode}
	return drama.VideoEpisodeWorkspace{Episode: drama.VideoEpisode{ScriptEpisodeID: episode, Title: "雨夜来信", StoryboardStatus: "pending"}, ScriptText: "林夏打开旧书店。", Characters: []drama.EpisodeAsset{{AssetID: "asset", Name: "林夏"}}, Scenes: []drama.EpisodeAsset{}, Props: []drama.EpisodeAsset{}}, s.videoError
}
func TestDramaVideoEpisodesReadRoutes(t *testing.T) {
	service := &dramaServiceStub{}
	session := accountTestSession()
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, Drama: service, SessionCookie: "test_session"})
	base := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617/video-episodes"
	for _, path := range []string{base, base + "/3a364763-e0df-4cb4-9c55-88ab0a11ff94"} {
		call := func(auth bool) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if auth {
				req.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			return res
		}
		if res := call(false); res.Code != http.StatusUnauthorized {
			t.Fatalf("无会话不得读取：%d", res.Code)
		}
		res := call(true)
		if res.Code != http.StatusOK {
			t.Fatalf("读取失败：%d %s", res.Code, res.Body)
		}
		var body struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if path == base {
			var items []drama.VideoEpisode
			if err := json.Unmarshal(body.Data, &items); err != nil || len(items) != 1 || items[0].ScriptEpisodeID != "3a364763-e0df-4cb4-9c55-88ab0a11ff94" || items[0].StoryboardStatus != "pending" {
				t.Fatal("列表契约必须包含原剧本主键和真实制作状态")
			}
		} else {
			var item drama.VideoEpisodeWorkspace
			if err := json.Unmarshal(body.Data, &item); err != nil || item.ScriptText != "林夏打开旧书店。" || len(item.Characters) != 1 || item.Characters[0].AssetID != "asset" || item.Scenes == nil || item.Props == nil {
				t.Fatal("工作区必须返回正文、真实资产标识和空数组")
			}
		}
		if len(body.Data) == 0 || service.videoScope[0] != session.User.OrganizationID || service.videoScope[1] != session.User.ID {
			t.Fatal("数据或身份范围不正确")
		}
		for _, failure := range []struct {
			err    error
			status int
		}{{drama.ErrNotFound, 404}, {drama.ErrInvalidInput, 400}} {
			service.videoError = failure.err
			if res = call(true); res.Code != failure.status {
				t.Fatalf("错误状态不正确：%d", res.Code)
			}
		}
		service.videoError = nil
	}
}
