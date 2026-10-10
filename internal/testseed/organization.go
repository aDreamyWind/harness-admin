package testseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidInput         = errors.New("测试组织参数不正确")
	ErrOrganizationNotFound = errors.New("企业不存在，请先初始化管理员")
	ErrActorNotFound        = errors.New("初始化操作人不存在")
)

type Department struct {
	Code       string
	Name       string
	ParentCode string
	SortOrder  int
}

type OrganizationInput struct {
	OrganizationCode string
	ActorUsername    string
}

type OrganizationResult struct {
	OrganizationID string
	CreatedCount   int
	UpdatedCount   int
}

type OrganizationRepository interface {
	SeedOrganization(context.Context, OrganizationInput, []Department) (OrganizationResult, error)
}

type Service struct {
	repository OrganizationRepository
}

func NewService(repository OrganizationRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) SeedOrganization(ctx context.Context, input OrganizationInput) (OrganizationResult, error) {
	input.OrganizationCode = strings.ToLower(strings.TrimSpace(input.OrganizationCode))
	input.ActorUsername = strings.ToLower(strings.TrimSpace(input.ActorUsername))
	if input.OrganizationCode == "" || input.ActorUsername == "" {
		return OrganizationResult{}, ErrInvalidInput
	}

	departments := defaultOrganizationTree()
	if err := validateOrganizationTree(departments); err != nil {
		return OrganizationResult{}, fmt.Errorf("测试组织树定义无效: %w", err)
	}
	result, err := s.repository.SeedOrganization(ctx, input, departments)
	if err != nil {
		return OrganizationResult{}, fmt.Errorf("写入测试组织树失败: %w", err)
	}
	return result, nil
}

func defaultOrganizationTree() []Department {
	return []Department{
		{Code: "headquarters", Name: "集团总部", SortOrder: 10},
		{Code: "executive-office", Name: "总经理办公室", ParentCode: "headquarters", SortOrder: 10},
		{Code: "human-resources", Name: "人力资源中心", ParentCode: "headquarters", SortOrder: 20},
		{Code: "talent-acquisition", Name: "招聘与员工关系部", ParentCode: "human-resources", SortOrder: 10},
		{Code: "compensation-performance", Name: "薪酬绩效部", ParentCode: "human-resources", SortOrder: 20},
		{Code: "finance", Name: "财务中心", ParentCode: "headquarters", SortOrder: 30},
		{Code: "product", Name: "产品中心", ParentCode: "headquarters", SortOrder: 40},
		{Code: "engineering", Name: "研发中心", ParentCode: "headquarters", SortOrder: 50},
		{Code: "backend", Name: "后端研发部", ParentCode: "engineering", SortOrder: 10},
		{Code: "frontend", Name: "前端研发部", ParentCode: "engineering", SortOrder: 20},
		{Code: "quality-assurance", Name: "测试与质量部", ParentCode: "engineering", SortOrder: 30},
		{Code: "platform-engineering", Name: "运维与平台部", ParentCode: "engineering", SortOrder: 40},
		{Code: "marketing-sales", Name: "营销中心", ParentCode: "headquarters", SortOrder: 60},
		{Code: "marketing", Name: "市场部", ParentCode: "marketing-sales", SortOrder: 10},
		{Code: "sales", Name: "销售部", ParentCode: "marketing-sales", SortOrder: 20},
		{Code: "customer-success", Name: "客户成功部", ParentCode: "marketing-sales", SortOrder: 30},
		{Code: "legal-compliance", Name: "法务与合规部", ParentCode: "headquarters", SortOrder: 70},
	}
}

func validateOrganizationTree(departments []Department) error {
	seen := make(map[string]struct{}, len(departments))
	for _, department := range departments {
		if department.Code == "" || department.Name == "" {
			return fmt.Errorf("部门代码和名称不能为空")
		}
		if _, exists := seen[department.Code]; exists {
			return fmt.Errorf("部门代码重复: %s", department.Code)
		}
		if department.ParentCode != "" {
			if _, exists := seen[department.ParentCode]; !exists {
				return fmt.Errorf("部门 %s 的上级部门 %s 尚未定义", department.Code, department.ParentCode)
			}
		}
		seen[department.Code] = struct{}{}
	}
	return nil
}
