package mcpadmin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type repositoryStub struct {
	service      ServiceRecord
	credentials  UpstreamCredentials
	connectionID string
	tools        []Tool
	saved        bool
	tested       bool
}

func (r *repositoryStub) List(context.Context, string) ([]ServiceRecord, error) {
	return []ServiceRecord{r.service}, nil
}
func (r *repositoryStub) Create(_ context.Context, input CreateInput) (ServiceRecord, error) {
	r.credentials = input.Credentials
	r.service = ServiceRecord{ID: "service-1", Code: input.Code, Name: input.Name, Description: input.Description, Status: "active", UpstreamURL: input.UpstreamURL, Transport: input.Transport, Parameters: input.Parameters}
	return r.service, nil
}
func (r *repositoryStub) Update(_ context.Context, input UpdateInput) (ServiceRecord, error) {
	r.service.Name = input.Name
	r.service.Description = input.Description
	r.service.Transport = input.Transport
	r.service.UpstreamURL = input.UpstreamURL
	r.service.Parameters = input.Parameters
	return r.service, nil
}
func (r *repositoryStub) GetCredentials(context.Context, string, string) (UpstreamCredentials, error) {
	return r.credentials, nil
}
func (r *repositoryStub) GetConnectionCredentials(_ context.Context, _, _, connectionID string) (UpstreamCredentials, error) {
	r.connectionID = connectionID
	return r.credentials, nil
}
func (r *repositoryStub) RevealCredentials(context.Context, Actor, string) (UpstreamCredentials, error) {
	return r.credentials, nil
}
func (r *repositoryStub) UpdateCredentials(_ context.Context, input UpdateCredentialsInput) (ServiceRecord, error) {
	r.credentials = input.Credentials
	return r.service, nil
}

func TestCreateGeneratesCodeAndAcceptsActualUpstreamFields(t *testing.T) {
	service := NewService(&repositoryStub{}, &upstreamStub{})
	created, err := service.Create(context.Background(), CreateInput{
		Actor: Actor{OrganizationID: "org-1", UserID: "user-1"}, Name: "任意参数服务",
		Transport: "streamable_http", UpstreamURL: "https://example.com/{tenant}/mcp",
		Parameters: []ParameterDefinition{
			{Location: "path", Name: "tenant", Required: true},
			{Location: "header", Name: "aaaa", Required: true},
			{Location: "header", Name: "Authorization"},
		},
		Credentials: UpstreamCredentials{Parameters: []ParameterValue{
			{Location: "path", Name: "tenant", Value: "corp"},
			{Location: "header", Name: "aaaa", Value: "secret"},
		}},
	})
	if err != nil || !codePattern.MatchString(created.Code) || len(created.Parameters) != 3 {
		t.Fatalf("自动编码或任意字段失败: %#v, %v", created, err)
	}
	if HasRequiredParameters(created.Parameters, UpstreamCredentials{Parameters: []ParameterValue{{Location: "header", Name: "aaaa", Value: "secret"}}}) {
		t.Fatal("缺少路径必填参数却被接受")
	}
	if !HasRequiredParameters(created.Parameters, UpstreamCredentials{Parameters: []ParameterValue{
		{Location: "header", Name: "aaaa", Value: "secret"}, {Location: "path", Name: "tenant", Value: "corp"},
	}}) {
		t.Fatal("任意字段名及路径参数应被接受")
	}
	if ValidParameterDefinitions([]ParameterDefinition{{Location: "path", Name: "tenant"}}, "https://example.com/{wrong}/mcp") {
		t.Fatal("路径占位符与声明不匹配")
	}
}

func TestServiceDefinitionDoesNotRequireConnectionValues(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, &upstreamStub{})
	actor := Actor{OrganizationID: "org-1", UserID: "user-1"}
	definition := ParameterDefinition{Location: "header", Name: "X-API-Key", Description: "由连接所属应用提供", Required: true}
	created, err := service.Create(context.Background(), CreateInput{Actor: actor, Name: "示例",
		Transport: "streamable_http", UpstreamURL: "https://example.com/mcp", Parameters: []ParameterDefinition{definition}})
	if err != nil || len(created.Parameters) != 1 || len(repository.credentials.AllParameters()) != 0 {
		t.Fatalf("服务应只登记参数定义: service=%#v credentials=%#v err=%v", created, repository.credentials, err)
	}
	updated, err := service.Update(context.Background(), UpdateInput{Actor: actor, ServiceID: created.ID,
		Name: "修改后", Description: "服务说明", Transport: "sse", UpstreamURL: "https://example.com/sse",
		Parameters: []ParameterDefinition{definition}})
	if err != nil || updated.Name != "修改后" || updated.Description != "服务说明" || updated.Transport != "sse" {
		t.Fatalf("修改服务失败: service=%#v err=%v", updated, err)
	}
	definition.Description = string(make([]byte, 501))
	if _, err := service.Update(context.Background(), UpdateInput{Actor: actor, ServiceID: created.ID,
		Name: "无效", Transport: "sse", UpstreamURL: "https://example.com/sse", Parameters: []ParameterDefinition{definition}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("过长参数说明应被拒绝: %v", err)
	}
}

func TestServiceDescriptionSupportsLongMarkdown(t *testing.T) {
	service := NewService(&repositoryStub{}, &upstreamStub{})
	actor := Actor{OrganizationID: "org-1", UserID: "user-1"}
	input := CreateInput{Actor: actor, Name: "文档服务", Description: "# 使用说明\n\n" + strings.Repeat("说明", 2500),
		Transport: "streamable_http", UpstreamURL: "https://example.com/mcp"}
	created, err := service.Create(context.Background(), input)
	if err != nil || created.Description != input.Description {
		t.Fatalf("应接受较长的 Markdown 描述: %v", err)
	}
	input.Description = strings.Repeat("文", 10001)
	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("超过上限的描述应被拒绝: %v", err)
	}
}
func (r *repositoryStub) Get(context.Context, string, string) (ServiceRecord, error) {
	return r.service, nil
}
func (r *repositoryStub) ChangeStatus(_ context.Context, input ChangeStatusInput) (ServiceRecord, error) {
	r.service.Status = input.Status
	return r.service, nil
}
func (r *repositoryStub) ReplaceTools(_ context.Context, _ DiscoverInput, tools []Tool) error {
	r.tools = tools
	r.saved = true
	return nil
}
func (r *repositoryStub) ListTools(context.Context, string, string) ([]Tool, error) {
	return r.tools, nil
}
func (r *repositoryStub) RecordTest(context.Context, TestInput, bool) error {
	r.tested = true
	return nil
}

type upstreamStub struct {
	authorization string
	called        bool
}

func (u *upstreamStub) ListTools(_ context.Context, _ ServiceRecord, credentials UpstreamCredentials) ([]Tool, error) {
	for _, item := range credentials.AllParameters() {
		if item.Location == "header" && item.Name == "Authorization" {
			u.authorization = item.Value
		}
	}
	return []Tool{{Name: "working_directory", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}
func (u *upstreamStub) CallTool(_ context.Context, _ ServiceRecord, credentials UpstreamCredentials, _ string, _ json.RawMessage) (json.RawMessage, error) {
	for _, item := range credentials.AllParameters() {
		if item.Location == "header" && item.Name == "Authorization" {
			u.authorization = item.Value
		}
	}
	u.called = true
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
}

func TestCreateRejectsCredentialInURL(t *testing.T) {
	service := NewService(&repositoryStub{}, &upstreamStub{})
	base := CreateInput{Actor: Actor{OrganizationID: "org-1", UserID: "user-1"}, Code: "example", Name: "示例", Transport: "streamable_http"}
	for _, raw := range []string{"http://user:secret@example.com/mcp", "https://example.com/mcp?key=secret", "https://example.com/mcp#fragment", "file:///etc/passwd"} {
		base.UpstreamURL = raw
		if _, err := service.Create(context.Background(), base); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("地址 %q 应拒绝，实际错误 %v", raw, err)
		}
	}
}

func TestDiscoverAndTestUseTransientAuthorization(t *testing.T) {
	repository := &repositoryStub{service: ServiceRecord{ID: "service-1", Status: "active"}}
	upstream := &upstreamStub{}
	service := NewService(repository, upstream)
	actor := Actor{OrganizationID: "org-1", UserID: "user-1"}
	tools, err := service.Discover(context.Background(), DiscoverInput{Actor: actor, ServiceID: "service-1", Credentials: UpstreamCredentials{Authorization: "Bearer upstream-secret"}})
	if err != nil || len(tools) != 1 || !repository.saved || upstream.authorization != "Bearer upstream-secret" {
		t.Fatalf("发现工具结果不正确: tools=%v err=%v", tools, err)
	}
	_, err = service.Test(context.Background(), TestInput{Actor: actor, ServiceID: "service-1", Name: "working_directory",
		Arguments: json.RawMessage(`{}`), Credentials: UpstreamCredentials{Authorization: "Bearer second-secret"}})
	if err != nil || !repository.tested || !upstream.called || upstream.authorization != "Bearer second-secret" {
		t.Fatalf("测试调用结果不正确: err=%v", err)
	}
}

func TestDiscoverUsesSavedServiceCredentialWithoutRequestValue(t *testing.T) {
	repository := &repositoryStub{
		service:     ServiceRecord{ID: "service-1", Status: "active", Parameters: []ParameterDefinition{{Location: "header", Name: "Authorization", Required: true}}},
		credentials: UpstreamCredentials{Parameters: []ParameterValue{{Location: "header", Name: "Authorization", Value: "Bearer saved"}}},
	}
	upstream := &upstreamStub{}
	service := NewService(repository, upstream)
	_, err := service.Discover(context.Background(), DiscoverInput{Actor: Actor{OrganizationID: "org-1", UserID: "user-1"}, ServiceID: "service-1"})
	if err != nil || upstream.authorization != "Bearer saved" {
		t.Fatalf("工具发现未使用服务已保存凭证: %v, %q", err, upstream.authorization)
	}
	merged := MergeCredentials(repository.credentials, UpstreamCredentials{Parameters: []ParameterValue{{Location: "header", Name: "Authorization", Value: "Bearer override"}}})
	if len(merged.Parameters) != 1 || merged.Parameters[0].Value != "Bearer override" {
		t.Fatalf("连接独立凭证未覆盖服务凭证: %#v", merged)
	}
}

func TestDiscoverAndTestUseSelectedConnectionCredentials(t *testing.T) {
	connectionID := "10000000-0000-4000-8000-000000000002"
	repository := &repositoryStub{
		service:     ServiceRecord{ID: "service-1", Status: "active", Parameters: []ParameterDefinition{{Location: "header", Name: "Authorization", Required: true}}},
		credentials: UpstreamCredentials{Parameters: []ParameterValue{{Location: "header", Name: "Authorization", Value: "Bearer connection-key"}}},
	}
	upstream := &upstreamStub{}
	service := NewService(repository, upstream)
	actor := Actor{OrganizationID: "org-1", UserID: "user-1"}
	if _, err := service.Discover(context.Background(), DiscoverInput{Actor: actor, ServiceID: "service-1", ConnectionID: connectionID}); err != nil {
		t.Fatalf("使用连接参数发现工具失败: %v", err)
	}
	if _, err := service.Test(context.Background(), TestInput{Actor: actor, ServiceID: "service-1", ConnectionID: connectionID,
		Name: "working_directory", Arguments: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("使用连接参数测试工具失败: %v", err)
	}
	if repository.connectionID != connectionID || upstream.authorization != "Bearer connection-key" {
		t.Fatalf("未使用指定连接的参数: id=%s authorization=%s", repository.connectionID, upstream.authorization)
	}
}

func TestUpdateServiceCredentialsRequiresDeclaredValues(t *testing.T) {
	repository := &repositoryStub{service: ServiceRecord{ID: "service-1", Status: "active", UpstreamURL: "https://example.com/mcp"}}
	service := NewService(repository, &upstreamStub{})
	definitions := []ParameterDefinition{{Location: "header", Name: "X-API-Key", Required: true}}
	input := UpdateCredentialsInput{Actor: Actor{OrganizationID: "org-1", UserID: "user-1"}, ServiceID: "service-1", Definitions: &definitions}
	if _, err := service.UpdateCredentials(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("缺少服务必填凭证应被拒绝: %v", err)
	}
	input.Credentials = UpstreamCredentials{Parameters: []ParameterValue{{Location: "header", Name: "X-API-Key", Value: "saved-secret"}}}
	if _, err := service.UpdateCredentials(context.Background(), input); err != nil ||
		len(repository.credentials.Parameters) != 1 {
		t.Fatalf("服务凭证更新失败: %v", err)
	}
}

func TestDisabledServiceCannotDiscoverOrTest(t *testing.T) {
	repository := &repositoryStub{service: ServiceRecord{ID: "service-1", Status: "disabled"}}
	service := NewService(repository, &upstreamStub{})
	actor := Actor{OrganizationID: "org-1", UserID: "user-1"}
	if _, err := service.Discover(context.Background(), DiscoverInput{Actor: actor, ServiceID: "service-1"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("停用服务仍可发现: %v", err)
	}
	if _, err := service.Test(context.Background(), TestInput{Actor: actor, ServiceID: "service-1", Name: "x", Arguments: json.RawMessage(`{}`)}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("停用服务仍可测试: %v", err)
	}
}
