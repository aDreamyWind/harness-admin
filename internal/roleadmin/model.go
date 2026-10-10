package roleadmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput         = errors.New("角色参数不正确")
	ErrRoleNotFound         = errors.New("角色不存在")
	ErrRoleConflict         = errors.New("角色代码已存在")
	ErrPermissionNotFound   = errors.New("包含不存在的权限")
	ErrBuiltInRole          = errors.New("系统内置角色不允许修改")
	ErrUserNotFound         = errors.New("用户不存在")
	ErrCannotChangeOwnRoles = errors.New("不能修改当前登录账号的角色")
)

type Permission struct {
	ID          string
	Code        string
	Name        string
	Resource    string
	Action      string
	Description string
}

type Role struct {
	ID              string
	Code            string
	Name            string
	Description     string
	BuiltIn         bool
	UserCount       int64
	PermissionCodes []string
	CreatedAt       time.Time
}

type CreateInput struct {
	OrganizationID  string
	ActorID         string
	Code            string
	Name            string
	Description     string
	PermissionCodes []string
	RequestID       string
	SourceIP        string
}

type UpdateInput struct {
	OrganizationID  string
	ActorID         string
	RoleID          string
	Name            string
	Description     string
	PermissionCodes []string
	RequestID       string
	SourceIP        string
}

type AssignUserRolesInput struct {
	OrganizationID string
	ActorID        string
	UserID         string
	RoleIDs        []string
	RequestID      string
	SourceIP       string
}

type NewRole struct {
	ID              string
	OrganizationID  string
	ActorID         string
	Code            string
	Name            string
	Description     string
	PermissionCodes []string
	RequestID       string
	SourceIP        string
}

type RoleChange struct {
	OrganizationID  string
	ActorID         string
	RoleID          string
	Name            string
	Description     string
	PermissionCodes []string
	RequestID       string
	SourceIP        string
}

type Repository interface {
	ListRoles(context.Context, string) ([]Role, error)
	ListPermissions(context.Context) ([]Permission, error)
	CreateRole(context.Context, NewRole) (Role, error)
	UpdateRole(context.Context, RoleChange) (Role, error)
	AssignUserRoles(context.Context, AssignUserRolesInput) error
}
