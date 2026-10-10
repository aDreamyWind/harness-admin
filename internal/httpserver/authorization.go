package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
)

type AuthorizationService interface {
	Allowed(context.Context, string, string) (bool, error)
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
			httpx.Fail(ctx, http.StatusForbidden, "forbidden", "当前账号没有执行此操作的权限")
			return
		}
		ctx.Next()
	}
}
