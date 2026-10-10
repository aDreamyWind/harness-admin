package useradmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput      = errors.New("用户参数不正确")
	ErrUserNotFound      = errors.New("用户不存在")
	ErrUserConflict      = errors.New("用户名或邮箱已存在")
	ErrInvalidDepartment = errors.New("部门不存在")
	ErrCannotDisableSelf = errors.New("不能停用当前登录账号")
	ErrInvalidUserStatus = errors.New("用户状态不正确")
)

type User struct {
	ID             string
	Username       string
	Email          string
	DisplayName    string
	DepartmentID   string
	DepartmentName string
	Status         string
	RoleNames      []string
	LastLoginAt    *time.Time
	CreatedAt      time.Time
}

type ListFilter struct {
	OrganizationID string
	Search         string
	Status         string
	Page           int
	PageSize       int
}

type UserList struct {
	Items    []User
	Total    int64
	Page     int
	PageSize int
}

type CreateInput struct {
	OrganizationID string
	ActorID        string
	Username       string
	Email          string
	DisplayName    string
	DepartmentID   string
	Password       string
	RequestID      string
	SourceIP       string
}

type NewUser struct {
	ID             string
	OrganizationID string
	ActorID        string
	Username       string
	Email          string
	DisplayName    string
	DepartmentID   string
	PasswordHash   string
	RequestID      string
	SourceIP       string
}

type ChangeStatusInput struct {
	OrganizationID string
	ActorID        string
	UserID         string
	Status         string
	RequestID      string
	SourceIP       string
}

type Repository interface {
	ListUsers(ctx context.Context, filter ListFilter) (UserList, error)
	CreateUser(ctx context.Context, input NewUser) (User, error)
	ChangeUserStatus(ctx context.Context, input ChangeStatusInput) (User, error)
}

type PasswordHashFunc func(password string) (string, error)
