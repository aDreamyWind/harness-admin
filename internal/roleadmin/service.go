package roleadmin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,79}$`)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, organizationID string) ([]Role, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	roles, err := s.repository.ListRoles(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询角色列表失败: %w", err)
	}
	return roles, nil
}

func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	permissions, err := s.repository.ListPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询权限列表失败: %w", err)
	}
	return permissions, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Role, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.PermissionCodes = uniqueStrings(input.PermissionCodes)
	if input.OrganizationID == "" || input.ActorID == "" || !roleCodePattern.MatchString(input.Code) ||
		!validLength(input.Name, 1, 120) || utf8.RuneCountInString(input.Description) > 500 {
		return Role{}, ErrInvalidInput
	}
	role, err := s.repository.CreateRole(ctx, NewRole{
		ID: uuid.NewString(), OrganizationID: input.OrganizationID, ActorID: input.ActorID,
		Code: input.Code, Name: input.Name, Description: input.Description,
		PermissionCodes: input.PermissionCodes, RequestID: input.RequestID, SourceIP: input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrRoleConflict) || errors.Is(err, ErrPermissionNotFound) {
			return Role{}, err
		}
		return Role{}, fmt.Errorf("创建角色失败: %w", err)
	}
	return role, nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (Role, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.PermissionCodes = uniqueStrings(input.PermissionCodes)
	if input.OrganizationID == "" || input.ActorID == "" || input.RoleID == "" ||
		!validLength(input.Name, 1, 120) || utf8.RuneCountInString(input.Description) > 500 {
		return Role{}, ErrInvalidInput
	}
	role, err := s.repository.UpdateRole(ctx, RoleChange{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RoleID: input.RoleID,
		Name: input.Name, Description: input.Description, PermissionCodes: input.PermissionCodes,
		RequestID: input.RequestID, SourceIP: input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) || errors.Is(err, ErrPermissionNotFound) || errors.Is(err, ErrBuiltInRole) {
			return Role{}, err
		}
		return Role{}, fmt.Errorf("修改角色失败: %w", err)
	}
	return role, nil
}

func (s *Service) AssignUserRoles(ctx context.Context, input AssignUserRolesInput) error {
	input.RoleIDs = uniqueStrings(input.RoleIDs)
	if input.OrganizationID == "" || input.ActorID == "" || input.UserID == "" {
		return ErrInvalidInput
	}
	if input.ActorID == input.UserID {
		return ErrCannotChangeOwnRoles
	}
	if err := s.repository.AssignUserRoles(ctx, input); err != nil {
		if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrRoleNotFound) {
			return err
		}
		return fmt.Errorf("分配用户角色失败: %w", err)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validLength(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}
