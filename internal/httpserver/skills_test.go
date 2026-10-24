package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/mcpclient"
	"harness-admin/internal/skilladmin"
)

type skillServiceStub struct {
	allow   bool
	archive []byte
	actor   skilladmin.Actor
	github  skilladmin.GitHubImportInput
}

func (s *skillServiceStub) Import(_ context.Context, actor skilladmin.Actor, archive []byte) (skilladmin.Asset, error) {
	s.archive = archive
	s.actor = actor
	return skilladmin.Asset{ID: "skill-1", Name: "lunch-helper"}, nil
}
func (s *skillServiceStub) ImportGitHub(_ context.Context, actor skilladmin.Actor, input skilladmin.GitHubImportInput) (skilladmin.GitHubImportResult, error) {
	s.actor, s.github = actor, input
	return skilladmin.GitHubImportResult{Asset: skilladmin.Asset{Name: "lunch-helper"}, Revision: strings.Repeat("a", 40)}, nil
}
func (*skillServiceStub) List(context.Context, string) ([]skilladmin.Asset, error) {
	return []skilladmin.Asset{}, nil
}
func (*skillServiceStub) Versions(context.Context, string, string) ([]skilladmin.Version, error) {
	return []skilladmin.Version{}, nil
}
func (*skillServiceStub) ReadVersionFile(context.Context, string, string, string, string) (skilladmin.File, error) {
	return skilladmin.File{}, nil
}
func (*skillServiceStub) Publish(context.Context, skilladmin.Actor, string, string, string) (skilladmin.Asset, error) {
	return skilladmin.Asset{}, nil
}
func (*skillServiceStub) ReplaceGrants(context.Context, skilladmin.Actor, string, []string, []string) (skilladmin.Asset, error) {
	return skilladmin.Asset{}, nil
}
func (s *skillServiceStub) ListForUser(_ context.Context, _, userID string) ([]skilladmin.Published, error) {
	if !s.allow || userID != "user-1" {
		return []skilladmin.Published{}, nil
	}
	return []skilladmin.Published{s.published()}, nil
}
func (s *skillServiceStub) GetForUser(_ context.Context, _, userID, name string) (skilladmin.Published, error) {
	if !s.allow || userID != "user-1" || name != "lunch-helper" {
		return skilladmin.Published{}, skilladmin.ErrForbidden
	}
	return s.published(), nil
}
func (s *skillServiceStub) ReadFile(_ context.Context, _, userID, name, filePath string) (skilladmin.File, error) {
	if !s.allow || userID != "user-1" || name != "lunch-helper" || filePath != "SKILL.md" {
		return skilladmin.File{}, skilladmin.ErrForbidden
	}
	return skilladmin.File{Path: "SKILL.md", MIMEType: "text/markdown", Content: []byte("---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n"), Digest: "sha256:" + strings.Repeat("a", 64), Size: 64}, nil
}
func (*skillServiceStub) published() skilladmin.Published {
	return skilladmin.Published{Asset: skilladmin.Asset{Name: "lunch-helper"}, Version: skilladmin.Version{
		Frontmatter: json.RawMessage(`{"name":"lunch-helper","description":"推荐午餐"}`),
		Files:       []skilladmin.File{{Path: "SKILL.md", MIMEType: "text/markdown", Digest: "sha256:" + strings.Repeat("a", 64), Size: 64}},
	}}
}

func TestEnterpriseSkillImportRequiresSessionAndCSRF(t *testing.T) {
	skills := &skillServiceStub{}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Access: accountAccessStub{}, Skills: skills, OIDCProvider: &mcpEmployeeProviderStub{}, SessionCookie: "test_session"})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "skill.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("example"))
	_ = form.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || len(skills.archive) != 0 {
		t.Fatalf("未登录导入未被拒绝: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/skills/import", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || len(skills.archive) != 0 {
		t.Fatalf("缺少 CSRF 未被拒绝: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/skills/import", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || string(skills.archive) != "example" || skills.actor.OrganizationID != "org-1" {
		t.Fatalf("已授权导入失败: %d %s", response.Code, response.Body.String())
	}
}

func TestEnterpriseSkillGitHubImportRequiresSessionAndCSRF(t *testing.T) {
	skills := &skillServiceStub{}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Access: accountAccessStub{}, Skills: skills, OIDCProvider: &mcpEmployeeProviderStub{}, SessionCookie: "test_session"})
	body := []byte(`{"repositoryUrl":"https://github.com/owner/repo","ref":"main","skillPath":"skills/lunch-helper"}`)
	for _, tc := range []struct {
		cookie, csrf string
		want         int
	}{
		{"", "", http.StatusUnauthorized}, {"token", "", http.StatusForbidden}, {"token", "csrf-token", http.StatusCreated},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import/github", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if tc.cookie != "" {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: tc.cookie})
		}
		if tc.csrf != "" {
			request.Header.Set("X-CSRF-Token", tc.csrf)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Fatalf("GitHub 导入返回 %d，预期 %d: %s", response.Code, tc.want, response.Body.String())
		}
	}
	if skills.github.RepositoryURL != "https://github.com/owner/repo" || skills.github.SkillPath != "skills/lunch-helper" {
		t.Fatalf("GitHub 参数未传递: %#v", skills.github)
	}
}

func TestEnterpriseSkillMCPListReadAndRevocation(t *testing.T) {
	skills := &skillServiceStub{allow: true}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{}, Skills: skills, OIDCProvider: &mcpEmployeeProviderStub{}})
	server := httptest.NewServer(router)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "enterprise-skill-test", Version: "1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/enterprise-skills/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("连接企业 Skills 网关失败: %v", err)
	}
	defer session.Close()
	if _, ok := session.InitializeResult().Capabilities.Extensions[mcpclient.SkillsExtension]; !ok {
		t.Fatal("未声明 Skills 扩展")
	}
	listed, err := mcp.CallCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](context.Background(), session, "skills/list", &mcpclient.ListSkillsParams{})
	if err != nil || len(listed.Skills) != 1 || listed.Skills[0].URI != "skill://enterprise/lunch-helper/SKILL.md" {
		t.Fatalf("企业 Skill 目录异常: %#v, %v", listed, err)
	}
	got, err := mcp.CallCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](context.Background(), session, "skills/get", &mcpclient.GetSkillParams{URI: listed.Skills[0].URI})
	if err != nil || got.Skill.URI != listed.Skills[0].URI {
		t.Fatalf("企业 Skill 元数据异常: %#v, %v", got, err)
	}
	read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: listed.Skills[0].URI})
	if err != nil || len(read.Contents) != 1 || !strings.Contains(read.Contents[0].Text, "正文") {
		t.Fatalf("企业 Skill 文件读取异常: %#v, %v", read, err)
	}
	skills.allow = false
	_, err = session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: listed.Skills[0].URI})
	if err == nil {
		t.Fatal("撤权后仍能读取企业 Skill")
	}
}
