package mcpconnection

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"harness-admin/internal/mcpadmin"
)

type repositoryStub struct {
	created CreateInput
	item    GatewayConnection
	allowed bool
}

func (*repositoryStub) List(context.Context, string) ([]Connection, error) { return nil, nil }
func (r *repositoryStub) Create(_ context.Context, input CreateInput) (Connection, error) {
	r.created = input
	return Connection{ID: "connection-1", Code: input.Code, Purpose: input.Purpose, AuthMode: input.AuthMode}, nil
}
func (*repositoryStub) ReplaceGrants(context.Context, GrantsInput) (Connection, error) {
	return Connection{}, nil
}
func (r *repositoryStub) GetByCode(context.Context, string) (GatewayConnection, error) {
	return r.item, nil
}
func (r *repositoryStub) GetByID(_ context.Context, organizationID, _ string) (GatewayConnection, error) {
	if r.item.OrganizationID != organizationID {
		return GatewayConnection{}, ErrNotFound
	}
	return r.item, nil
}
func (*repositoryStub) ChangeStatus(context.Context, Actor, string, string) (Connection, error) {
	return Connection{}, nil
}
func (*repositoryStub) RevealSecret(context.Context, Actor, string) (string, error) { return "", nil }
func (*repositoryStub) RevealUpstreamAuthorization(context.Context, Actor, string) (string, error) {
	return "", nil
}
func (*repositoryStub) RevealUpstreamCredentials(context.Context, Actor, string) (mcpadmin.UpstreamCredentials, error) {
	return mcpadmin.UpstreamCredentials{}, nil
}
func (*repositoryStub) RotateSecret(context.Context, Actor, string, string, []byte, string) error {
	return nil
}
func (*repositoryStub) UpdateUpstreamCredentials(context.Context, Actor, string, mcpadmin.UpstreamCredentials) (Connection, error) {
	return Connection{}, nil
}
func (r *repositoryStub) AllowedUser(context.Context, string, string, string) (string, bool, error) {
	return "department-1", r.allowed, nil
}
func (*repositoryStub) ListForUser(context.Context, string, string) ([]Connection, error) {
	return nil, nil
}
func (*repositoryStub) RecordCall(context.Context, CallRecord) error       { return nil }
func (*repositoryStub) ListUsage(context.Context, string) ([]Usage, error) { return nil, nil }

type limiterStub struct {
	key   string
	limit int
}

func (l *limiterStub) Allow(_ context.Context, key string, limit int) (bool, error) {
	l.key, l.limit = key, limit
	return true, nil
}

func TestCreateDeveloperConnectionGeneratesBearerAndCanChooseNone(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, &limiterStub{})
	input := CreateInput{Actor: Actor{OrganizationID: "org-1", UserID: "user-1"},
		ServiceID: "00000000-0000-4000-8000-000000000001", Code: "app_a", Name: "应用 A",
		Purpose: "developer", AuthMode: "bearer"}
	created, err := service.Create(context.Background(), input)
	if err != nil || !strings.HasPrefix(created.Secret, "harness_mcp_") || len(repository.created.SecretHash) != 32 {
		t.Fatalf("开发者密钥未生成: %#v, %v", created, err)
	}
	input.Code, input.AuthMode = "app_public", "none"
	created, err = service.Create(context.Background(), input)
	if err != nil || created.Secret != "" || len(repository.created.SecretHash) != 0 {
		t.Fatalf("明确选择无鉴权后仍生成密钥: %#v, %v", created, err)
	}
	input.Code, input.AuthMode = "app_invalid", "oidc"
	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("开发者连接不应接受 OIDC 模式: %v", err)
	}
}

func TestCreateConnectionGeneratesInternalCode(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, &limiterStub{})
	created, err := service.Create(context.Background(), CreateInput{
		Actor:     Actor{OrganizationID: "org-1", UserID: "user-1"},
		ServiceID: "00000000-0000-4000-8000-000000000001", Name: "业务连接",
		Purpose: "developer", AuthMode: "bearer",
		UpstreamParameters: []mcpadmin.ParameterValue{{Location: "header", Name: "aaaa", Value: "secret"}},
	})
	if err != nil || !strings.HasPrefix(created.Code, "mcp_c_") || repository.created.UpstreamParameters[0].Name != "aaaa" {
		t.Fatalf("自动编码或自定义参数失败: %#v, %v", created, err)
	}
}

func TestAuthorizeDeveloperAndHarnessHaveDifferentStatisticsIdentity(t *testing.T) {
	repository := &repositoryStub{allowed: true}
	limiter := &limiterStub{}
	service := NewService(repository, limiter)
	secret := "harness_mcp_secret"
	digest := sha256.Sum256([]byte(secret))
	developer := GatewayConnection{Connection: Connection{ID: "conn-1", Purpose: "developer", AuthMode: "bearer", RequestsPerMinute: 60}, SecretHash: digest[:]}
	if _, err := service.AuthorizeDeveloper(context.Background(), developer, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("错误开发者密钥未被拒绝: %v", err)
	}
	caller, err := service.AuthorizeDeveloper(context.Background(), developer, secret)
	if err != nil || caller.UserID != "" || limiter.key != "mcp:conn-1" {
		t.Fatalf("开发者身份或限流键不正确: %#v, %s, %v", caller, limiter.key, err)
	}
	harness := GatewayConnection{Connection: Connection{ID: "conn-2", Purpose: "harness", AuthMode: "oidc", RequestsPerMinute: 60}, OrganizationID: "org-1"}
	caller, err = service.AuthorizeUser(context.Background(), harness, "org-1", "user-1")
	if err != nil || caller.UserID != "user-1" || caller.DepartmentID != "department-1" || limiter.key != "mcp:conn-2:user:user-1" {
		t.Fatalf("员工身份或限流键不正确: %#v, %s, %v", caller, limiter.key, err)
	}
	repository.allowed = false
	if _, err := service.AuthorizeUser(context.Background(), harness, "org-1", "user-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("撤权员工仍可调用: %v", err)
	}
}

func TestExpiredConnectionIsRejected(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	repository := &repositoryStub{item: GatewayConnection{Connection: Connection{Code: "expired", Status: "active", ExpiresAt: &expired}, ServiceStatus: "active"}}
	service := NewService(repository, &limiterStub{})
	if _, err := service.Lookup(context.Background(), "expired"); !errors.Is(err, ErrInactive) {
		t.Fatalf("过期连接仍可查得路由: %v", err)
	}
}

type healthProberStub struct {
	credentials mcpadmin.UpstreamCredentials
	tools       []mcpadmin.Tool
	err         error
	called      bool
}

func (p *healthProberStub) ListTools(_ context.Context, _ mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error) {
	p.called, p.credentials = true, credentials
	return p.tools, p.err
}

func TestConnectionHealthUsesOwnCredentialsAndDoesNotCallTools(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	repository := &repositoryStub{item: GatewayConnection{
		Connection: Connection{ID: id, Status: "active"}, OrganizationID: "org-1", ServiceStatus: "active",
		Transport: "streamable_http", UpstreamURL: "https://example.com/mcp",
		ServiceParameters:  []mcpadmin.ParameterValue{{Location: "header", Name: "X-API-Key", Value: "old"}},
		UpstreamParameters: []mcpadmin.ParameterValue{{Location: "header", Name: "X-API-Key", Value: "current"}},
		Tools:              []mcpadmin.Tool{{Name: "search"}},
	}}
	prober := &healthProberStub{tools: []mcpadmin.Tool{{Name: "search"}}}
	result, err := NewService(repository, &limiterStub{}).CheckHealth(context.Background(), "org-1", id, prober)
	if err != nil || result.Status != "healthy" || result.ToolCount != 1 || !prober.called ||
		len(prober.credentials.AllParameters()) != 1 || prober.credentials.AllParameters()[0].Value != "current" {
		t.Fatalf("连接健康检查未使用此连接的参数: result=%+v credentials=%+v err=%v", result, prober.credentials, err)
	}
	if _, err := NewService(repository, &limiterStub{}).CheckHealth(context.Background(), "other-org", id, prober); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他企业不应查询此连接: %v", err)
	}
}

func TestConnectionHealthDistinguishesInactiveAndFailedHandshake(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	repository := &repositoryStub{item: GatewayConnection{Connection: Connection{ID: id, Status: "disabled"}, OrganizationID: "org-1"}}
	service := NewService(repository, &limiterStub{})
	prober := &healthProberStub{}
	result, err := service.CheckHealth(context.Background(), "org-1", id, prober)
	if err != nil || result.Status != "inactive" || prober.called {
		t.Fatalf("停用连接不应探测上游: %+v %v", result, err)
	}
	repository.item.Status, repository.item.ServiceStatus = "active", "active"
	repository.item.Transport = "streamable_http"
	prober.err = errors.New("upstream secret should not be returned")
	result, err = service.CheckHealth(context.Background(), "org-1", id, prober)
	if err != nil || result.Status != "unavailable" || strings.Contains(result.Message, "secret") {
		t.Fatalf("上游错误应返回安全的连接状态: %+v %v", result, err)
	}
}
