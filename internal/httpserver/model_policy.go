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
	"harness-admin/internal/modelgateway"
)

type ModelPolicyService interface {
	List(context.Context, string) ([]modelgateway.SubjectPolicy, error)
	Save(context.Context, modelgateway.SavePolicyInput) (modelgateway.SubjectPolicy, error)
	SaveDepartments(context.Context, modelgateway.SaveDepartmentPoliciesInput) ([]modelgateway.SubjectPolicy, error)
	Delete(context.Context, modelgateway.DeletePolicyInput) error
}

type modelPolicyWriteRequest struct {
	ScopeMode         string   `json:"scopeMode"`
	AllowedModelCodes []string `json:"allowedModelCodes"`
	RequestsPerMinute int      `json:"requestsPerMinute"`
	MonthlyTokenLimit int64    `json:"monthlyTokenLimit"`
	MonthlyTokenMode  string   `json:"monthlyTokenMode"`
}

type modelDepartmentPolicyBatchRequest struct {
	SubjectIDs []string `json:"subjectIds"`
	modelPolicyWriteRequest
}

type modelPolicyResponse struct {
	SubjectType       string    `json:"subjectType"`
	SubjectID         string    `json:"subjectId"`
	SubjectName       string    `json:"subjectName"`
	ScopeMode         string    `json:"scopeMode"`
	AllowedModelCodes []string  `json:"allowedModelCodes"`
	RequestsPerMinute int       `json:"requestsPerMinute"`
	MonthlyTokenLimit int64     `json:"monthlyTokenLimit"`
	MonthlyTokenMode  string    `json:"monthlyTokenMode"`
	MonthlyTokenUsed  int64     `json:"monthlyTokenUsed"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func registerModelPolicyRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/model-policies", requireSession(deps))
	group.GET("", requirePermission(deps, "model.policy.read"), func(ctx *gin.Context) {
		items, err := deps.ModelPolicy.List(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleModelPolicyError(ctx, deps.Logger, err)
			return
		}
		response := make([]modelPolicyResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentModelPolicy(item))
		}
		httpx.OK(ctx, http.StatusOK, response)
	})
	group.PUT("/departments", requirePermission(deps, "model.policy.write"), requireCSRF(deps), func(ctx *gin.Context) {
		var request modelDepartmentPolicyBatchRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		for i, id := range request.SubjectIDs {
			parsed, err := uuid.Parse(id)
			if err != nil {
				httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "部门标识格式不正确")
				return
			}
			request.SubjectIDs[i] = parsed.String()
		}
		session := currentSession(ctx)
		items, err := deps.ModelPolicy.SaveDepartments(ctx.Request.Context(), modelgateway.SaveDepartmentPoliciesInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			SubjectIDs: request.SubjectIDs, ScopeMode: request.ScopeMode,
			AllowedModelCodes: request.AllowedModelCodes, RequestsPerMinute: request.RequestsPerMinute,
			MonthlyTokenLimit: request.MonthlyTokenLimit, MonthlyTokenMode: request.MonthlyTokenMode,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelPolicyError(ctx, deps.Logger, err)
			return
		}
		response := make([]modelPolicyResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentModelPolicy(item))
		}
		httpx.OK(ctx, http.StatusOK, response)
	})
	group.PUT("/:subjectType/:subjectId", requirePermission(deps, "model.policy.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validModelPolicyPath(ctx) {
			return
		}
		var request modelPolicyWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		item, err := deps.ModelPolicy.Save(ctx.Request.Context(), modelgateway.SavePolicyInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			SubjectType: ctx.Param("subjectType"), SubjectID: ctx.Param("subjectId"),
			ScopeMode:         request.ScopeMode,
			AllowedModelCodes: request.AllowedModelCodes, RequestsPerMinute: request.RequestsPerMinute,
			MonthlyTokenLimit: request.MonthlyTokenLimit, MonthlyTokenMode: request.MonthlyTokenMode,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelPolicyError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentModelPolicy(item))
	})
	group.DELETE("/:subjectType/:subjectId", requirePermission(deps, "model.policy.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validModelPolicyPath(ctx) {
			return
		}
		session := currentSession(ctx)
		if err := deps.ModelPolicy.Delete(ctx.Request.Context(), modelgateway.DeletePolicyInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			SubjectType: ctx.Param("subjectType"), SubjectID: ctx.Param("subjectId"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		}); err != nil {
			handleModelPolicyError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"deleted": true})
	})
}

func validModelPolicyPath(ctx *gin.Context) bool {
	if ctx.Param("subjectType") != "department" && ctx.Param("subjectType") != "user" {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "策略对象类型不正确")
		return false
	}
	if _, err := uuid.Parse(ctx.Param("subjectId")); err != nil {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "策略对象标识格式不正确")
		return false
	}
	return true
}

func presentModelPolicy(item modelgateway.SubjectPolicy) modelPolicyResponse {
	return modelPolicyResponse{
		SubjectType: item.SubjectType, SubjectID: item.SubjectID, SubjectName: item.SubjectName,
		ScopeMode:         item.ScopeMode,
		AllowedModelCodes: item.AllowedModelCodes, RequestsPerMinute: item.RequestsPerMinute,
		MonthlyTokenLimit: item.MonthlyTokenLimit, MonthlyTokenMode: item.MonthlyTokenMode, MonthlyTokenUsed: item.MonthlyTokenUsed,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func handleModelPolicyError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, modelgateway.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_policy", "模型策略参数不正确")
	case errors.Is(err, modelgateway.ErrPolicySubjectNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "policy_subject_not_found", "所属部门或用户不存在")
	case errors.Is(err, modelgateway.ErrModelNotFound):
		httpx.Fail(ctx, http.StatusBadRequest, "model_not_found", "策略中包含不存在的模型编码")
	case errors.Is(err, modelgateway.ErrPolicyNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "policy_not_found", "模型策略不存在")
	default:
		logger.Error("模型策略接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
