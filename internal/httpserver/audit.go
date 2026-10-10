package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/auditadmin"
	"harness-admin/internal/httpx"
)

type AuditEventService interface {
	List(context.Context, auditadmin.ListFilter) (auditadmin.EventList, error)
	Get(context.Context, string, int64) (auditadmin.Event, error)
}

type auditEventResponse struct {
	ID               string         `json:"id"`
	ActorID          string         `json:"actorId,omitempty"`
	ActorUsername    string         `json:"actorUsername,omitempty"`
	ActorDisplayName string         `json:"actorDisplayName,omitempty"`
	RequestID        string         `json:"requestId,omitempty"`
	Action           string         `json:"action"`
	ResourceType     string         `json:"resourceType"`
	ResourceID       string         `json:"resourceId,omitempty"`
	Result           string         `json:"result"`
	SourceIP         string         `json:"sourceIp,omitempty"`
	Detail           map[string]any `json:"detail"`
	CreatedAt        time.Time      `json:"createdAt"`
}

type auditEventListResponse struct {
	Items    []auditEventResponse `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
}

func registerAuditRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/audit-events", requireSession(deps), requirePermission(deps, "audit.event.read"))
	group.GET("", listAuditEventsHandler(deps))
	group.GET("/:id", getAuditEventHandler(deps))
}

func listAuditEventsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		page, err := queryInt(ctx, "page", 1)
		if err != nil {
			handleAuditError(ctx, deps.Logger, auditadmin.ErrInvalidFilter)
			return
		}
		pageSize, err := queryInt(ctx, "pageSize", 20)
		if err != nil {
			handleAuditError(ctx, deps.Logger, auditadmin.ErrInvalidFilter)
			return
		}
		startedAt, err := optionalRFC3339(ctx.Query("startedAt"))
		if err != nil {
			handleAuditError(ctx, deps.Logger, auditadmin.ErrInvalidFilter)
			return
		}
		endedAt, err := optionalRFC3339(ctx.Query("endedAt"))
		if err != nil {
			handleAuditError(ctx, deps.Logger, auditadmin.ErrInvalidFilter)
			return
		}
		session := currentSession(ctx)
		result, err := deps.AuditEvents.List(ctx.Request.Context(), auditadmin.ListFilter{
			OrganizationID: session.User.OrganizationID,
			Search:         ctx.Query("search"),
			Action:         ctx.Query("action"),
			Result:         ctx.Query("result"),
			StartedAt:      startedAt,
			EndedAt:        endedAt,
			Page:           page,
			PageSize:       pageSize,
		})
		if err != nil {
			handleAuditError(ctx, deps.Logger, err)
			return
		}
		items := make([]auditEventResponse, 0, len(result.Items))
		for _, event := range result.Items {
			items = append(items, presentAuditEvent(event))
		}
		httpx.OK(ctx, http.StatusOK, auditEventListResponse{
			Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
		})
	}
}

func getAuditEventHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		eventID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil || eventID < 1 {
			handleAuditError(ctx, deps.Logger, auditadmin.ErrInvalidFilter)
			return
		}
		session := currentSession(ctx)
		event, err := deps.AuditEvents.Get(ctx.Request.Context(), session.User.OrganizationID, eventID)
		if err != nil {
			handleAuditError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentAuditEvent(event))
	}
}

func handleAuditError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, auditadmin.ErrInvalidFilter):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_audit_filter", "审计筛选参数不正确")
	case errors.Is(err, auditadmin.ErrEventNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "audit_event_not_found", "审计事件不存在")
	default:
		logger.Error("审计事件接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func presentAuditEvent(event auditadmin.Event) auditEventResponse {
	return auditEventResponse{
		ID: strconv.FormatInt(event.ID, 10), ActorID: event.ActorID,
		ActorUsername: event.ActorUsername, ActorDisplayName: event.ActorDisplayName,
		RequestID: event.RequestID, Action: event.Action, ResourceType: event.ResourceType,
		ResourceID: event.ResourceID, Result: event.Result, SourceIP: event.SourceIP,
		Detail: event.Detail, CreatedAt: event.CreatedAt,
	}
}

func optionalRFC3339(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
