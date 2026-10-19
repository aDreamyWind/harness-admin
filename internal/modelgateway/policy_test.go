package modelgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type policyRepositoryStub struct {
	snapshot UserPolicySnapshot
	models   []AvailableModel
	saved    SavePolicyInput
	batch    SaveDepartmentPoliciesInput
}

func (*policyRepositoryStub) ListPolicies(context.Context, string) ([]SubjectPolicy, error) {
	return nil, nil
}
func (p *policyRepositoryStub) SavePolicy(_ context.Context, input SavePolicyInput) (SubjectPolicy, error) {
	p.saved = input
	return SubjectPolicy{ScopeMode: input.ScopeMode, AllowedModelCodes: input.AllowedModelCodes}, nil
}
func (p *policyRepositoryStub) SaveDepartmentPolicies(_ context.Context, input SaveDepartmentPoliciesInput) ([]SubjectPolicy, error) {
	p.batch = input
	return []SubjectPolicy{{SubjectType: "department", SubjectID: input.SubjectIDs[0]}}, nil
}
func (*policyRepositoryStub) DeletePolicy(context.Context, DeletePolicyInput) error { return nil }
func (p *policyRepositoryStub) FindUserPolicy(context.Context, string, string) (UserPolicySnapshot, error) {
	return p.snapshot, nil
}
func (p *policyRepositoryStub) ListAvailableModels(context.Context, string) ([]AvailableModel, error) {
	return p.models, nil
}

func activeRoute() Route {
	return Route{ModelID: "model-1", ModelCode: "main", ModelStatus: "active", UpstreamModelID: "upstream-main", ProviderID: "provider-1", ProviderStatus: "active"}
}

func TestUserModelScopeInheritsAndOverridesDepartment(t *testing.T) {
	repository := &repositoryStub{route: activeRoute()}
	policy := &policyRepositoryStub{snapshot: UserPolicySnapshot{
		DepartmentID: "dept-1", DepartmentPolicy: &SubjectPolicy{SubjectType: "department", SubjectID: "dept-1", ScopeMode: "selected", AllowedModelCodes: []string{"other"}},
	}}
	service := NewService(repository, nil, policy)
	request := []byte(`{"model":"main","messages":[]}`)
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("部门模型范围未生效: %v", err)
	}
	policy.snapshot.UserPolicy = &SubjectPolicy{SubjectType: "user", SubjectID: "user-1", ScopeMode: "all"}
	prepared, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request)
	if err != nil || prepared.Caller.DepartmentID != "dept-1" {
		t.Fatalf("用户覆盖部门后应允许调用: prepared=%#v, err=%v", prepared, err)
	}
	policy.snapshot.UserPolicy.ScopeMode = "none"
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("用户禁用全部模型未生效: %v", err)
	}
}

func TestUserDepartmentAndPersonalQuotasBothApply(t *testing.T) {
	policy := &policyRepositoryStub{snapshot: UserPolicySnapshot{
		DepartmentID:     "dept-1",
		DepartmentPolicy: &SubjectPolicy{SubjectType: "department", SubjectID: "dept-1", ScopeMode: "all", MonthlyTokenLimit: 100, MonthlyTokenUsed: 100},
		UserPolicy:       &SubjectPolicy{SubjectType: "user", SubjectID: "user-1", ScopeMode: "inherit", MonthlyTokenLimit: 500},
	}}
	service := NewService(&repositoryStub{route: activeRoute()}, nil, policy)
	request := []byte(`{"model":"main","messages":[]}`)
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrMonthlyQuota) {
		t.Fatalf("部门共享额度未生效: %v", err)
	}
	policy.snapshot.DepartmentPolicy.MonthlyTokenUsed = 0
	policy.snapshot.UserPolicy.MonthlyTokenUsed = 500
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrMonthlyQuota) {
		t.Fatalf("用户个人额度未生效: %v", err)
	}
	policy.snapshot.UserPolicy.MonthlyTokenUsed = 0
	policy.snapshot.DepartmentPolicy.RequestsPerMinute = 1
	limiter := &requestLimiterStub{allowed: false}
	service = NewService(&repositoryStub{route: activeRoute()}, limiter, policy)
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("部门按人限流未生效: %v", err)
	}
	if limiter.keyID != "policy:org-1:department:dept-1:user:user-1" {
		t.Fatalf("限流维度错误: %q", limiter.keyID)
	}
}

func TestDepartmentPolicyOnlyAppliesToDirectDepartment(t *testing.T) {
	policy := &policyRepositoryStub{snapshot: UserPolicySnapshot{
		DepartmentID: "child",
	}}
	service := NewService(&repositoryStub{route: activeRoute()}, nil, policy)
	request := []byte(`{"model":"main","messages":[]}`)
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); err != nil {
		t.Fatalf("未配置直属部门策略时不应套用上级策略: %v", err)
	}
	policy.snapshot.DepartmentPolicy = &SubjectPolicy{SubjectType: "department", SubjectID: "child", ScopeMode: "selected", AllowedModelCodes: []string{"other"}}
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("直属部门的模型范围未生效: %v", err)
	}
}

func TestDepartmentPerUserTokenAndRPM(t *testing.T) {
	policy := &policyRepositoryStub{snapshot: UserPolicySnapshot{
		DepartmentID: "child",
		DepartmentPolicy: &SubjectPolicy{
			SubjectType: "department", SubjectID: "child", ScopeMode: "all", MonthlyTokenMode: "per_user",
			MonthlyTokenLimit: 100, MonthlyTokenUsed: 100,
		},
	}}
	service := NewService(&repositoryStub{route: activeRoute()}, nil, policy)
	request := []byte(`{"model":"main","messages":[]}`)
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrMonthlyQuota) || !strings.Contains(err.Error(), "个人额度") {
		t.Fatalf("部门按用户额度未生效: %v", err)
	}
	policy.snapshot.DepartmentPolicy.MonthlyTokenUsed = 0
	policy.snapshot.DepartmentPolicy.RequestsPerMinute = 1
	limiter := &requestLimiterStub{allowed: false}
	service = NewService(&repositoryStub{route: activeRoute()}, limiter, policy)
	if _, err := service.PrepareForUser(context.Background(), "org-1", "user-1", request); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("部门每人限流未生效: %v", err)
	}
	if limiter.keyID != "policy:org-1:department:child:user:user-1" {
		t.Fatalf("部门每人限流键错误: %q", limiter.keyID)
	}
}

func TestAvailableModelsFilteredByUserAndKeyScope(t *testing.T) {
	policy := &policyRepositoryStub{
		snapshot: UserPolicySnapshot{UserPolicy: &SubjectPolicy{ScopeMode: "selected", AllowedModelCodes: []string{"main"}}},
		models:   []AvailableModel{{Code: "main", CreatedAt: time.Now()}, {Code: "other", CreatedAt: time.Now()}},
	}
	repository := &repositoryStub{apiKey: APIKey{ID: "key-1", OrganizationID: "org-1", Status: "active", AllowedModelCodes: []string{"other"}}}
	service := NewService(repository, nil, policy)
	userModels, err := service.ListModelsForUser(context.Background(), "org-1", "user-1")
	if err != nil || len(userModels) != 1 || userModels[0].Code != "main" {
		t.Fatalf("用户模型目录过滤错误: %#v, err=%v", userModels, err)
	}
	keyModels, err := service.ListModelsForAPIKey(context.Background(), "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789")
	if err != nil || len(keyModels) != 1 || keyModels[0].Code != "other" {
		t.Fatalf("密钥模型目录过滤错误: %#v, err=%v", keyModels, err)
	}
}

func TestPolicyServiceRequiresSelectedModelsAndNormalizesCodes(t *testing.T) {
	repo := &policyRepositoryStub{}
	service := NewPolicyService(repo)
	input := SavePolicyInput{OrganizationID: "org-1", ActorID: "admin-1", SubjectType: "user", SubjectID: "user-1", ScopeMode: "selected"}
	if _, err := service.Save(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("未选择模型应拒绝: %v", err)
	}
	input.AllowedModelCodes = []string{" Main ", "main"}
	if _, err := service.Save(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(repo.saved.AllowedModelCodes) != 1 || repo.saved.AllowedModelCodes[0] != "main" {
		t.Fatalf("模型编码未归一化: %#v", repo.saved.AllowedModelCodes)
	}
}

func TestPolicyServiceValidatesBatchAndTokenMode(t *testing.T) {
	repo := &policyRepositoryStub{}
	service := NewPolicyService(repo)
	input := SaveDepartmentPoliciesInput{
		OrganizationID: "org-1", ActorID: "admin-1", SubjectIDs: []string{"dept-1", "dept-1"},
		ScopeMode: "all", MonthlyTokenMode: "per_user",
	}
	if _, err := service.SaveDepartments(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("重复部门应拒绝: %v", err)
	}
	input.SubjectIDs = []string{"dept-1"}
	if _, err := service.SaveDepartments(context.Background(), input); err != nil || repo.batch.MonthlyTokenMode != "per_user" {
		t.Fatalf("按人额度的批量保存失败: input=%#v err=%v", repo.batch, err)
	}
	if _, err := service.Save(context.Background(), SavePolicyInput{
		OrganizationID: "org-1", ActorID: "admin-1", SubjectType: "user", SubjectID: "user-1",
		ScopeMode: "all", MonthlyTokenMode: "per_user",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("用户策略不应设置部门额度模式: %v", err)
	}
	input.ScopeMode = "inherit"
	if _, err := service.SaveDepartments(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("部门策略不应隐式继承上级部门: %v", err)
	}
}
