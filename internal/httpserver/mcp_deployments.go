package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpdeploy"
)

type MCPDeploymentService interface {
	Enabled() bool
	InspectRepository(context.Context, string, string) (mcpdeploy.SourceInspection, error)
	Import(context.Context, mcpdeploy.Input) (mcpdeploy.Result, error)
	NewVersion(context.Context, mcpdeploy.Input) (mcpdeploy.Result, error)
	Start(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error)
	Stop(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error)
	Restart(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error)
	Rollback(context.Context, mcpadmin.Actor, string, string) (mcpadmin.ServiceRecord, error)
	Inspect(context.Context, string, string) (mcpdeploy.RuntimeStatus, error)
	ListVersions(context.Context, string, string) ([]mcpdeploy.Version, error)
	UpdateMetadata(context.Context, mcpadmin.Actor, string, string, string) (mcpadmin.ServiceRecord, error)
}

type mcpGitHubImportRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Repository    string `json:"repository"`
	Commit        string `json:"commit"`
	RepositoryURL string `json:"repositoryUrl"`
	Ref           string `json:"ref"`
	RuntimeType   string `json:"runtimeType"`
	EntryFile     string `json:"entryFile"`
}

type mcpRollbackRequest struct {
	VersionID string `json:"versionId"`
}

type mcpDeploymentMetadataRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func registerMCPDeploymentRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/mcp-deployments", requireSession(deps))
	group.GET("/capability", requirePermission(deps, "mcp.service.read"), func(ctx *gin.Context) {
		httpx.OK(ctx, http.StatusOK, gin.H{"enabled": deps.MCPDeploy.Enabled(),
			"sourceTypes": []string{"github", "gitee", "upload"}, "runtimes": []string{"node", "python"}})
	})
	group.POST("/inspect/repository", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		var request struct {
			RepositoryURL string `json:"repositoryUrl"`
			Ref           string `json:"ref"`
		}
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.MCPDeploy.InspectRepository(ctx.Request.Context(), request.RepositoryURL, request.Ref)
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	group.POST("/import/repository", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), mcpRepositoryImportHandler(deps, false))
	group.POST("/import/github", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), mcpRepositoryImportHandler(deps, false))
	group.POST("/import/upload", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), mcpUploadImportHandler(deps, false))
	group.POST("/:id/versions/repository", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), mcpRepositoryImportHandler(deps, true))
	group.POST("/:id/versions/github", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), mcpRepositoryImportHandler(deps, true))
	group.POST("/:id/versions/upload", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), mcpUploadImportHandler(deps, true))
	group.GET("/:id/versions", requirePermission(deps, "mcp.service.write"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		items, err := deps.MCPDeploy.ListVersions(ctx.Request.Context(), currentSession(ctx).User.OrganizationID, ctx.Param("id"))
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.GET("/:id/runtime", requirePermission(deps, "mcp.service.write"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		item, err := deps.MCPDeploy.Inspect(ctx.Request.Context(), currentSession(ctx).User.OrganizationID, ctx.Param("id"))
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	group.PUT("/:id/metadata", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpDeploymentMetadataRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.MCPDeploy.UpdateMetadata(ctx.Request.Context(), mcpActor(ctx), ctx.Param("id"), request.Name, request.Description)
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	for action, operation := range map[string]func(context.Context, mcpadmin.Actor, string) (mcpadmin.ServiceRecord, error){
		"start": deps.MCPDeploy.Start, "stop": deps.MCPDeploy.Stop, "restart": deps.MCPDeploy.Restart,
	} {
		group.POST("/:id/"+action, requirePermission(deps, "mcp.service.write"), requireCSRF(deps),
			func(ctx *gin.Context) {
				if !validMCPID(ctx) {
					return
				}
				item, err := operation(ctx.Request.Context(), mcpActor(ctx), ctx.Param("id"))
				if err != nil {
					handleMCPDeploymentError(ctx, deps, err)
					return
				}
				httpx.OK(ctx, http.StatusOK, item)
			})
	}
	group.POST("/:id/rollback", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpRollbackRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.MCPDeploy.Rollback(ctx.Request.Context(), mcpActor(ctx), ctx.Param("id"), request.VersionID)
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
}

func mcpRepositoryImportHandler(deps Dependencies, existing bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if existing && !validMCPID(ctx) {
			return
		}
		var request mcpGitHubImportRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		repositoryURL := strings.TrimSpace(request.RepositoryURL)
		if repositoryURL == "" {
			repositoryURL = strings.TrimSpace(request.Repository)
			if !strings.HasPrefix(repositoryURL, "https://") {
				repositoryURL = "https://github.com/" + repositoryURL
			}
		}
		provider, _, parseErr := mcpdeploy.ParseRepositoryURL(repositoryURL)
		if parseErr != nil {
			handleMCPDeploymentError(ctx, deps, mcpdeploy.ErrInvalidInput)
			return
		}
		ref := request.Ref
		if ref == "" {
			ref = request.Commit
		}
		input := mcpdeploy.Input{Actor: mcpActor(ctx), Name: request.Name,
			SourceType: provider, SourceRef: repositoryURL, Revision: ref,
			RuntimeType: request.RuntimeType, EntryFile: request.EntryFile}
		var result mcpdeploy.Result
		var err error
		if existing {
			input.ServiceID = ctx.Param("id")
			result, err = deps.MCPDeploy.NewVersion(ctx.Request.Context(), input)
		} else {
			result, err = deps.MCPDeploy.Import(ctx.Request.Context(), input)
		}
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusAccepted, result)
	}
}

func mcpUploadImportHandler(deps Dependencies, existing bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if existing && !validMCPID(ctx) {
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 21*1024*1024)
		if err := ctx.Request.ParseMultipartForm(1 << 20); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_archive", "上传代码包格式或大小不正确")
			return
		}
		if ctx.Request.MultipartForm != nil {
			defer ctx.Request.MultipartForm.RemoveAll()
		}
		fileHeader, err := ctx.FormFile("archive")
		if err != nil || fileHeader.Size > 20*1024*1024 || !strings.HasSuffix(fileHeader.Filename, ".tar.gz") {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_archive", "请上传不超过 20 MiB 的 .tar.gz 代码包")
			return
		}
		file, err := fileHeader.Open()
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		defer file.Close()
		archive, err := io.ReadAll(io.LimitReader(file, 20*1024*1024+1))
		if err != nil || len(archive) > 20*1024*1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_archive", "代码包超过 20 MiB")
			return
		}
		input := mcpdeploy.Input{Actor: mcpActor(ctx), Name: ctx.PostForm("name"),
			SourceType: "upload", SourceRef: fileHeader.Filename, RuntimeType: ctx.PostForm("runtimeType"),
			EntryFile: ctx.PostForm("entryFile"), Archive: archive}
		var result mcpdeploy.Result
		if existing {
			input.ServiceID = ctx.Param("id")
			result, err = deps.MCPDeploy.NewVersion(ctx.Request.Context(), input)
		} else {
			result, err = deps.MCPDeploy.Import(ctx.Request.Context(), input)
		}
		if err != nil {
			handleMCPDeploymentError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusAccepted, result)
	}
}

func handleMCPDeploymentError(ctx *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, mcpdeploy.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_deployment", "仓库地址、版本、代码包、运行环境或运行入口不正确")
	case errors.Is(err, mcpdeploy.ErrSource):
		httpx.Fail(ctx, http.StatusUnprocessableEntity, "mcp_source_failed", err.Error())
	case errors.Is(err, mcpdeploy.ErrDisabled):
		httpx.Fail(ctx, http.StatusServiceUnavailable, "mcp_deployment_disabled", "服务器尚未开启 MCP 源码部署")
	case errors.Is(err, mcpdeploy.ErrNotFound), errors.Is(err, mcpadmin.ErrNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "mcp_deployment_not_found", "MCP 服务或构建版本不存在")
	case errors.Is(err, mcpdeploy.ErrNotReady):
		httpx.Fail(ctx, http.StatusConflict, "mcp_deployment_not_ready", "MCP 服务尚未构建成功")
	case errors.Is(err, mcpdeploy.ErrBusy):
		httpx.Fail(ctx, http.StatusConflict, "mcp_deployment_busy", "此 MCP 正在执行构建或运行操作，请稍后重试")
	default:
		deps.Logger.Error("MCP 源码部署操作失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "mcp_deployment_failed", "MCP 部署操作失败，请检查 Docker 状态或构建记录")
	}
}

var _ = uuid.NewString
