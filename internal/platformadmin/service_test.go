package platformadmin

import (
	"context"
	"testing"
	"time"
)

type repositoryStub struct {
	admin            Admin
	createdInput     OrganizationCreation
	created          Organization
	organizationList []Organization
}

func (s *repositoryStub) FindAdminForLogin(context.Context, string) (Admin, error) {
	return s.admin, nil
}
func (s *repositoryStub) SaveLogin(context.Context, NewSession, AuditEvent) error { return nil }
func (s *repositoryStub) FindSessionByTokenHash(context.Context, string) (Session, error) {
	return Session{}, nil
}
func (s *repositoryStub) RevokeSession(context.Context, string, AuditEvent) error { return nil }
func (s *repositoryStub) RecordAudit(context.Context, AuditEvent) error           { return nil }
func (s *repositoryStub) ListOrganizations(context.Context) ([]Organization, error) {
	return s.organizationList, nil
}
func (s *repositoryStub) CreateOrganization(_ context.Context, input OrganizationCreation) (Organization, error) {
	s.createdInput = input
	return s.created, nil
}
func (s *repositoryStub) UpdateOrganizationStatus(context.Context, StatusUpdate) (Organization, error) {
	return Organization{}, nil
}
func (s *repositoryStub) BootstrapPlatformAdmin(context.Context, BootstrapInput) (BootstrapResult, error) {
	return BootstrapResult{}, nil
}

type limiterStub struct{}

func (limiterStub) Allow(context.Context, string) (bool, error)                { return true, nil }
func (limiterStub) RecordFailure(context.Context, string) (int64, bool, error) { return 1, false, nil }
func (limiterStub) Clear(context.Context, string) error                        { return nil }

func TestCreateOrganizationNormalizesInputAndHashesPassword(t *testing.T) {
	repository := &repositoryStub{created: Organization{ID: "org-1", Code: "example", Name: "示例企业"}}
	service := NewService(repository, limiterStub{}, func(password string) (string, error) {
		if password != "123" {
			t.Fatalf("哈希密码 = %q", password)
		}
		return "hashed", nil
	}, func(string, string) bool { return true }, time.Hour)

	organization, err := service.CreateOrganization(context.Background(), CreateOrganizationInput{
		Session: Session{Admin: Admin{ID: "platform-1"}}, Code: " Example ", Name: " 示例企业 ",
		AdminUsername: " Admin ", AdminDisplayName: " 企业管理员 ", AdminEmail: " ADMIN@EXAMPLE.COM ", AdminPassword: "123",
	})
	if err != nil {
		t.Fatalf("CreateOrganization() error = %v", err)
	}
	if organization.ID != "org-1" {
		t.Fatalf("企业 ID = %q", organization.ID)
	}
	input := repository.createdInput
	if input.Code != "example" || input.AdminUsername != "admin" || input.AdminEmail != "admin@example.com" {
		t.Fatalf("参数未规范化: %#v", input)
	}
	if input.AdminPasswordHash != "hashed" || input.ActorID != "platform-1" {
		t.Fatalf("创建参数不正确: %#v", input)
	}
}

func TestCreateOrganizationRejectsInvalidCode(t *testing.T) {
	service := NewService(&repositoryStub{}, limiterStub{}, func(string) (string, error) { return "", nil }, func(string, string) bool { return true }, time.Hour)
	_, err := service.CreateOrganization(context.Background(), CreateOrganizationInput{
		Session: Session{Admin: Admin{ID: "platform-1"}}, Code: "中文", Name: "示例企业",
		AdminUsername: "admin", AdminDisplayName: "企业管理员", AdminPassword: "123",
	})
	if err != ErrInvalidInput {
		t.Fatalf("error = %v，期望 ErrInvalidInput", err)
	}
}
