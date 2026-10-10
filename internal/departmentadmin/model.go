package departmentadmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput       = errors.New("部门参数不正确")
	ErrDepartmentNotFound = errors.New("部门不存在")
	ErrDepartmentConflict = errors.New("部门代码已存在")
	ErrParentNotFound     = errors.New("上级部门不存在或未启用")
	ErrInvalidParent      = errors.New("不能将部门移动到自身或其下级部门")
	ErrDepartmentInUse    = errors.New("部门下仍有启用中的子部门或用户")
	ErrInvalidStatus      = errors.New("部门状态不正确")
	ErrUserNotFound       = errors.New("用户不存在")
)

type Department struct {
	ID        string
	ParentID  string
	Code      string
	Name      string
	SortOrder int
	Status    string
	UserCount int64
	CreatedAt time.Time
}

type CreateInput struct {
	OrganizationID string
	ActorID        string
	ParentID       string
	Code           string
	Name           string
	SortOrder      int
	RequestID      string
	SourceIP       string
}

type NewDepartment struct {
	ID             string
	OrganizationID string
	ActorID        string
	ParentID       string
	Code           string
	Name           string
	SortOrder      int
	RequestID      string
	SourceIP       string
}

type UpdateInput struct {
	OrganizationID string
	ActorID        string
	DepartmentID   string
	ParentID       string
	Name           string
	SortOrder      int
	RequestID      string
	SourceIP       string
}

type DepartmentChange struct {
	OrganizationID string
	ActorID        string
	DepartmentID   string
	ParentID       string
	Name           string
	SortOrder      int
	RequestID      string
	SourceIP       string
}

type ChangeStatusInput struct {
	OrganizationID string
	ActorID        string
	DepartmentID   string
	Status         string
	RequestID      string
	SourceIP       string
}

type AssignUserInput struct {
	OrganizationID string
	ActorID        string
	UserID         string
	DepartmentID   string
	RequestID      string
	SourceIP       string
}

type Repository interface {
	ListDepartments(context.Context, string) ([]Department, error)
	CreateDepartment(context.Context, NewDepartment) (Department, error)
	UpdateDepartment(context.Context, DepartmentChange) (Department, error)
	ChangeDepartmentStatus(context.Context, ChangeStatusInput) (Department, error)
	AssignUserDepartment(context.Context, AssignUserInput) error
}
