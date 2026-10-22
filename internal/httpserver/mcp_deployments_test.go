package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpdeploy"
)

type mcpDeploymentStub struct{ input mcpdeploy.Input }

func (*mcpDeploymentStub) Enabled() bool { return true }
func (*mcpDeploymentStub) InspectRepository(_ context.Context, repositoryURL, ref string) (mcpdeploy.SourceInspection, error) {
	if repositoryURL != "https://github.com/owner/repo" || ref != "main" {
		return mcpdeploy.SourceInspection{}, mcpdeploy.ErrInvalidInput
	}
	return mcpdeploy.SourceInspection{Revision: strings.Repeat("a", 40), NodePlan: mcpdeploy.NodePlan{
		EntryFile: "build/index.js", BuildCommand: "npm run build", HasLockfile: false}}, nil
}
func (s *mcpDeploymentStub) Import(_ context.Context, input mcpdeploy.Input) (mcpdeploy.Result, error) {
	s.input = input
	return mcpdeploy.Result{Service: mcpadmin.ServiceRecord{ID: "10000000-0000-4000-8000-000000000001"},
		Version: mcpdeploy.Version{BuildStatus: "building"}}, nil
}
func (s *mcpDeploymentStub) NewVersion(ctx context.Context, input mcpdeploy.Input) (mcpdeploy.Result, error) {
	return s.Import(ctx, input)
}
func (*mcpDeploymentStub) Start(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (*mcpDeploymentStub) Stop(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (*mcpDeploymentStub) Restart(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (*mcpDeploymentStub) Rollback(context.Context, mcpadmin.Actor, string, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (*mcpDeploymentStub) Inspect(context.Context, string, string) (mcpdeploy.RuntimeStatus, error) {
	return mcpdeploy.RuntimeStatus{}, nil
}
func (*mcpDeploymentStub) ListVersions(context.Context, string, string) ([]mcpdeploy.Version, error) {
	return []mcpdeploy.Version{}, nil
}
func (*mcpDeploymentStub) UpdateMetadata(context.Context, mcpadmin.Actor, string, string, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}

func TestRepositoryMCPImportRequiresCSRFAndUsesCurrentOrganization(t *testing.T) {
	service := &mcpDeploymentStub{}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Access: accountAccessStub{}, MCPDeploy: service, SessionCookie: "test_session"})
	body := `{"name":"源码工具","repositoryUrl":"https://gitee.com/owner/repo","ref":"main","runtimeType":"node","entryFile":"server.js"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-deployments/import/repository", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 时状态码 = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/mcp-deployments/import/repository", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || service.input.OrganizationID != "org-1" || service.input.UserID != "user-1" ||
		service.input.SourceType != "gitee" || service.input.SourceRef != "https://gitee.com/owner/repo" ||
		service.input.Revision != "main" || service.input.EntryFile != "server.js" ||
		!strings.Contains(response.Body.String(), `"buildStatus":"building"`) {
		t.Fatalf("导入请求未正确处理: status=%d input=%+v body=%s", response.Code, service.input, response.Body.String())
	}
}

func TestMCPRepositoryInspectionRequiresCSRFAndReturnsPlan(t *testing.T) {
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Access: accountAccessStub{}, MCPDeploy: &mcpDeploymentStub{}, SessionCookie: "test_session"})
	body := `{"repositoryUrl":"https://github.com/owner/repo","ref":"main"}`
	for _, test := range []struct {
		csrf string
		want int
	}{{"", http.StatusForbidden}, {"csrf-token", http.StatusOK}} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-deployments/inspect/repository", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if test.csrf != "" {
			request.Header.Set("X-CSRF-Token", test.csrf)
		}
		request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.want || (test.want == http.StatusOK && !strings.Contains(response.Body.String(), "build/index.js")) {
			t.Fatalf("预检状态=%d 内容=%s", response.Code, response.Body.String())
		}
	}
}
