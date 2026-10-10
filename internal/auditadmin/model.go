package auditadmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidFilter = errors.New("审计筛选参数不正确")
	ErrEventNotFound = errors.New("审计事件不存在")
)

type Event struct {
	ID               int64
	ActorID          string
	ActorUsername    string
	ActorDisplayName string
	RequestID        string
	Action           string
	ResourceType     string
	ResourceID       string
	Result           string
	SourceIP         string
	Detail           map[string]any
	CreatedAt        time.Time
}

type ListFilter struct {
	OrganizationID string
	Search         string
	Action         string
	Result         string
	StartedAt      *time.Time
	EndedAt        *time.Time
	Page           int
	PageSize       int
}

type EventList struct {
	Items    []Event
	Total    int64
	Page     int
	PageSize int
}

type Repository interface {
	ListAuditEvents(context.Context, ListFilter) (EventList, error)
	FindAuditEvent(context.Context, string, int64) (Event, error)
}
