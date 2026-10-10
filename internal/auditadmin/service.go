package auditadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, filter ListFilter) (EventList, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Action = strings.TrimSpace(filter.Action)
	filter.Result = strings.TrimSpace(filter.Result)
	if filter.OrganizationID == "" || !validResult(filter.Result) ||
		(filter.StartedAt != nil && filter.EndedAt != nil && filter.StartedAt.After(*filter.EndedAt)) {
		return EventList{}, ErrInvalidFilter
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
	result, err := s.repository.ListAuditEvents(ctx, filter)
	if err != nil {
		return EventList{}, fmt.Errorf("查询审计事件失败: %w", err)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, organizationID string, eventID int64) (Event, error) {
	if organizationID == "" || eventID < 1 {
		return Event{}, ErrInvalidFilter
	}
	event, err := s.repository.FindAuditEvent(ctx, organizationID, eventID)
	if err != nil {
		if errors.Is(err, ErrEventNotFound) {
			return Event{}, err
		}
		return Event{}, fmt.Errorf("查询审计事件详情失败: %w", err)
	}
	return event, nil
}

func validResult(result string) bool {
	return result == "" || result == "success" || result == "denied" || result == "failed"
}
