package access

import (
	"context"
	"fmt"
)

type Repository interface {
	HasPermission(ctx context.Context, userID, permissionCode string) (bool, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Allowed(ctx context.Context, userID, permissionCode string) (bool, error) {
	if userID == "" || permissionCode == "" {
		return false, nil
	}
	allowed, err := s.repository.HasPermission(ctx, userID, permissionCode)
	if err != nil {
		return false, fmt.Errorf("检查用户权限失败: %w", err)
	}
	return allowed, nil
}
