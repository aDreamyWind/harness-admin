package organizationadmin

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	updated NameUpdate
}

func (*repositoryStub) FindOrganization(_ context.Context, organizationID string) (Organization, error) {
	return Organization{ID: organizationID, Code: "default", Name: "默认企业"}, nil
}

func (r *repositoryStub) UpdateOrganization(_ context.Context, input NameUpdate) (Organization, error) {
	r.updated = input
	return Organization{ID: input.OrganizationID, Code: "default", Name: input.Name}, nil
}

func TestGetRejectsMissingOrganization(t *testing.T) {
	service := NewService(&repositoryStub{})
	if _, err := service.Get(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("错误 = %v，期望 ErrInvalidInput", err)
	}
}

func TestUpdateNormalizesOrganizationName(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	organization, err := service.Update(context.Background(), UpdateInput{
		OrganizationID: "org-1",
		ActorID:        "user-1",
		Name:           "  星河科技  ",
		RequestID:      "request-1",
		SourceIP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if organization.Name != "星河科技" || repository.updated.Name != "星河科技" {
		t.Fatalf("企业名称未规范化: organization=%#v input=%#v", organization, repository.updated)
	}
	if repository.updated.ActorID != "user-1" || repository.updated.RequestID != "request-1" {
		t.Fatalf("修改企业资料参数不完整: %#v", repository.updated)
	}
}

func TestUpdateRejectsInvalidName(t *testing.T) {
	service := NewService(&repositoryStub{})
	for _, name := range []string{"", "   "} {
		if _, err := service.Update(context.Background(), UpdateInput{
			OrganizationID: "org-1", ActorID: "user-1", Name: name,
		}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("名称 %q 的错误 = %v，期望 ErrInvalidInput", name, err)
		}
	}
}
