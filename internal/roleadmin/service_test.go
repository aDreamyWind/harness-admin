package roleadmin

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	created  NewRole
	updated  RoleChange
	assigned AssignUserRolesInput
	err      error
}

func (r *repositoryStub) ListRoles(context.Context, string) ([]Role, error) {
	return nil, r.err
}

func (r *repositoryStub) ListPermissions(context.Context) ([]Permission, error) {
	return nil, r.err
}

func (r *repositoryStub) CreateRole(_ context.Context, input NewRole) (Role, error) {
	r.created = input
	return Role{ID: input.ID, Code: input.Code, Name: input.Name, PermissionCodes: input.PermissionCodes}, r.err
}

func (r *repositoryStub) UpdateRole(_ context.Context, input RoleChange) (Role, error) {
	r.updated = input
	return Role{ID: input.RoleID, Name: input.Name, PermissionCodes: input.PermissionCodes}, r.err
}

func (r *repositoryStub) AssignUserRoles(_ context.Context, input AssignUserRolesInput) error {
	r.assigned = input
	return r.err
}

func TestCreateNormalizesRoleAndPermissions(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	role, err := service.Create(context.Background(), CreateInput{
		OrganizationID: "org-1", ActorID: "user-1", Code: " Audit_Viewer ", Name: "  审计员  ",
		PermissionCodes: []string{"audit.event.read", "audit.event.read", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if role.ID == "" || repository.created.Code != "audit_viewer" || repository.created.Name != "审计员" {
		t.Fatalf("角色未正确规范化: %#v", repository.created)
	}
	if len(repository.created.PermissionCodes) != 1 {
		t.Fatalf("权限未去重: %#v", repository.created.PermissionCodes)
	}
}

func TestCreateRejectsInvalidCode(t *testing.T) {
	service := NewService(&repositoryStub{})
	_, err := service.Create(context.Background(), CreateInput{
		OrganizationID: "org-1", ActorID: "user-1", Code: "中文角色", Name: "角色",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("错误 = %v，期望 ErrInvalidInput", err)
	}
}

func TestAssignRejectsChangingOwnRoles(t *testing.T) {
	service := NewService(&repositoryStub{})
	err := service.AssignUserRoles(context.Background(), AssignUserRolesInput{
		OrganizationID: "org-1", ActorID: "user-1", UserID: "user-1", RoleIDs: []string{"role-1"},
	})
	if !errors.Is(err, ErrCannotChangeOwnRoles) {
		t.Fatalf("错误 = %v，期望 ErrCannotChangeOwnRoles", err)
	}
}

func TestAssignDeduplicatesRoles(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	err := service.AssignUserRoles(context.Background(), AssignUserRolesInput{
		OrganizationID: "org-1", ActorID: "admin-1", UserID: "user-1",
		RoleIDs: []string{"role-1", "role-1", "role-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.assigned.RoleIDs) != 2 {
		t.Fatalf("角色未去重: %#v", repository.assigned.RoleIDs)
	}
}
