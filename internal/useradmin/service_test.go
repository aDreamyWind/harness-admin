package useradmin

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	listFilter  ListFilter
	created     NewUser
	statusInput ChangeStatusInput
}

func (r *repositoryStub) ListUsers(_ context.Context, filter ListFilter) (UserList, error) {
	r.listFilter = filter
	return UserList{Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *repositoryStub) CreateUser(_ context.Context, input NewUser) (User, error) {
	r.created = input
	return User{ID: input.ID, Username: input.Username}, nil
}

func (r *repositoryStub) ChangeUserStatus(_ context.Context, input ChangeStatusInput) (User, error) {
	r.statusInput = input
	return User{ID: input.UserID, Status: input.Status}, nil
}

func TestListAppliesPaginationDefaults(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, func(string) (string, error) { return "hash", nil })
	if _, err := service.List(context.Background(), ListFilter{}); err != nil {
		t.Fatal(err)
	}
	if repository.listFilter.Page != 1 || repository.listFilter.PageSize != 20 {
		t.Fatalf("分页默认值错误: %#v", repository.listFilter)
	}
}

func TestCreateHashesPassword(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, func(password string) (string, error) {
		if password != "SecurePassword2026!" {
			t.Fatalf("收到的密码 = %q", password)
		}
		return "password-hash", nil
	})
	user, err := service.Create(context.Background(), CreateInput{
		OrganizationID: "org-1",
		ActorID:        "admin-1",
		Username:       "new.user",
		DisplayName:    "新用户",
		Email:          "USER@EXAMPLE.COM",
		Password:       "SecurePassword2026!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || repository.created.PasswordHash != "password-hash" {
		t.Fatal("用户标识或密码哈希未正确传递")
	}
	if repository.created.Email != "user@example.com" {
		t.Fatalf("邮箱未规范化: %s", repository.created.Email)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	service := NewService(&repositoryStub{}, func(string) (string, error) { return "", errors.New("密码过短") })
	_, err := service.Create(context.Background(), CreateInput{
		OrganizationID: "org-1", ActorID: "admin-1", Username: "user", DisplayName: "用户", Password: "123",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("错误 = %v，期望 ErrInvalidInput", err)
	}
}

func TestChangeStatusRejectsDisablingSelf(t *testing.T) {
	service := NewService(&repositoryStub{}, func(string) (string, error) { return "hash", nil })
	_, err := service.ChangeStatus(context.Background(), ChangeStatusInput{
		OrganizationID: "org-1", ActorID: "user-1", UserID: "user-1", Status: "disabled",
	})
	if !errors.Is(err, ErrCannotDisableSelf) {
		t.Fatalf("错误 = %v，期望 ErrCannotDisableSelf", err)
	}
}
