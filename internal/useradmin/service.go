package useradmin

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Service struct {
	repository   Repository
	hashPassword PasswordHashFunc
}

func NewService(repository Repository, hashPassword PasswordHashFunc) *Service {
	return &Service{repository: repository, hashPassword: hashPassword}
}

func (s *Service) List(ctx context.Context, filter ListFilter) (UserList, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	if filter.Status != "" && filter.Status != "active" && filter.Status != "disabled" && filter.Status != "locked" {
		return UserList{}, ErrInvalidUserStatus
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	result, err := s.repository.ListUsers(ctx, filter)
	if err != nil {
		return UserList{}, fmt.Errorf("查询用户列表失败: %w", err)
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (User, error) {
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.DepartmentID = strings.TrimSpace(input.DepartmentID)

	if !validLength(input.Username, 3, 80) || strings.ContainsAny(input.Username, " \t\r\n") ||
		!validLength(input.DisplayName, 1, 120) || input.OrganizationID == "" || input.ActorID == "" {
		return User{}, ErrInvalidInput
	}
	if input.Email != "" {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email {
			return User{}, ErrInvalidInput
		}
	}
	passwordHash, err := s.hashPassword(input.Password)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	user, err := s.repository.CreateUser(ctx, NewUser{
		ID:             uuid.NewString(),
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		Username:       input.Username,
		Email:          input.Email,
		DisplayName:    input.DisplayName,
		DepartmentID:   input.DepartmentID,
		PasswordHash:   passwordHash,
		RequestID:      input.RequestID,
		SourceIP:       input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrUserConflict) || errors.Is(err, ErrInvalidDepartment) {
			return User{}, err
		}
		return User{}, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

func (s *Service) ChangeStatus(ctx context.Context, input ChangeStatusInput) (User, error) {
	if input.Status != "active" && input.Status != "disabled" {
		return User{}, ErrInvalidUserStatus
	}
	if input.Status == "disabled" && input.UserID == input.ActorID {
		return User{}, ErrCannotDisableSelf
	}
	user, err := s.repository.ChangeUserStatus(ctx, input)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return User{}, err
		}
		return User{}, fmt.Errorf("修改用户状态失败: %w", err)
	}
	return user, nil
}

func validLength(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}
