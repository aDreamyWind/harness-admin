package modelgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPolicySubjectNotFound = errors.New("策略所属部门或用户不存在")
var ErrPolicyNotFound = errors.New("模型策略不存在")

type SubjectPolicy struct {
	OrganizationID    string
	SubjectType       string
	SubjectID         string
	SubjectName       string
	ScopeMode         string
	AllowedModelCodes []string
	RequestsPerMinute int
	MonthlyTokenLimit int64
	MonthlyTokenMode  string
	MonthlyTokenUsed  int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SavePolicyInput struct {
	OrganizationID    string
	ActorID           string
	SubjectType       string
	SubjectID         string
	ScopeMode         string
	AllowedModelCodes []string
	RequestsPerMinute int
	MonthlyTokenLimit int64
	MonthlyTokenMode  string
	RequestID         string
	SourceIP          string
}

type DeletePolicyInput struct {
	OrganizationID string
	ActorID        string
	SubjectType    string
	SubjectID      string
	RequestID      string
	SourceIP       string
}

type UserPolicySnapshot struct {
	DepartmentID     string
	DepartmentPolicy *SubjectPolicy
	UserPolicy       *SubjectPolicy
}

type SaveDepartmentPoliciesInput struct {
	OrganizationID    string
	ActorID           string
	SubjectIDs        []string
	ScopeMode         string
	AllowedModelCodes []string
	RequestsPerMinute int
	MonthlyTokenLimit int64
	MonthlyTokenMode  string
	RequestID         string
	SourceIP          string
}

type AvailableModel struct {
	Code            string
	Name            string
	ModelType       string
	Capabilities    []string
	CreatedAt       time.Time
	UpstreamModelID string `json:"-"`
	ProviderKind    string `json:"-"`
}

type PolicyRepository interface {
	ListPolicies(context.Context, string) ([]SubjectPolicy, error)
	SavePolicy(context.Context, SavePolicyInput) (SubjectPolicy, error)
	SaveDepartmentPolicies(context.Context, SaveDepartmentPoliciesInput) ([]SubjectPolicy, error)
	DeletePolicy(context.Context, DeletePolicyInput) error
	FindUserPolicy(context.Context, string, string) (UserPolicySnapshot, error)
	ListAvailableModels(context.Context, string) ([]AvailableModel, error)
}

type PolicyService struct{ repository PolicyRepository }

func NewPolicyService(repository PolicyRepository) *PolicyService {
	return &PolicyService{repository: repository}
}

func (s *PolicyService) List(ctx context.Context, organizationID string) ([]SubjectPolicy, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	items, err := s.repository.ListPolicies(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询模型策略失败: %w", err)
	}
	return items, nil
}

func (s *PolicyService) Save(ctx context.Context, input SavePolicyInput) (SubjectPolicy, error) {
	input.SubjectType = strings.TrimSpace(input.SubjectType)
	input.ScopeMode = strings.TrimSpace(input.ScopeMode)
	input.AllowedModelCodes = normalizeOptionalModelCodes(input.AllowedModelCodes)
	if input.MonthlyTokenMode == "" {
		input.MonthlyTokenMode = "shared"
	}
	if !validPolicySubject(input.OrganizationID, input.SubjectType, input.SubjectID) || input.ActorID == "" ||
		input.RequestsPerMinute < 0 || input.RequestsPerMinute > 100000 || input.MonthlyTokenLimit < 0 || len(input.AllowedModelCodes) > 100 ||
		!validScopeMode(input.ScopeMode, input.AllowedModelCodes) || (input.SubjectType == "department" && input.ScopeMode == "inherit") ||
		!validTokenMode(input.SubjectType, input.MonthlyTokenMode) {
		return SubjectPolicy{}, ErrInvalidInput
	}
	for _, code := range input.AllowedModelCodes {
		if !gatewayModelCodePattern.MatchString(code) {
			return SubjectPolicy{}, ErrInvalidInput
		}
	}
	item, err := s.repository.SavePolicy(ctx, input)
	if err != nil {
		if errors.Is(err, ErrPolicySubjectNotFound) || errors.Is(err, ErrModelNotFound) {
			return SubjectPolicy{}, err
		}
		return SubjectPolicy{}, fmt.Errorf("保存模型策略失败: %w", err)
	}
	return item, nil
}

func (s *PolicyService) SaveDepartments(ctx context.Context, input SaveDepartmentPoliciesInput) ([]SubjectPolicy, error) {
	input.ScopeMode = strings.TrimSpace(input.ScopeMode)
	input.AllowedModelCodes = normalizeOptionalModelCodes(input.AllowedModelCodes)
	if len(input.SubjectIDs) == 0 || len(input.SubjectIDs) > 10000 || input.OrganizationID == "" || input.ActorID == "" ||
		input.RequestsPerMinute < 0 || input.RequestsPerMinute > 100000 || input.MonthlyTokenLimit < 0 || len(input.AllowedModelCodes) > 100 ||
		!validScopeMode(input.ScopeMode, input.AllowedModelCodes) || input.ScopeMode == "inherit" ||
		!validTokenMode("department", input.MonthlyTokenMode) {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(input.SubjectIDs))
	for _, id := range input.SubjectIDs {
		if id == "" {
			return nil, ErrInvalidInput
		}
		if _, exists := seen[id]; exists {
			return nil, ErrInvalidInput
		}
		seen[id] = struct{}{}
	}
	for _, code := range input.AllowedModelCodes {
		if !gatewayModelCodePattern.MatchString(code) {
			return nil, ErrInvalidInput
		}
	}
	items, err := s.repository.SaveDepartmentPolicies(ctx, input)
	if err != nil {
		if errors.Is(err, ErrPolicySubjectNotFound) || errors.Is(err, ErrModelNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("批量保存部门模型策略失败: %w", err)
	}
	return items, nil
}

func (s *PolicyService) Delete(ctx context.Context, input DeletePolicyInput) error {
	if !validPolicySubject(input.OrganizationID, input.SubjectType, input.SubjectID) || input.ActorID == "" {
		return ErrInvalidInput
	}
	if err := s.repository.DeletePolicy(ctx, input); err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			return err
		}
		return fmt.Errorf("删除模型策略失败: %w", err)
	}
	return nil
}

func validPolicySubject(organizationID, subjectType, subjectID string) bool {
	return organizationID != "" && subjectID != "" && (subjectType == "department" || subjectType == "user")
}

func normalizeOptionalModelCodes(values []string) []string {
	if values == nil {
		return nil
	}
	return normalizeModelCodes(values)
}

func validScopeMode(mode string, codes []string) bool {
	if mode == "selected" {
		return len(codes) > 0
	}
	return (mode == "inherit" || mode == "all" || mode == "none") && len(codes) == 0
}

func validTokenMode(subjectType, mode string) bool {
	return mode == "shared" || (subjectType == "department" && mode == "per_user")
}

func effectiveUserModels(snapshot UserPolicySnapshot) []string {
	if snapshot.UserPolicy != nil && snapshot.UserPolicy.ScopeMode != "inherit" {
		return policyScopeCodes(snapshot.UserPolicy)
	}
	if snapshot.DepartmentPolicy != nil {
		return policyScopeCodes(snapshot.DepartmentPolicy)
	}
	return nil
}

func policyScopeCodes(policy *SubjectPolicy) []string {
	switch policy.ScopeMode {
	case "none":
		return []string{}
	case "selected":
		return policy.AllowedModelCodes
	default:
		return nil
	}
}

func policyModelAllowed(allowed []string, modelCode string) bool {
	if allowed == nil {
		return true
	}
	return len(allowed) > 0 && modelAllowed(allowed, modelCode)
}
