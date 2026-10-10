package useradmin

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	listFilter    ListFilter
	created       NewUser
	updated       ProfileUpdate
	statusInput   ChangeStatusInput
	passwordReset PasswordReset
}

func (r *repositoryStub) ListUsers(_ context.Context, filter ListFilter) (UserList, error) {
	r.listFilter = filter
	return UserList{Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *repositoryStub) CreateUser(_ context.Context, input NewUser) (User, error) {
	r.created = input
	return User{ID: input.ID, Username: input.Username}, nil
}

func (r *repositoryStub) UpdateUser(_ context.Context, input ProfileUpdate) (User, error) {
	r.updated = input
	return User{ID: input.UserID, Email: input.Email, DisplayName: input.DisplayName}, nil
}

func (r *repositoryStub) ChangeUserStatus(_ context.Context, input ChangeStatusInput) (User, error) {
	r.statusInput = input
	return User{ID: input.UserID, Status: input.Status}, nil
}

func (r *repositoryStub) ResetUserPassword(_ context.Context, input PasswordReset) error {
	r.passwordReset = input
	return nil
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

func TestUpdateNormalizesProfile(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, func(string) (string, error) { return "hash", nil })
	user, err := service.Update(context.Background(), UpdateInput{
		OrganizationID: "org-1",
		ActorID:        "admin-1",
		UserID:         "user-1",
		DisplayName:    "  新名称  ",
		Email:          "USER@EXAMPLE.COM ",
		RequestID:      "request-1",
		SourceIP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "新名称" || user.Email != "user@example.com" {
		t.Fatalf("返回的用户资料未规范化: %#v", user)
	}
	if repository.updated.OrganizationID != "org-1" || repository.updated.ActorID != "admin-1" ||
		repository.updated.UserID != "user-1" || repository.updated.DisplayName != "新名称" ||
		repository.updated.Email != "user@example.com" || repository.updated.RequestID != "request-1" {
		t.Fatalf("修改用户资料参数不正确: %#v", repository.updated)
	}
}

func TestUpdateRejectsInvalidProfile(t *testing.T) {
	service := NewService(&repositoryStub{}, func(string) (string, error) { return "hash", nil })
	for _, input := range []UpdateInput{
		{OrganizationID: "org-1", ActorID: "admin-1", UserID: "user-1", DisplayName: ""},
		{OrganizationID: "org-1", ActorID: "admin-1", UserID: "user-1", DisplayName: "用户", Email: "not-an-email"},
	} {
		if _, err := service.Update(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("输入 %#v 的错误 = %v，期望 ErrInvalidInput", input, err)
		}
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

func TestResetPasswordHashesPasswordAndRejectsSelf(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, func(password string) (string, error) {
		if password != "123" {
			t.Fatalf("收到的密码 = %q", password)
		}
		return "password-hash", nil
	})
	if err := service.ResetPassword(context.Background(), ResetPasswordInput{
		OrganizationID: "org-1", ActorID: "admin-1", UserID: "user-1", Password: "123",
	}); err != nil {
		t.Fatal(err)
	}
	if repository.passwordReset.PasswordHash != "password-hash" || repository.passwordReset.UserID != "user-1" {
		t.Fatalf("重置密码参数不正确: %#v", repository.passwordReset)
	}
	if err := service.ResetPassword(context.Background(), ResetPasswordInput{
		OrganizationID: "org-1", ActorID: "admin-1", UserID: "admin-1", Password: "123",
	}); !errors.Is(err, ErrCannotResetSelf) {
		t.Fatalf("错误 = %v，期望 ErrCannotResetSelf", err)
	}
}
