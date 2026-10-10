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
	repository    Repository
	hashPassword  PasswordHashFunc
	loginFailures LoginFailureResetter
}

func NewService(repository Repository, hashPassword PasswordHashFunc, loginFailures LoginFailureResetter) *Service {
	return &Service{repository: repository, hashPassword: hashPassword, loginFailures: loginFailures}
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

func (s *Service) Update(ctx context.Context, input UpdateInput) (User, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.OrganizationID == "" || input.ActorID == "" || input.UserID == "" ||
		!validLength(input.DisplayName, 1, 120) {
		return User{}, ErrInvalidInput
	}
	if input.Email != "" {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email {
			return User{}, ErrInvalidInput
		}
	}
	user, err := s.repository.UpdateUser(ctx, ProfileUpdate{
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		UserID:         input.UserID,
		Email:          input.Email,
		DisplayName:    input.DisplayName,
		RequestID:      input.RequestID,
		SourceIP:       input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrUserConflict) {
			return User{}, err
		}
		return User{}, fmt.Errorf("修改用户资料失败: %w", err)
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
		if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrLockedUserStatus) {
			return User{}, err
		}
		return User{}, fmt.Errorf("修改用户状态失败: %w", err)
	}
	return user, nil
}

func (s *Service) ResetPassword(ctx context.Context, input ResetPasswordInput) error {
	if input.OrganizationID == "" || input.ActorID == "" || input.UserID == "" || len(input.Password) > 1024 {
		return ErrInvalidInput
	}
	if input.ActorID == input.UserID {
		return ErrCannotResetSelf
	}
	passwordHash, err := s.hashPassword(input.Password)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	err = s.repository.ResetUserPassword(ctx, PasswordReset{
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		UserID:         input.UserID,
		PasswordHash:   passwordHash,
		RequestID:      input.RequestID,
		SourceIP:       input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return err
		}
		return fmt.Errorf("重置用户密码失败: %w", err)
	}
	return nil
}

func (s *Service) Unlock(ctx context.Context, input UnlockInput) (User, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.UserID == "" {
		return User{}, ErrInvalidInput
	}
	lockedUser, err := s.repository.FindLockedUserIdentity(ctx, input.OrganizationID, input.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrUserNotLocked) {
			return User{}, err
		}
		return User{}, fmt.Errorf("查询锁定用户失败: %w", err)
	}
	if err := s.loginFailures.Clear(ctx, lockedUser.OrganizationCode, lockedUser.Username); err != nil {
		return User{}, fmt.Errorf("清除登录失败计数失败: %w", err)
	}
	user, err := s.repository.UnlockUser(ctx, input)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrUserNotLocked) {
			return User{}, err
		}
		return User{}, fmt.Errorf("解锁用户失败: %w", err)
	}
	return user, nil
}

func validLength(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}
