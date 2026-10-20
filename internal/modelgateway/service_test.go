package modelgateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type repositoryStub struct {
	createdInput CreateAPIKeyInput
	apiKey       APIKey
	route        Route
	touchedID    string
	usageQuery   UsageQuery
	recorded     RecordCallInput
}

type requestLimiterStub struct {
	allowed bool
	keyID   string
	maximum int
}

func (l *requestLimiterStub) Allow(_ context.Context, keyID string, maximum int) (bool, error) {
	l.keyID = keyID
	l.maximum = maximum
	return l.allowed, nil
}

func (*repositoryStub) ListAPIKeys(context.Context, string) ([]APIKey, error) { return nil, nil }
func (r *repositoryStub) CreateAPIKey(_ context.Context, input CreateAPIKeyInput) (APIKey, error) {
	r.createdInput = input
	return APIKey{ID: "key-1", OrganizationID: input.OrganizationID, Name: input.Name, KeyPrefix: input.KeyPrefix, Status: "active"}, nil
}
func (*repositoryStub) UpdateAPIKey(context.Context, UpdateAPIKeyInput) (APIKey, error) {
	return APIKey{}, nil
}
func (*repositoryStub) RevokeAPIKey(context.Context, RevokeAPIKeyInput) (APIKey, error) {
	return APIKey{}, nil
}
func (*repositoryStub) RevealAPIKeySecret(context.Context, RevealAPIKeySecretInput) (string, error) {
	return "harness_sk_revealed", nil
}
func (r *repositoryStub) FindAPIKeyByHash(context.Context, []byte) (APIKey, error) {
	if r.apiKey.ID == "" {
		return APIKey{}, ErrAPIKeyNotFound
	}
	return r.apiKey, nil
}
func (r *repositoryStub) TouchAPIKey(_ context.Context, id string) error {
	r.touchedID = id
	return nil
}
func (r *repositoryStub) FindRoute(context.Context, string, string) (Route, error) {
	if r.route.ModelID == "" {
		return Route{}, ErrModelNotFound
	}
	return r.route, nil
}
func (r *repositoryStub) RecordCall(_ context.Context, input RecordCallInput) error {
	r.recorded = input
	return nil
}
func (*repositoryStub) ListCalls(context.Context, CallQuery) (CallPage, error) {
	return CallPage{}, nil
}
func (r *repositoryStub) ListUsage(_ context.Context, query UsageQuery) (UsagePage, error) {
	r.usageQuery = query
	return UsagePage{}, nil
}

func TestUsageQueryUsesUTCMonthAndValidatesDimensions(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, nil, nil)
	service.now = func() time.Time { return time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC) }
	if _, err := service.ListUsage(context.Background(), UsageQuery{OrganizationID: "org-1"}); err != nil {
		t.Fatal(err)
	}
	if repository.usageQuery.Month != "2026-11" || repository.usageQuery.GroupBy != "department" || repository.usageQuery.Page != 1 {
		t.Fatalf("默认统计条件错误: %#v", repository.usageQuery)
	}
	for _, query := range []UsageQuery{
		{OrganizationID: "org-1", Month: "2026-13", GroupBy: "model"},
		{OrganizationID: "org-1", Month: "2026-10", GroupBy: "api_key"},
		{OrganizationID: "org-1", Month: "2026-10", GroupBy: "user", PageSize: 101},
	} {
		if _, err := service.ListUsage(context.Background(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("无效统计条件应拒绝: %#v, err=%v", query, err)
		}
	}
}

func TestRecordCallDistinguishesReportedUsage(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, nil, nil)
	input := RecordCallInput{OrganizationID: "org-1", APIKeyID: "key-1", AuthType: "api_key",
		ModelID: "model-1", ProviderID: "provider-1", RequestID: "request-1", Result: "success", HTTPStatus: 200}
	if err := service.RecordCall(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if repository.recorded.UsageReported {
		t.Fatal("没有上游 usage 时不应标为已统计")
	}
	input.TotalTokens = 8
	if err := service.RecordCall(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if !repository.recorded.UsageReported {
		t.Fatal("上游有 Token 用量时应标为已统计")
	}
}

func TestCreateAPIKeyPersistsHashAndEncryptedSecretInput(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, nil, &policyRepositoryStub{})
	service.now = func() time.Time { return time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC) }
	service.newSecret = func() (string, error) { return "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789", nil }

	created, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		OrganizationID: "org-1", ActorID: "user-1", Name: "生产调用",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Secret != "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789" {
		t.Fatalf("返回的调用密钥不正确: %q", created.Secret)
	}
	expected := sha256.Sum256([]byte(created.Secret))
	if string(repository.createdInput.SecretHash) != string(expected[:]) {
		t.Fatal("数据库输入未保存完整密钥的 SHA-256 哈希")
	}
	if repository.createdInput.KeyPrefix != "harness_sk_abcdefg" {
		t.Fatalf("可见前缀 = %q", repository.createdInput.KeyPrefix)
	}
	if repository.createdInput.Secret != created.Secret {
		t.Fatal("仓储输入未收到用于加密保存的完整密钥")
	}
}

func TestPrepareRewritesModelCodeToUpstreamModelID(t *testing.T) {
	repository := &repositoryStub{
		apiKey: APIKey{ID: "key-1", OrganizationID: "org-1", Status: "active"},
		route: Route{
			ModelID: "model-1", ModelCode: "deepseek_chat", ModelStatus: "active", UpstreamModelID: "deepseek-chat",
			ProviderID: "provider-1", ProviderStatus: "active", ProviderBaseURL: "https://api.deepseek.com", ProviderAPIKey: "secret", ProviderTimeoutMS: 15000,
		},
	}
	service := NewService(repository, nil, &policyRepositoryStub{})
	prepared, err := service.Prepare(context.Background(), "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789", []byte(`{"model":"DEEPSEEK_CHAT","messages":[],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(prepared.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "deepseek-chat" || !prepared.Streaming {
		t.Fatalf("转发请求未正确改写: %#v", body)
	}
	if repository.touchedID != "key-1" {
		t.Fatalf("未更新调用密钥使用时间: %q", repository.touchedID)
	}
}

func TestPrepareForUserKeepsOIDCCallerIdentity(t *testing.T) {
	repository := &repositoryStub{route: Route{
		ModelID: "model-1", ModelCode: "main", ModelStatus: "active", UpstreamModelID: "upstream-main",
		ProviderID: "provider-1", ProviderStatus: "active",
	}}
	prepared, err := NewService(repository, nil, &policyRepositoryStub{}).PrepareForUser(
		context.Background(), "org-1", "user-1", []byte(`{"model":"main","messages":[]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Caller.AuthType != "user" || prepared.Caller.OrganizationID != "org-1" || prepared.Caller.UserID != "user-1" || prepared.Caller.APIKeyID != "" {
		t.Fatalf("OIDC 调用身份不完整: %#v", prepared.Caller)
	}
}

func TestPrepareRejectsRevokedAndExpiredKeys(t *testing.T) {
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	repository := &repositoryStub{apiKey: APIKey{ID: "key-1", OrganizationID: "org-1", Status: "revoked"}}
	service := NewService(repository, nil, &policyRepositoryStub{})
	service.now = func() time.Time { return now }
	_, err := service.Prepare(context.Background(), "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789", []byte(`{"model":"main"}`))
	if !errors.Is(err, ErrAPIKeyRevoked) {
		t.Fatalf("已吊销密钥错误 = %v", err)
	}

	expiredAt := now.Add(-time.Minute)
	repository.apiKey = APIKey{ID: "key-1", OrganizationID: "org-1", Status: "active", ExpiresAt: &expiredAt}
	_, err = service.Prepare(context.Background(), "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789", []byte(`{"model":"main"}`))
	if !errors.Is(err, ErrAPIKeyExpired) {
		t.Fatalf("已过期密钥错误 = %v", err)
	}
}

func TestPrepareEnforcesModelScopeRateLimitAndMonthlyQuota(t *testing.T) {
	credential := "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789"
	body := []byte(`{"model":"main","messages":[]}`)
	repository := &repositoryStub{apiKey: APIKey{
		ID: "key-1", OrganizationID: "org-1", Status: "active", AllowedModelCodes: []string{"other"},
	}}
	service := NewService(repository, nil, &policyRepositoryStub{})
	if _, err := service.Prepare(context.Background(), credential, body); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("模型范围错误 = %v", err)
	}

	repository.apiKey.AllowedModelCodes = nil
	repository.apiKey.MonthlyTokenLimit = 100
	repository.apiKey.MonthlyTokenUsed = 100
	if _, err := service.Prepare(context.Background(), credential, body); !errors.Is(err, ErrMonthlyQuota) {
		t.Fatalf("月度额度错误 = %v", err)
	}

	repository.apiKey.MonthlyTokenLimit = 0
	repository.apiKey.RequestsPerMinute = 2
	limiter := &requestLimiterStub{allowed: false}
	service = NewService(repository, limiter, &policyRepositoryStub{})
	if _, err := service.Prepare(context.Background(), credential, body); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("每分钟限流错误 = %v", err)
	}
	if limiter.keyID != "key-1" || limiter.maximum != 2 {
		t.Fatalf("限流参数不正确: %#v", limiter)
	}
}

func TestCreateAPIKeyNormalizesAllowedModelCodes(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, nil, &policyRepositoryStub{})
	service.newSecret = func() (string, error) { return "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789", nil }
	_, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		OrganizationID: "org-1", ActorID: "user-1", Name: "生产调用",
		AllowedModelCodes: []string{" Main ", "backup", "MAIN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"backup", "main"}
	if len(repository.createdInput.AllowedModelCodes) != len(want) {
		t.Fatalf("模型范围 = %#v", repository.createdInput.AllowedModelCodes)
	}
	for index := range want {
		if repository.createdInput.AllowedModelCodes[index] != want[index] {
			t.Fatalf("模型范围 = %#v", repository.createdInput.AllowedModelCodes)
		}
	}
}
