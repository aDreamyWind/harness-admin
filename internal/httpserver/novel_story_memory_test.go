package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"harness-admin/internal/novel"
)

func (s *novelHTTPStub) storyMemoryResult(owner novel.Owner, project, action string) (novel.StoryMemory, error) {
	s.owner, s.storyMemoryProject, s.storyMemoryAction = owner, project, action
	return s.storyMemory, s.storyMemoryError
}

func (s *novelHTTPStub) GetStoryMemory(_ context.Context, owner novel.Owner, project string) (novel.StoryMemory, error) {
	return s.storyMemoryResult(owner, project, "get")
}

func (s *novelHTTPStub) BackfillStoryMemory(_ context.Context, owner novel.Owner, project string) (novel.StoryMemory, error) {
	return s.storyMemoryResult(owner, project, "backfill")
}

func (s *novelHTTPStub) ResolveStoryMemory(_ context.Context, owner novel.Owner, project string, input novel.MemoryResolutionInput) (novel.StoryMemory, error) {
	s.memoryResolution = input
	return s.storyMemoryResult(owner, project, "resolve")
}

func TestNovelStoryMemoryRoutes(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	const project = "2ebf4214-5527-407d-92c0-a9694d1f0926"
	const path = "/api/v1/ai-novel/projects/" + project + "/story-memory"
	const conflictID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const resolveInput = `{"conflictId":"` + conflictID + `","choice":0,"ownerId":"other-user","organizationId":"other-org"}`
	send := func(method, path, body string, session, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if session {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, tt := range []struct {
		name, method, path, body string
		session, csrf            bool
		status                   int
	}{
		{"匿名查询", http.MethodGet, path, "", false, false, http.StatusUnauthorized},
		{"匿名补提取", http.MethodPost, path + "/backfill", "{}", false, true, http.StatusUnauthorized},
		{"匿名解决冲突", http.MethodPost, path + "/resolve", resolveInput, false, true, http.StatusUnauthorized},
		{"补提取缺少 CSRF", http.MethodPost, path + "/backfill", "{}", true, false, http.StatusForbidden},
		{"解决冲突缺少 CSRF", http.MethodPost, path + "/resolve", resolveInput, true, false, http.StatusForbidden},
		{"补提取请求无效", http.MethodPost, path + "/backfill", "null", true, true, http.StatusBadRequest},
		{"解决冲突请求无效", http.MethodPost, path + "/resolve", `{"choice":"0"}`, true, true, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if response := send(tt.method, tt.path, tt.body, tt.session, tt.csrf); response.Code != tt.status {
				t.Fatalf("响应状态 %d，预期 %d: %s", response.Code, tt.status, response.Body)
			}
		})
	}
	if stub.storyMemoryAction != "" {
		t.Fatalf("未授权或格式无效的请求进入故事记忆服务: %s", stub.storyMemoryAction)
	}
	const payload = `{
		"facts":[{"kind":"character","name":"阿宁","field":"relationship","value":"师徒","related":"海伯","quote":"阿宁认海伯为师。","chapter":1}],
		"conflicts":[{"id":"` + conflictID + `","reason":"两章对阿宁的本名记载不同","options":[
			{"kind":"character","name":"阿宁","field":"birth_name","value":"江安","quote":"阿宁本名江安。","chapter":1},
			{"kind":"character","name":"阿宁","field":"birth_name","value":"江晏","quote":"阿宁本名江晏。","chapter":2}
		]}],
		"pending":1,"completed":2,"failed":1,"missing":3
	}`
	if err := json.Unmarshal([]byte(payload), &stub.storyMemory); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(payload), &want); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		method, suffix, body, action string
		status                       int
	}{
		{http.MethodGet, "", "", "get", http.StatusOK},
		{http.MethodPost, "/backfill", "{}", "backfill", http.StatusAccepted},
		{http.MethodPost, "/resolve", resolveInput, "resolve", http.StatusOK},
	} {
		response := send(tt.method, path+tt.suffix, tt.body, true, tt.method == http.MethodPost)
		if response.Code != tt.status || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s 响应状态或缓存头错误: %d %+v %s", tt.action, response.Code, response.Header(), response.Body)
		}
		var result struct{ Data map[string]any }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !reflect.DeepEqual(result.Data, want) {
			t.Fatalf("%s 响应未返回完整故事记忆: %s (%v)", tt.action, response.Body, err)
		}
		if stub.storyMemoryAction != tt.action || stub.storyMemoryProject != project || stub.owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) {
			t.Fatalf("%s 未使用路径作品和会话身份: %s %s %+v", tt.action, stub.storyMemoryAction, stub.storyMemoryProject, stub.owner)
		}
	}
	if stub.memoryResolution.ConflictID != conflictID || stub.memoryResolution.Choice == nil || *stub.memoryResolution.Choice != 0 {
		t.Fatalf("选择第一条证据未正确传递: %+v", stub.memoryResolution)
	}
	stub.storyMemoryError = novel.ErrInvalid
	if response := send(http.MethodPost, path+"/resolve", `{"conflictId":"`+conflictID+`"}`, true, true); response.Code != http.StatusBadRequest || stub.memoryResolution.Choice != nil {
		t.Fatalf("未将缺省选择与选择第一条区分: %d %s", response.Code, response.Body)
	}
	stub.storyMemoryError = novel.ErrConflict
	if response := send(http.MethodPost, path+"/resolve", resolveInput, true, true); response.Code != http.StatusConflict {
		t.Fatalf("过期冲突未返回 409: %d %s", response.Code, response.Body)
	}
	stub.storyMemoryError = novel.ErrNotFound
	if response := send(http.MethodGet, path, "", true, false); response.Code != http.StatusNotFound {
		t.Fatalf("不可访问作品未返回 404: %d %s", response.Code, response.Body)
	}
}
