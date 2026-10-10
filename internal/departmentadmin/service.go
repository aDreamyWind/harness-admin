package departmentadmin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var departmentCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,63}$`)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, organizationID string) ([]Department, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	departments, err := s.repository.ListDepartments(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询部门列表失败: %w", err)
	}
	return departments, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Department, error) {
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	if input.OrganizationID == "" || input.ActorID == "" || !departmentCodePattern.MatchString(input.Code) ||
		!validLength(input.Name, 1, 160) || input.SortOrder < 0 || input.SortOrder > 999999 {
		return Department{}, ErrInvalidInput
	}
	department, err := s.repository.CreateDepartment(ctx, NewDepartment{
		ID: uuid.NewString(), OrganizationID: input.OrganizationID, ActorID: input.ActorID,
		ParentID: input.ParentID, Code: input.Code, Name: input.Name, SortOrder: input.SortOrder,
		RequestID: input.RequestID, SourceIP: input.SourceIP,
	})
	if err != nil {
		if knownWriteError(err) {
			return Department{}, err
		}
		return Department{}, fmt.Errorf("创建部门失败: %w", err)
	}
	return department, nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (Department, error) {
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.Name = strings.TrimSpace(input.Name)
	if input.OrganizationID == "" || input.ActorID == "" || input.DepartmentID == "" ||
		!validLength(input.Name, 1, 160) || input.SortOrder < 0 || input.SortOrder > 999999 {
		return Department{}, ErrInvalidInput
	}
	department, err := s.repository.UpdateDepartment(ctx, DepartmentChange{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, DepartmentID: input.DepartmentID,
		ParentID: input.ParentID, Name: input.Name, SortOrder: input.SortOrder,
		RequestID: input.RequestID, SourceIP: input.SourceIP,
	})
	if err != nil {
		if knownWriteError(err) {
			return Department{}, err
		}
		return Department{}, fmt.Errorf("修改部门失败: %w", err)
	}
	return department, nil
}

func (s *Service) ChangeStatus(ctx context.Context, input ChangeStatusInput) (Department, error) {
	if input.Status != "active" && input.Status != "disabled" {
		return Department{}, ErrInvalidStatus
	}
	department, err := s.repository.ChangeDepartmentStatus(ctx, input)
	if err != nil {
		if knownWriteError(err) {
			return Department{}, err
		}
		return Department{}, fmt.Errorf("修改部门状态失败: %w", err)
	}
	return department, nil
}

func (s *Service) AssignUser(ctx context.Context, input AssignUserInput) error {
	input.DepartmentID = strings.TrimSpace(input.DepartmentID)
	if input.OrganizationID == "" || input.ActorID == "" || input.UserID == "" {
		return ErrInvalidInput
	}
	if err := s.repository.AssignUserDepartment(ctx, input); err != nil {
		if knownWriteError(err) {
			return err
		}
		return fmt.Errorf("调整用户部门失败: %w", err)
	}
	return nil
}

func knownWriteError(err error) bool {
	return errors.Is(err, ErrDepartmentNotFound) || errors.Is(err, ErrDepartmentConflict) ||
		errors.Is(err, ErrParentNotFound) || errors.Is(err, ErrInvalidParent) ||
		errors.Is(err, ErrDepartmentInUse) || errors.Is(err, ErrUserNotFound)
}

func validLength(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}
