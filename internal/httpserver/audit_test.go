package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"harness-admin/internal/auditadmin"
	"harness-admin/internal/identity"
)

type auditAuthStub struct {
	session identity.Session
}

func (s auditAuthStub) Login(context.Context, identity.LoginInput) (identity.LoginResult, error) {
	return identity.LoginResult{}, nil
}

func (s auditAuthStub) Authenticate(context.Context, string) (identity.Session, error) {
	return s.session, nil
}

func (auditAuthStub) ValidateCSRF(identity.Session, string) error { return nil }
func (auditAuthStub) Logout(context.Context, identity.Session, string, string) error {
	return nil
}
func (auditAuthStub) ChangePassword(context.Context, identity.ChangePasswordInput) (int64, error) {
	return 0, nil
}
func (auditAuthStub) ListSessions(context.Context, identity.Session) ([]identity.UserSession, error) {
	return nil, nil
}
func (auditAuthStub) RevokeManagedSession(context.Context, identity.Session, string, string, string) error {
	return nil
}
func (auditAuthStub) RevokeOtherSessions(context.Context, identity.Session, string, string) (int64, error) {
	return 0, nil
}

type auditAccessStub struct{ allowed bool }

func (s auditAccessStub) Allowed(context.Context, string, string) (bool, error) {
	return s.allowed, nil
}

type auditEventServiceStub struct {
	filter auditadmin.ListFilter
	list   auditadmin.EventList
}

func (s *auditEventServiceStub) List(_ context.Context, filter auditadmin.ListFilter) (auditadmin.EventList, error) {
	s.filter = filter
	return s.list, nil
}

func (*auditEventServiceStub) Get(context.Context, string, int64) (auditadmin.Event, error) {
	return auditadmin.Event{}, nil
}

type auditRecorderStub struct{ event identity.AuditEvent }

func (s *auditRecorderStub) RecordAudit(_ context.Context, event identity.AuditEvent) error {
	s.event = event
	return nil
}

func TestListAuditEventsUsesCurrentOrganization(t *testing.T) {
	service := &auditEventServiceStub{list: auditadmin.EventList{
		Items: []auditadmin.Event{{ID: 7, Action: "auth.login", ResourceType: "session", Result: "success", Detail: map[string]any{}, CreatedAt: time.Now()}},
		Total: 1, Page: 2, PageSize: 20,
	}}
	router := newAuditTestRouter(true, service, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit-events?search=admin&result=success&page=2", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if service.filter.OrganizationID != "org-1" || service.filter.Search != "admin" || service.filter.Result != "success" || service.filter.Page != 2 {
		t.Fatalf("审计筛选参数不正确: %#v", service.filter)
	}
	var body struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].ID != "7" {
		t.Fatalf("响应中的事件编号应使用字符串: %s", response.Body.String())
	}
}

func TestPermissionDenialWritesAuditEvent(t *testing.T) {
	service := &auditEventServiceStub{}
	recorder := &auditRecorderStub{}
	router := newAuditTestRouter(false, service, recorder)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit-events", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusForbidden)
	}
	if recorder.event.OrganizationID != "org-1" || recorder.event.ActorID != "user-1" ||
		recorder.event.Result != "denied" || recorder.event.ResourceID != "audit.event.read" {
		t.Fatalf("权限拒绝事件不完整: %#v", recorder.event)
	}
}

func newAuditTestRouter(allowed bool, service AuditEventService, recorder AuditRecorder) http.Handler {
	return New(Dependencies{
		AppName:       "Harness Admin",
		Environment:   "test",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:          auditAuthStub{session: identity.Session{User: identity.User{ID: "user-1", OrganizationID: "org-1", Status: "active"}}},
		Access:        auditAccessStub{allowed: allowed},
		AuditEvents:   service,
		AuditRecorder: recorder,
		SessionCookie: "test_session",
	})
}
