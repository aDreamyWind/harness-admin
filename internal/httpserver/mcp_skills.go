package httpserver

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/mcpclient"
	"harness-admin/internal/mcpconnection"
)

func registerMCPGatewaySkills(server *mcp.Server, deps Dependencies, route mcpconnection.GatewayConnection, upstream MCPSkillCaller) error {
	if err := mcp.AddReceivingCustomMethod(server, "skills/list",
		func(ctx context.Context, _ *mcp.ServerSession, params *mcpclient.ListSkillsParams) (*mcpclient.ListSkillsResult, error) {
			callCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			defer stop()
			service, credentials, err := mcpGatewayCurrentUpstream(callCtx, deps, route)
			if err != nil {
				return nil, err
			}
			return upstream.ListSkills(callCtx, service, credentials, params.Cursor)
		}); err != nil {
		return err
	}
	if err := mcp.AddReceivingCustomMethod(server, "skills/get",
		func(ctx context.Context, _ *mcp.ServerSession, params *mcpclient.GetSkillParams) (*mcpclient.GetSkillResult, error) {
			callCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			defer stop()
			service, credentials, err := mcpGatewayCurrentUpstream(callCtx, deps, route)
			if err != nil {
				return nil, err
			}
			return upstream.GetSkill(callCtx, service, credentials, params.URI)
		}); err != nil {
		return err
	}
	content, ok := deps.MCPToolCaller.(MCPContentCaller)
	if !ok {
		return errors.New("Skills 扩展需要 MCP 资源读取能力")
	}
	// 技能文件使用普通 resources/read。匹配整个 skill:// 命名空间，
	// 让未出现在分页目录中的技能也能通过 URI 直接读取。
	server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "skill://{+path}", Name: "Skill 文件"},
		func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			callCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			defer stop()
			service, credentials, err := mcpGatewayCurrentUpstream(callCtx, deps, route)
			if err != nil {
				return nil, err
			}
			return content.ReadResource(callCtx, service, credentials, request.Params.URI)
		})
	return nil
}
