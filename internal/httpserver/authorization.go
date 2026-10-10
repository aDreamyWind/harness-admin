package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
	"harness-admin/internal/identity"
)

type AuthorizationService interface {
	Allowed(context.Context, string, string) (bool, error)
}

type AuditRecorder interface {
	RecordAudit(context.Context, identity.AuditEvent) error
}

func requirePermission(deps Dependencies, permissionCode string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := currentSession(ctx)
		allowed, err := deps.Access.Allowed(ctx.Request.Context(), session.User.ID, permissionCode)
		if err != nil {
			deps.Logger.Error("权限检查失败", "error", err, "permission", permissionCode,
				"user_id", session.User.ID, "request_id", contextString(ctx, "request_id"))
			httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
			return
		}
		if !allowed {
			if deps.AuditRecorder != nil {
				if err := deps.AuditRecorder.RecordAudit(ctx.Request.Context(), identity.AuditEvent{
					OrganizationID: session.User.OrganizationID,
					ActorID:        session.User.ID,
					RequestID:      contextString(ctx, "request_id"),
					Action:         "authorization.permission.denied",
					ResourceType:   "permission",
					ResourceID:     permissionCode,
					Result:         "denied",
					SourceIP:       ctx.ClientIP(),
					Detail: map[string]any{
						"method": ctx.Request.Method,
						"path":   ctx.Request.URL.Path,
					},
				}); err != nil {
					deps.Logger.Error("记录权限拒绝事件失败", "error", err, "permission", permissionCode,
						"user_id", session.User.ID, "request_id", contextString(ctx, "request_id"))
				}
			}
			httpx.Fail(ctx, http.StatusForbidden, "forbidden", "当前账号没有执行此操作的权限")
			return
		}
		ctx.Next()
	}
}
