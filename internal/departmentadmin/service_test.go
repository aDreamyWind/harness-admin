package departmentadmin

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	created  NewDepartment
	updated  DepartmentChange
	status   ChangeStatusInput
	assigned AssignUserInput
	err      error
}

func (r *repositoryStub) ListDepartments(context.Context, string) ([]Department, error) {
	return nil, r.err
}

func (r *repositoryStub) CreateDepartment(_ context.Context, input NewDepartment) (Department, error) {
	r.created = input
	return Department{ID: input.ID, Code: input.Code, Name: input.Name}, r.err
}

func (r *repositoryStub) UpdateDepartment(_ context.Context, input DepartmentChange) (Department, error) {
	r.updated = input
	return Department{ID: input.DepartmentID, Name: input.Name}, r.err
}

func (r *repositoryStub) ChangeDepartmentStatus(_ context.Context, input ChangeStatusInput) (Department, error) {
	r.status = input
	return Department{ID: input.DepartmentID, Status: input.Status}, r.err
}

func (r *repositoryStub) AssignUserDepartment(_ context.Context, input AssignUserInput) error {
	r.assigned = input
	return r.err
}

func TestCreateNormalizesDepartment(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	department, err := service.Create(context.Background(), CreateInput{
		OrganizationID: "org-1", ActorID: "admin-1", Code: " Engineering ", Name: " 研发中心 ", SortOrder: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if department.ID == "" || repository.created.Code != "engineering" || repository.created.Name != "研发中心" {
		t.Fatalf("部门未正确规范化: %#v", repository.created)
	}
}

func TestCreateRejectsInvalidCode(t *testing.T) {
	service := NewService(&repositoryStub{})
	_, err := service.Create(context.Background(), CreateInput{
		OrganizationID: "org-1", ActorID: "admin-1", Code: "研发部", Name: "研发部",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("错误 = %v，期望 ErrInvalidInput", err)
	}
}

func TestChangeStatusRejectsUnknownStatus(t *testing.T) {
	service := NewService(&repositoryStub{})
	_, err := service.ChangeStatus(context.Background(), ChangeStatusInput{
		OrganizationID: "org-1", ActorID: "admin-1", DepartmentID: "department-1", Status: "archived",
	})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("错误 = %v，期望 ErrInvalidStatus", err)
	}
}

func TestAssignUserAllowsClearingDepartment(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	err := service.AssignUser(context.Background(), AssignUserInput{
		OrganizationID: "org-1", ActorID: "admin-1", UserID: "user-1", DepartmentID: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.assigned.UserID != "user-1" || repository.assigned.DepartmentID != "" {
		t.Fatalf("用户部门参数未正确传递: %#v", repository.assigned)
	}
}
