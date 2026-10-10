package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/useradmin"
)

type UserAdminService interface {
	List(context.Context, useradmin.ListFilter) (useradmin.UserList, error)
	Create(context.Context, useradmin.CreateInput) (useradmin.User, error)
	Update(context.Context, useradmin.UpdateInput) (useradmin.User, error)
	ChangeStatus(context.Context, useradmin.ChangeStatusInput) (useradmin.User, error)
	ResetPassword(context.Context, useradmin.ResetPasswordInput) error
}

type createUserRequest struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	DisplayName  string `json:"displayName"`
	DepartmentID string `json:"departmentId"`
	Password     string `json:"password"`
}

type changeUserStatusRequest struct {
	Status string `json:"status"`
}

type updateUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type resetUserPasswordRequest struct {
	Password string `json:"password"`
}

type managedUserResponse struct {
	ID             string     `json:"id"`
	Username       string     `json:"username"`
	Email          string     `json:"email,omitempty"`
	DisplayName    string     `json:"displayName"`
	DepartmentID   string     `json:"departmentId,omitempty"`
	DepartmentName string     `json:"departmentName,omitempty"`
	Status         string     `json:"status"`
	RoleIDs        []string   `json:"roleIds"`
	RoleNames      []string   `json:"roleNames"`
	LastLoginAt    *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type userListResponse struct {
	Items    []managedUserResponse `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
}

func registerUserRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/users", requireSession(deps))
	group.GET("", requirePermission(deps, "iam.user.read"), listUsersHandler(deps))
	group.POST("", requirePermission(deps, "iam.user.write"), requireCSRF(deps), createUserHandler(deps))
	group.PUT("/:id", requirePermission(deps, "iam.user.write"), requireCSRF(deps), updateUserHandler(deps))
	group.PATCH("/:id/status", requirePermission(deps, "iam.user.write"), requireCSRF(deps), changeUserStatusHandler(deps))
	group.PUT("/:id/password", requirePermission(deps, "iam.user.write"), requireCSRF(deps), resetUserPasswordHandler(deps))
}

func updateUserHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户标识格式不正确")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16*1024)
		var request updateUserRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户资料参数格式不正确")
			return
		}
		session := currentSession(ctx)
		user, err := deps.UserAdmin.Update(ctx.Request.Context(), useradmin.UpdateInput{
			OrganizationID: session.User.OrganizationID,
			ActorID:        session.User.ID,
			UserID:         ctx.Param("id"),
			Email:          request.Email,
			DisplayName:    request.DisplayName,
			RequestID:      contextString(ctx, "request_id"),
			SourceIP:       ctx.ClientIP(),
		})
		if err != nil {
			handleUserAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentManagedUser(user))
	}
}

func resetUserPasswordHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户标识格式不正确")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 8*1024)
		var request resetUserPasswordRequest
		if err := ctx.ShouldBindJSON(&request); err != nil || len(request.Password) > 1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "密码参数格式不正确")
			return
		}
		session := currentSession(ctx)
		if err := deps.UserAdmin.ResetPassword(ctx.Request.Context(), useradmin.ResetPasswordInput{
			OrganizationID: session.User.OrganizationID,
			ActorID:        session.User.ID,
			UserID:         ctx.Param("id"),
			Password:       request.Password,
			RequestID:      contextString(ctx, "request_id"),
			SourceIP:       ctx.ClientIP(),
		}); err != nil {
			handleUserAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"reset": true})
	}
}

func listUsersHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		page, err := queryInt(ctx, "page", 1)
		if err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "分页参数格式不正确")
			return
		}
		pageSize, err := queryInt(ctx, "pageSize", 20)
		if err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "分页参数格式不正确")
			return
		}
		session := currentSession(ctx)
		result, err := deps.UserAdmin.List(ctx.Request.Context(), useradmin.ListFilter{
			OrganizationID: session.User.OrganizationID,
			Search:         ctx.Query("search"),
			Status:         ctx.Query("status"),
			Page:           page,
			PageSize:       pageSize,
		})
		if err != nil {
			handleUserAdminError(ctx, deps.Logger, err)
			return
		}
		items := make([]managedUserResponse, 0, len(result.Items))
		for _, user := range result.Items {
			items = append(items, presentManagedUser(user))
		}
		httpx.OK(ctx, http.StatusOK, userListResponse{
			Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
		})
	}
}

func createUserHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
		var request createUserRequest
		if err := ctx.ShouldBindJSON(&request); err != nil || len(request.Password) > 1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户参数格式不正确")
			return
		}
		session := currentSession(ctx)
		user, err := deps.UserAdmin.Create(ctx.Request.Context(), useradmin.CreateInput{
			OrganizationID: session.User.OrganizationID,
			ActorID:        session.User.ID,
			Username:       request.Username,
			Email:          request.Email,
			DisplayName:    request.DisplayName,
			DepartmentID:   request.DepartmentID,
			Password:       request.Password,
			RequestID:      contextString(ctx, "request_id"),
			SourceIP:       ctx.ClientIP(),
		})
		if err != nil {
			handleUserAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentManagedUser(user))
	}
}

func changeUserStatusHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户标识格式不正确")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 8*1024)
		var request changeUserStatusRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户状态参数格式不正确")
			return
		}
		session := currentSession(ctx)
		user, err := deps.UserAdmin.ChangeStatus(ctx.Request.Context(), useradmin.ChangeStatusInput{
			OrganizationID: session.User.OrganizationID,
			ActorID:        session.User.ID,
			UserID:         ctx.Param("id"),
			Status:         request.Status,
			RequestID:      contextString(ctx, "request_id"),
			SourceIP:       ctx.ClientIP(),
		})
		if err != nil {
			handleUserAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentManagedUser(user))
	}
}

func handleUserAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, useradmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_user", err.Error())
	case errors.Is(err, useradmin.ErrInvalidUserStatus):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_status", "用户状态不正确")
	case errors.Is(err, useradmin.ErrUserConflict):
		httpx.Fail(ctx, http.StatusConflict, "user_conflict", "用户名或邮箱已存在")
	case errors.Is(err, useradmin.ErrInvalidDepartment):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_department", "所选部门不存在")
	case errors.Is(err, useradmin.ErrCannotDisableSelf):
		httpx.Fail(ctx, http.StatusConflict, "cannot_disable_self", "不能停用当前登录账号")
	case errors.Is(err, useradmin.ErrCannotResetSelf):
		httpx.Fail(ctx, http.StatusConflict, "cannot_reset_self", err.Error())
	case errors.Is(err, useradmin.ErrUserNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "user_not_found", "用户不存在")
	default:
		logger.Error("用户管理接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func presentManagedUser(user useradmin.User) managedUserResponse {
	return managedUserResponse{
		ID: user.ID, Username: user.Username, Email: user.Email, DisplayName: user.DisplayName,
		DepartmentID: user.DepartmentID, DepartmentName: user.DepartmentName, Status: user.Status,
		RoleIDs: user.RoleIDs, RoleNames: user.RoleNames, LastLoginAt: user.LastLoginAt, CreatedAt: user.CreatedAt,
	}
}

func queryInt(ctx *gin.Context, key string, fallback int) (int, error) {
	value := ctx.Query(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
