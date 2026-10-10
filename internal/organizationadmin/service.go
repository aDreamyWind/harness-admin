package organizationadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Get(ctx context.Context, organizationID string) (Organization, error) {
	if organizationID == "" {
		return Organization{}, ErrInvalidInput
	}
	organization, err := s.repository.FindOrganization(ctx, organizationID)
	if err != nil {
		if errors.Is(err, ErrOrganizationNotFound) {
			return Organization{}, err
		}
		return Organization{}, fmt.Errorf("查询企业资料失败: %w", err)
	}
	return organization, nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (Organization, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.OrganizationID == "" || input.ActorID == "" ||
		utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 160 {
		return Organization{}, ErrInvalidInput
	}
	organization, err := s.repository.UpdateOrganization(ctx, NameUpdate{
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		Name:           input.Name,
		RequestID:      input.RequestID,
		SourceIP:       input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrOrganizationNotFound) {
			return Organization{}, err
		}
		return Organization{}, fmt.Errorf("修改企业资料失败: %w", err)
	}
	return organization, nil
}
