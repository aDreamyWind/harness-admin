package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/httpx"
	"harness-admin/internal/mcpclient"
	"harness-admin/internal/skilladmin"
)

type SkillService interface {
	Import(context.Context, skilladmin.Actor, []byte) (skilladmin.Asset, error)
	ImportGitHub(context.Context, skilladmin.Actor, skilladmin.GitHubImportInput) (skilladmin.GitHubImportResult, error)
	List(context.Context, string) ([]skilladmin.Asset, error)
	Versions(context.Context, string, string) ([]skilladmin.Version, error)
	ReadVersionFile(context.Context, string, string, string, string) (skilladmin.File, error)
	Publish(context.Context, skilladmin.Actor, string, string, string) (skilladmin.Asset, error)
	ReplaceGrants(context.Context, skilladmin.Actor, string, []string, []string) (skilladmin.Asset, error)
	ListForUser(context.Context, string, string) ([]skilladmin.Published, error)
	GetForUser(context.Context, string, string, string) (skilladmin.Published, error)
	ReadFile(context.Context, string, string, string, string) (skilladmin.File, error)
}

type skillPublishRequest struct {
	Status    string `json:"status"`
	VersionID string `json:"versionId"`
}

type skillGrantsRequest struct {
	DepartmentIDs []string `json:"departmentIds"`
	UserIDs       []string `json:"userIds"`
}

func registerSkillRoutes(router *gin.Engine, deps Dependencies) {
	admin := router.Group("/api/v1/skills", requireSession(deps))
	admin.GET("", requirePermission(deps, "skill.asset.read"), func(ctx *gin.Context) {
		items, err := deps.Skills.List(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	admin.POST("/import", requirePermission(deps, "skill.asset.write"), requireCSRF(deps), func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 18*1024*1024)
		header, err := ctx.FormFile("file")
		if err != nil || header.Size > 16*1024*1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_skill_package", "请选择不超过 16 MiB 的 .zip 或 .tar.gz Skill 包")
			return
		}
		file, err := header.Open()
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
		if err != nil || len(body) > 16*1024*1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_skill_package", "Skill 包过大")
			return
		}
		item, err := deps.Skills.Import(ctx.Request.Context(), skillActor(ctx), body)
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, item)
	})
	admin.POST("/import/github", requirePermission(deps, "skill.asset.write"), requireCSRF(deps), func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16*1024)
		var request skilladmin.GitHubImportInput
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.Skills.ImportGitHub(ctx.Request.Context(), skillActor(ctx), request)
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, item)
	})
	admin.GET("/:id/versions", requirePermission(deps, "skill.asset.read"), func(ctx *gin.Context) {
		items, err := deps.Skills.Versions(ctx.Request.Context(), currentSession(ctx).User.OrganizationID, ctx.Param("id"))
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	admin.GET("/:id/versions/:versionId/files/*path", requirePermission(deps, "skill.asset.read"), func(ctx *gin.Context) {
		file, err := deps.Skills.ReadVersionFile(ctx.Request.Context(), currentSession(ctx).User.OrganizationID,
			ctx.Param("id"), ctx.Param("versionId"), strings.TrimPrefix(ctx.Param("path"), "/"))
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "private, no-store")
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("Content-Disposition", "attachment")
		ctx.Data(http.StatusOK, "application/octet-stream", file.Content)
	})
	admin.PATCH("/:id/publish", requirePermission(deps, "skill.asset.write"), requireCSRF(deps), func(ctx *gin.Context) {
		var request skillPublishRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.Skills.Publish(ctx.Request.Context(), skillActor(ctx), ctx.Param("id"), request.VersionID, request.Status)
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	admin.PUT("/:id/grants", requirePermission(deps, "skill.asset.write"), requireCSRF(deps), func(ctx *gin.Context) {
		var request skillGrantsRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.Skills.ReplaceGrants(ctx.Request.Context(), skillActor(ctx), ctx.Param("id"), request.DepartmentIDs, request.UserIDs)
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	router.GET("/api/v1/skills/directory", func(ctx *gin.Context) {
		subject, ok := skillSubject(ctx, deps)
		if !ok {
			return
		}
		items, err := deps.Skills.ListForUser(ctx.Request.Context(), subject.OrganizationID, subject.ID)
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "private, no-store")
		httpx.OK(ctx, http.StatusOK, items)
	})
	router.GET("/api/v1/skills/files/:name/*path", func(ctx *gin.Context) {
		subject, ok := skillSubject(ctx, deps)
		if !ok {
			return
		}
		file, err := deps.Skills.ReadFile(ctx.Request.Context(), subject.OrganizationID, subject.ID,
			ctx.Param("name"), strings.TrimPrefix(ctx.Param("path"), "/"))
		if err != nil {
			handleSkillError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "private, no-store")
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("Content-Disposition", "attachment")
		ctx.Header("ETag", `"`+file.Digest+`"`)
		ctx.Data(http.StatusOK, "application/octet-stream", file.Content)
	})
	router.POST("/enterprise-skills/mcp", enterpriseSkillsGateway(deps))
	router.GET("/enterprise-skills/mcp", enterpriseSkillsGateway(deps))
}

func skillActor(ctx *gin.Context) skilladmin.Actor {
	session := currentSession(ctx)
	return skilladmin.Actor{OrganizationID: session.User.OrganizationID, UserID: session.User.ID,
		RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP()}
}

func skillSubject(ctx *gin.Context, deps Dependencies) (subject struct{ OrganizationID, ID string }, ok bool) {
	authenticated, err := deps.OIDCProvider.AuthenticateAccessToken(ctx.Request.Context(), bearerCredential(ctx.GetHeader("Authorization")), "mcp.invoke")
	if err != nil {
		ctx.Header("WWW-Authenticate", "Bearer")
		httpx.Fail(ctx, http.StatusUnauthorized, "invalid_token", "请使用具有 MCP 调用权限的员工访问令牌")
		return subject, false
	}
	subject.OrganizationID, subject.ID = authenticated.OrganizationID, authenticated.ID
	return subject, true
}

func enterpriseSkillsGateway(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if origin := ctx.GetHeader("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host == "" || parsed.User != nil || !strings.EqualFold(parsed.Host, ctx.Request.Host) {
				httpx.Fail(ctx, http.StatusForbidden, "mcp_origin_forbidden", "MCP 请求来源不受信任")
				return
			}
		}
		subject, ok := skillSubject(ctx, deps)
		if !ok {
			return
		}
		capabilities := &mcp.ServerCapabilities{Resources: &mcp.ResourceCapabilities{}}
		capabilities.AddExtension(mcpclient.SkillsExtension, nil)
		server := mcp.NewServer(&mcp.Implementation{Name: "Harness Admin 企业 Skills", Version: "1.0.0"},
			&mcp.ServerOptions{Capabilities: capabilities})
		if err := registerEnterpriseSkillMethods(server, deps.Skills, subject.OrganizationID, subject.ID); err != nil {
			httpx.Fail(ctx, http.StatusInternalServerError, "skill_gateway_error", "企业 Skill 网关暂不可用")
			return
		}
		ctx.Header("Cache-Control", "private, no-store")
		mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
			&mcp.StreamableHTTPOptions{Stateless: true}).ServeHTTP(ctx.Writer, ctx.Request)
	}
}

func registerEnterpriseSkillMethods(server *mcp.Server, skills SkillService, orgID, userID string) error {
	if err := mcp.AddReceivingCustomMethod(server, "skills/list",
		func(ctx context.Context, _ *mcp.ServerSession, params *mcpclient.ListSkillsParams) (*mcpclient.ListSkillsResult, error) {
			items, err := skills.ListForUser(ctx, orgID, userID)
			if err != nil {
				return nil, err
			}
			result := &mcpclient.ListSkillsResult{ResultType: "complete", Skills: make([]mcpclient.SkillEntry, 0), CacheScope: "private"}
			lastName := ""
			for _, item := range items {
				if item.Asset.Name <= params.Cursor {
					continue
				}
				if len(result.Skills) == 50 {
					result.NextCursor = lastName
					break
				}
				entry, err := enterpriseSkillEntry(item)
				if err != nil {
					return nil, err
				}
				result.Skills = append(result.Skills, entry)
				lastName = item.Asset.Name
			}
			return result, nil
		}); err != nil {
		return err
	}
	if err := mcp.AddReceivingCustomMethod(server, "skills/get",
		func(ctx context.Context, _ *mcp.ServerSession, params *mcpclient.GetSkillParams) (*mcpclient.GetSkillResult, error) {
			name, file, ok := parseEnterpriseSkillURI(params.URI)
			if !ok || file != "SKILL.md" {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Skill 不存在"}
			}
			item, err := skills.GetForUser(ctx, orgID, userID, name)
			if errors.Is(err, skilladmin.ErrForbidden) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Skill 不存在"}
			}
			if err != nil {
				return nil, err
			}
			entry, err := enterpriseSkillEntry(item)
			if err != nil {
				return nil, err
			}
			return &mcpclient.GetSkillResult{ResultType: "complete", Skill: entry, CacheScope: "private"}, nil
		}); err != nil {
		return err
	}
	server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "skill://enterprise/{+path}", Name: "企业 Skill 文件"},
		func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			name, filePath, ok := parseEnterpriseSkillURI(request.Params.URI)
			if !ok {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Skill 文件不存在"}
			}
			file, err := skills.ReadFile(ctx, orgID, userID, name, filePath)
			if errors.Is(err, skilladmin.ErrForbidden) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Skill 文件不存在"}
			}
			if err != nil {
				return nil, err
			}
			content := &mcp.ResourceContents{URI: request.Params.URI, MIMEType: file.MIMEType}
			if strings.HasPrefix(file.MIMEType, "text/") {
				content.Text = string(file.Content)
			} else {
				content.Blob = file.Content
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}}, nil
		})
	return nil
}

func parseEnterpriseSkillURI(value string) (name, filePath string, ok bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "skill" || parsed.Host != "enterprise" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	segments := strings.TrimPrefix(parsed.Path, "/")
	name, filePath, ok = strings.Cut(segments, "/")
	return name, filePath, ok && name != "" && filePath != ""
}

func enterpriseSkillEntry(item skilladmin.Published) (mcpclient.SkillEntry, error) {
	resources := make([]mcpclient.SkillResource, 0, len(item.Version.Files))
	base := "skill://enterprise/" + item.Asset.Name + "/"
	for _, file := range item.Version.Files {
		resources = append(resources, mcpclient.SkillResource{URI: base + file.Path, Digest: file.Digest, Size: int64(file.Size)})
	}
	encoded, err := json.Marshal(resources)
	if err != nil {
		return mcpclient.SkillEntry{}, err
	}
	var frontmatter map[string]any
	if err := json.Unmarshal(item.Version.Frontmatter, &frontmatter); err != nil {
		return mcpclient.SkillEntry{}, err
	}
	return mcpclient.SkillEntry{URI: base + "SKILL.md", Frontmatter: frontmatter, Resources: encoded}, nil
}

func handleSkillError(ctx *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, skilladmin.ErrSkillManifestMissing):
		httpx.Fail(ctx, http.StatusBadRequest, "skill_manifest_missing", "压缩包中找不到 SKILL.md；请打包单个 Skill 目录")
	case errors.Is(err, skilladmin.ErrMultipleSkillManifests):
		httpx.Fail(ctx, http.StatusBadRequest, "multiple_skill_manifests", "压缩包中有多个 SKILL.md；请只打包一个 Skill，或从 GitHub 指定子目录导入")
	case errors.Is(err, skilladmin.ErrInvalidPackage), errors.Is(err, skilladmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_skill", "Skill 包校验失败；请检查 SKILL.md 内容、文件大小和压缩包路径")
	case errors.Is(err, skilladmin.ErrNotFound), errors.Is(err, skilladmin.ErrForbidden):
		httpx.Fail(ctx, http.StatusNotFound, "skill_not_found", "Skill 不存在或无权读取")
	case errors.Is(err, skilladmin.ErrConflict):
		httpx.Fail(ctx, http.StatusConflict, "skill_conflict", "相同内容的 Skill 版本已存在")
	case errors.Is(err, skilladmin.ErrRepositorySource):
		deps.Logger.Warn("GitHub Skill 仓库读取失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusBadGateway, "skill_repository_unavailable", "GitHub 仓库读取失败，请检查公开状态、版本和网络")
	default:
		deps.Logger.Error("企业 Skill 操作失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
