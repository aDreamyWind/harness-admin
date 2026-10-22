package mcpsource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestResolveAndFetchKnownRepositoryPlatforms(t *testing.T) {
	for _, tc := range []struct{ provider, apiHost, archiveHost string }{
		{"github", "api.github.com", "codeload.github.com"},
		{"gitee", "gitee.com", "gitee.com"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			sha := strings.Repeat("a", 40)
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != tc.apiHost && req.URL.Host != tc.archiveHost {
					t.Fatalf("意外目标: %s", req.URL)
				}
				body := `{"default_branch":"main"}`
				if strings.Contains(req.URL.Path, "/commits/") {
					body = `{"sha":"` + sha + `"}`
				}
				if tc.provider == "gitee" && strings.HasSuffix(req.URL.Path, "/commits") {
					body = `[{"sha":"` + sha + `"}]`
				}
				if strings.Contains(req.URL.Path, "tarball") || strings.Contains(req.URL.Path, "/tar.gz/") {
					body = "archive"
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			source := &GitHub{client: client}
			got, err := source.Resolve(context.Background(), tc.provider, "team/tool", "")
			if err != nil || got != sha {
				t.Fatalf("未解析默认分支提交: %s %v", got, err)
			}
			archive, err := source.Fetch(context.Background(), tc.provider, "team/tool", sha)
			if err != nil || string(archive) != "archive" {
				t.Fatalf("固定版本下载失败: %q %v", archive, err)
			}
		})
	}
}

func TestGiteeArchiveRejectsForeignRedirect(t *testing.T) {
	source := &GitHub{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")),
			Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}}, nil
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if _, err := source.Fetch(context.Background(), "gitee", "team/tool", strings.Repeat("a", 40)); err == nil {
		t.Fatal("必须拒绝跳转到内网地址")
	}
}
