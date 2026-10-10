package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
	"harness-admin/internal/organizationadmin"
)

type OrganizationAdminService interface {
	Get(context.Context, string) (organizationadmin.Organization, error)
	Update(context.Context, organizationadmin.UpdateInput) (organizationadmin.Organization, error)
}

type updateOrganizationRequest struct {
	Name string `json:"name"`
}

type organizationResponse struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func registerOrganizationRoutes(router *gin.Engine, deps Dependencies) {
	router.GET("/api/v1/organization", requireSession(deps), requirePermission(deps, "iam.organization.read"), getOrganizationHandler(deps))
	router.PUT("/api/v1/organization", requireSession(deps), requirePermission(deps, "iam.organization.write"), requireCSRF(deps), updateOrganizationHandler(deps))
}

func getOrganizationHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		organization, err := deps.OrganizationAdmin.Get(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleOrganizationAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentOrganization(organization))
	}
}

func updateOrganizationHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 8*1024)
		var request updateOrganizationRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "企业资料参数格式不正确")
			return
		}
		session := currentSession(ctx)
		organization, err := deps.OrganizationAdmin.Update(ctx.Request.Context(), organizationadmin.UpdateInput{
			OrganizationID: session.User.OrganizationID,
			ActorID:        session.User.ID,
			Name:           request.Name,
			RequestID:      contextString(ctx, "request_id"),
			SourceIP:       ctx.ClientIP(),
		})
		if err != nil {
			handleOrganizationAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentOrganization(organization))
	}
}

func handleOrganizationAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, organizationadmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_organization", "企业资料参数不正确")
	case errors.Is(err, organizationadmin.ErrOrganizationNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "organization_not_found", "企业不存在")
	default:
		logger.Error("企业设置接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func presentOrganization(organization organizationadmin.Organization) organizationResponse {
	return organizationResponse{
		ID: organization.ID, Code: organization.Code, Name: organization.Name, Status: organization.Status,
		CreatedAt: organization.CreatedAt, UpdatedAt: organization.UpdatedAt,
	}
}
