package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/roleadmin"
)

type RoleAdminService interface {
	List(context.Context, string) ([]roleadmin.Role, error)
	ListPermissions(context.Context) ([]roleadmin.Permission, error)
	Create(context.Context, roleadmin.CreateInput) (roleadmin.Role, error)
	Update(context.Context, roleadmin.UpdateInput) (roleadmin.Role, error)
	AssignUserRoles(context.Context, roleadmin.AssignUserRolesInput) error
}

type roleWriteRequest struct {
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	PermissionCodes []string `json:"permissionCodes"`
}

type assignUserRolesRequest struct {
	RoleIDs []string `json:"roleIds"`
}

type permissionResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type roleResponse struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	BuiltIn         bool      `json:"builtIn"`
	UserCount       int64     `json:"userCount"`
	PermissionCodes []string  `json:"permissionCodes"`
	CreatedAt       time.Time `json:"createdAt"`
}

func registerRoleRoutes(router *gin.Engine, deps Dependencies) {
	router.GET("/api/v1/roles", requireSession(deps), requirePermission(deps, "iam.role.read"), listRolesHandler(deps))
	router.GET("/api/v1/permissions", requireSession(deps), requirePermission(deps, "iam.role.read"), listPermissionsHandler(deps))
	router.POST("/api/v1/roles", requireSession(deps), requirePermission(deps, "iam.role.write"), requireCSRF(deps), createRoleHandler(deps))
	router.PUT("/api/v1/roles/:id", requireSession(deps), requirePermission(deps, "iam.role.write"), requireCSRF(deps), updateRoleHandler(deps))
	router.PUT("/api/v1/users/:id/roles", requireSession(deps), requirePermission(deps, "iam.user.write"), requireCSRF(deps), assignUserRolesHandler(deps))
}

func listRolesHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		roles, err := deps.RoleAdmin.List(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleRoleAdminError(ctx, deps.Logger, err)
			return
		}
		items := make([]roleResponse, 0, len(roles))
		for _, role := range roles {
			items = append(items, presentRole(role))
		}
		httpx.OK(ctx, http.StatusOK, items)
	}
}

func listPermissionsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		permissions, err := deps.RoleAdmin.ListPermissions(ctx.Request.Context())
		if err != nil {
			handleRoleAdminError(ctx, deps.Logger, err)
			return
		}
		items := make([]permissionResponse, 0, len(permissions))
		for _, permission := range permissions {
			items = append(items, permissionResponse{
				ID: permission.ID, Code: permission.Code, Name: permission.Name,
				Resource: permission.Resource, Action: permission.Action, Description: permission.Description,
			})
		}
		httpx.OK(ctx, http.StatusOK, items)
	}
}

func createRoleHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
		var request roleWriteRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "角色参数格式不正确")
			return
		}
		session := currentSession(ctx)
		role, err := deps.RoleAdmin.Create(ctx.Request.Context(), roleadmin.CreateInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			Code: request.Code, Name: request.Name, Description: request.Description,
			PermissionCodes: request.PermissionCodes, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleRoleAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentRole(role))
	}
}

func updateRoleHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "角色标识格式不正确")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
		var request roleWriteRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "角色参数格式不正确")
			return
		}
		session := currentSession(ctx)
		role, err := deps.RoleAdmin.Update(ctx.Request.Context(), roleadmin.UpdateInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, RoleID: ctx.Param("id"),
			Name: request.Name, Description: request.Description, PermissionCodes: request.PermissionCodes,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleRoleAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentRole(role))
	}
}

func assignUserRolesHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户标识格式不正确")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
		var request assignUserRolesRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "角色分配参数格式不正确")
			return
		}
		for _, roleID := range request.RoleIDs {
			if _, err := uuid.Parse(roleID); err != nil {
				httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "角色标识格式不正确")
				return
			}
		}
		session := currentSession(ctx)
		if err := deps.RoleAdmin.AssignUserRoles(ctx.Request.Context(), roleadmin.AssignUserRolesInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, UserID: ctx.Param("id"),
			RoleIDs: request.RoleIDs, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		}); err != nil {
			handleRoleAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"userId": ctx.Param("id"), "roleIds": request.RoleIDs})
	}
}

func presentRole(role roleadmin.Role) roleResponse {
	return roleResponse{
		ID: role.ID, Code: role.Code, Name: role.Name, Description: role.Description,
		BuiltIn: role.BuiltIn, UserCount: role.UserCount,
		PermissionCodes: role.PermissionCodes, CreatedAt: role.CreatedAt,
	}
}

func handleRoleAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, roleadmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_role", "角色参数不正确")
	case errors.Is(err, roleadmin.ErrPermissionNotFound):
		httpx.Fail(ctx, http.StatusBadRequest, "permission_not_found", "包含不存在的权限")
	case errors.Is(err, roleadmin.ErrRoleConflict):
		httpx.Fail(ctx, http.StatusConflict, "role_conflict", "角色代码已存在")
	case errors.Is(err, roleadmin.ErrBuiltInRole):
		httpx.Fail(ctx, http.StatusConflict, "built_in_role", "系统内置角色不允许修改")
	case errors.Is(err, roleadmin.ErrCannotChangeOwnRoles):
		httpx.Fail(ctx, http.StatusConflict, "cannot_change_own_roles", "不能修改当前登录账号的角色")
	case errors.Is(err, roleadmin.ErrRoleNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "role_not_found", "角色不存在")
	case errors.Is(err, roleadmin.ErrUserNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "user_not_found", "用户不存在")
	default:
		logger.Error("角色管理接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
