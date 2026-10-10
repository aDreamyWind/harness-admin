package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/departmentadmin"
	"harness-admin/internal/httpx"
)

type DepartmentAdminService interface {
	List(context.Context, string) ([]departmentadmin.Department, error)
	Create(context.Context, departmentadmin.CreateInput) (departmentadmin.Department, error)
	Update(context.Context, departmentadmin.UpdateInput) (departmentadmin.Department, error)
	ChangeStatus(context.Context, departmentadmin.ChangeStatusInput) (departmentadmin.Department, error)
	AssignUser(context.Context, departmentadmin.AssignUserInput) error
}

type departmentWriteRequest struct {
	ParentID  string `json:"parentId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

type changeDepartmentStatusRequest struct {
	Status string `json:"status"`
}

type assignUserDepartmentRequest struct {
	DepartmentID string `json:"departmentId"`
}

type departmentResponse struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parentId,omitempty"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	Status    string    `json:"status"`
	UserCount int64     `json:"userCount"`
	CreatedAt time.Time `json:"createdAt"`
}

func registerDepartmentRoutes(router *gin.Engine, deps Dependencies) {
	router.GET("/api/v1/departments", requireSession(deps), requirePermission(deps, "iam.department.read"), listDepartmentsHandler(deps))
	router.POST("/api/v1/departments", requireSession(deps), requirePermission(deps, "iam.department.write"), requireCSRF(deps), createDepartmentHandler(deps))
	router.PUT("/api/v1/departments/:id", requireSession(deps), requirePermission(deps, "iam.department.write"), requireCSRF(deps), updateDepartmentHandler(deps))
	router.PATCH("/api/v1/departments/:id/status", requireSession(deps), requirePermission(deps, "iam.department.write"), requireCSRF(deps), changeDepartmentStatusHandler(deps))
	router.PATCH("/api/v1/users/:id/department", requireSession(deps), requirePermission(deps, "iam.user.write"), requireCSRF(deps), assignUserDepartmentHandler(deps))
}

func listDepartmentsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		departments, err := deps.DepartmentAdmin.List(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleDepartmentAdminError(ctx, deps.Logger, err)
			return
		}
		items := make([]departmentResponse, 0, len(departments))
		for _, department := range departments {
			items = append(items, presentDepartment(department))
		}
		httpx.OK(ctx, http.StatusOK, items)
	}
}

func createDepartmentHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 32*1024)
		var request departmentWriteRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门参数格式不正确")
			return
		}
		if !validOptionalUUID(request.ParentID) {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "上级部门标识格式不正确")
			return
		}
		session := currentSession(ctx)
		department, err := deps.DepartmentAdmin.Create(ctx.Request.Context(), departmentadmin.CreateInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			ParentID: request.ParentID, Code: request.Code, Name: request.Name, SortOrder: request.SortOrder,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleDepartmentAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentDepartment(department))
	}
}

func updateDepartmentHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门标识格式不正确")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 32*1024)
		var request departmentWriteRequest
		if err := ctx.ShouldBindJSON(&request); err != nil || !validOptionalUUID(request.ParentID) {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门参数格式不正确")
			return
		}
		session := currentSession(ctx)
		department, err := deps.DepartmentAdmin.Update(ctx.Request.Context(), departmentadmin.UpdateInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, DepartmentID: ctx.Param("id"),
			ParentID: request.ParentID, Name: request.Name, SortOrder: request.SortOrder,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleDepartmentAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentDepartment(department))
	}
}

func changeDepartmentStatusHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门标识格式不正确")
			return
		}
		var request changeDepartmentStatusRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门状态参数格式不正确")
			return
		}
		session := currentSession(ctx)
		department, err := deps.DepartmentAdmin.ChangeStatus(ctx.Request.Context(), departmentadmin.ChangeStatusInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, DepartmentID: ctx.Param("id"),
			Status: request.Status, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleDepartmentAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentDepartment(department))
	}
}

func assignUserDepartmentHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "用户标识格式不正确")
			return
		}
		var request assignUserDepartmentRequest
		if err := ctx.ShouldBindJSON(&request); err != nil || !validOptionalUUID(request.DepartmentID) {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门分配参数格式不正确")
			return
		}
		session := currentSession(ctx)
		if err := deps.DepartmentAdmin.AssignUser(ctx.Request.Context(), departmentadmin.AssignUserInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, UserID: ctx.Param("id"),
			DepartmentID: request.DepartmentID, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		}); err != nil {
			handleDepartmentAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"userId": ctx.Param("id"), "departmentId": request.DepartmentID})
	}
}

func validOptionalUUID(value string) bool {
	if value == "" {
		return true
	}
	_, err := uuid.Parse(value)
	return err == nil
}

func presentDepartment(department departmentadmin.Department) departmentResponse {
	return departmentResponse{
		ID: department.ID, ParentID: department.ParentID, Code: department.Code, Name: department.Name,
		SortOrder: department.SortOrder, Status: department.Status, UserCount: department.UserCount,
		CreatedAt: department.CreatedAt,
	}
}

func handleDepartmentAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, departmentadmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_department", "部门参数不正确")
	case errors.Is(err, departmentadmin.ErrInvalidStatus):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_status", "部门状态不正确")
	case errors.Is(err, departmentadmin.ErrParentNotFound):
		httpx.Fail(ctx, http.StatusBadRequest, "parent_not_found", "上级部门不存在或未启用")
	case errors.Is(err, departmentadmin.ErrInvalidParent):
		httpx.Fail(ctx, http.StatusConflict, "invalid_parent", "不能将部门移动到自身或其下级部门")
	case errors.Is(err, departmentadmin.ErrDepartmentConflict):
		httpx.Fail(ctx, http.StatusConflict, "department_conflict", "部门代码已存在")
	case errors.Is(err, departmentadmin.ErrDepartmentInUse):
		httpx.Fail(ctx, http.StatusConflict, "department_in_use", "请先处理该部门下启用中的子部门和用户")
	case errors.Is(err, departmentadmin.ErrDepartmentNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "department_not_found", "部门不存在")
	case errors.Is(err, departmentadmin.ErrUserNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "user_not_found", "用户不存在")
	default:
		logger.Error("部门管理接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
