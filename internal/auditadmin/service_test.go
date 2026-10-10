package auditadmin

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repositoryStub struct {
	filter ListFilter
	event  Event
	err    error
}

func (r *repositoryStub) ListAuditEvents(_ context.Context, filter ListFilter) (EventList, error) {
	r.filter = filter
	return EventList{Page: filter.Page, PageSize: filter.PageSize}, r.err
}

func (r *repositoryStub) FindAuditEvent(context.Context, string, int64) (Event, error) {
	return r.event, r.err
}

func TestListNormalizesFilterAndPagination(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	result, err := service.List(context.Background(), ListFilter{
		OrganizationID: "org-1", Search: "  张三  ", Action: " auth.login ", PageSize: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.filter.Search != "张三" || repository.filter.Action != "auth.login" {
		t.Fatalf("筛选参数未规范化: %#v", repository.filter)
	}
	if result.Page != 1 || result.PageSize != 100 {
		t.Fatalf("分页参数未限制: %#v", result)
	}
}

func TestListRejectsInvalidResultAndTimeRange(t *testing.T) {
	service := NewService(&repositoryStub{})
	if _, err := service.List(context.Background(), ListFilter{OrganizationID: "org-1", Result: "unknown"}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("错误 = %v，期望 ErrInvalidFilter", err)
	}
	start := time.Now()
	end := start.Add(-time.Hour)
	if _, err := service.List(context.Background(), ListFilter{OrganizationID: "org-1", StartedAt: &start, EndedAt: &end}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("错误 = %v，期望 ErrInvalidFilter", err)
	}
}

func TestGetKeepsNotFoundError(t *testing.T) {
	service := NewService(&repositoryStub{err: ErrEventNotFound})
	_, err := service.Get(context.Background(), "org-1", 10)
	if !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("错误 = %v，期望 ErrEventNotFound", err)
	}
}
