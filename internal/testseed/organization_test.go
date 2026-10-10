package testseed

import (
	"context"
	"errors"
	"testing"
)

type fakeOrganizationRepository struct {
	input       OrganizationInput
	departments []Department
	result      OrganizationResult
	err         error
}

func (f *fakeOrganizationRepository) SeedOrganization(_ context.Context, input OrganizationInput, departments []Department) (OrganizationResult, error) {
	f.input = input
	f.departments = departments
	return f.result, f.err
}

func TestSeedOrganizationUsesEnterpriseTree(t *testing.T) {
	repository := &fakeOrganizationRepository{result: OrganizationResult{OrganizationID: "org-1", CreatedCount: 17}}
	service := NewService(repository)

	result, err := service.SeedOrganization(context.Background(), OrganizationInput{
		OrganizationCode: " Default ",
		ActorUsername:    " Admin ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedCount != 17 || len(repository.departments) != 17 {
		t.Fatalf("测试组织树数量不正确: result=%#v departments=%d", result, len(repository.departments))
	}
	if repository.input.OrganizationCode != "default" || repository.input.ActorUsername != "admin" {
		t.Fatalf("测试组织参数未规范化: %#v", repository.input)
	}
	if err := validateOrganizationTree(repository.departments); err != nil {
		t.Fatalf("测试组织树结构不合法: %v", err)
	}
}

func TestSeedOrganizationRejectsEmptyInput(t *testing.T) {
	service := NewService(&fakeOrganizationRepository{})

	_, err := service.SeedOrganization(context.Background(), OrganizationInput{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("期望参数错误，实际为 %v", err)
	}
}
