package database_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/database"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpconnection"
	"harness-admin/internal/mcpdeploy"
	"harness-admin/internal/secret"
	pgstore "harness-admin/internal/store/postgres"
)

type mcpTestLimiter struct{}

func (mcpTestLimiter) Allow(context.Context, string, int) (bool, error) { return true, nil }

// 使用随机命名的临时数据库；不触碰已有企业数据。
func TestMCPConnectionsAgainstPostgres(t *testing.T) {
	adminURL := os.Getenv("HARNESS_TEST_MIGRATION_ADMIN_URL")
	if adminURL == "" {
		t.Skip("设置 HARNESS_TEST_MIGRATION_ADMIN_URL 后运行 MCP 数据库集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "harness_mcp_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("清理 MCP 临时测试数据库失败: %v", err)
		}
	}()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	testURL := parsed.String()
	if err := database.Migrate(ctx, testURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	orgID, userID, departmentID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, code, name) VALUES ($1,$2,'MCP 测试企业')`, orgID, "mcp-test-"+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO departments (id, organization_id, code, name) VALUES ($1,$2,'tools','工具部门')`, departmentID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, organization_id, username, display_name, password_hash, department_id)
		VALUES ($1,$2,'mcp-test-user','测试员工','not-a-real-password',$3)`, userID, orgID, departmentID); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	serviceRepo := pgstore.NewMCPRepository(pool, cipher)
	service, err := serviceRepo.Create(ctx, mcpadmin.CreateInput{
		Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID},
		Code:  "tools", Name: "工具服务", Transport: "streamable_http", UpstreamURL: "https://example.com/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := serviceRepo.ReplaceTools(ctx, mcpadmin.DiscoverInput{
		Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, ServiceID: service.ID,
	}, []mcpadmin.Tool{{Name: "working_directory", InputSchema: []byte(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	service, err = serviceRepo.Update(ctx, mcpadmin.UpdateInput{
		Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, ServiceID: service.ID,
		Name: "工具服务（已修改）", Description: "修改后的说明", Transport: "streamable_http",
		UpstreamURL: "https://example.com/new-mcp",
	})
	if err != nil || service.Name != "工具服务（已修改）" || service.Description != "修改后的说明" {
		t.Fatalf("修改服务失败: %#v, %v", service, err)
	}
	toolsAfterUpdate, err := serviceRepo.ListTools(ctx, orgID, service.ID)
	if err != nil || len(toolsAfterUpdate) != 0 {
		t.Fatalf("地址修改后旧工具快照未清空: %#v, %v", toolsAfterUpdate, err)
	}
	if err := serviceRepo.ReplaceTools(ctx, mcpadmin.DiscoverInput{
		Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, ServiceID: service.ID,
	}, []mcpadmin.Tool{{Name: "working_directory", InputSchema: []byte(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	repository := pgstore.NewMCPConnectionRepository(pool, cipher)
	connections := mcpconnection.NewService(repository, mcpTestLimiter{})
	actor := mcpconnection.Actor{OrganizationID: orgID, UserID: userID}
	deploymentRepo := pgstore.NewMCPDeploymentRepository(pool)
	managed, err := deploymentRepo.Create(ctx, mcpdeploy.Input{Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID},
		Name: "托管测试工具", SourceType: "upload", SourceRef: "echo.tar.gz", Revision: "sha256-test",
		RuntimeType: "node", EntryFile: "echo.js"})
	if err != nil || managed.Service.Transport != "stdio" || managed.Service.DeploymentStatus != "building" {
		t.Fatalf("创建托管服务失败: %#v %v", managed, err)
	}
	release, err := deploymentRepo.AcquireLock(ctx, managed.Service.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deploymentRepo.AcquireLock(ctx, managed.Service.ID); !errors.Is(err, mcpdeploy.ErrBusy) {
		t.Fatalf("相同服务并发部署未被拒绝: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	managed, err = deploymentRepo.CompleteBuild(ctx, mcpadmin.Actor{OrganizationID: orgID, UserID: userID},
		managed.Service.ID, managed.Version.ID, "sha256:test-image", "构建完成", "")
	if err != nil || managed.Version.BuildStatus != "succeeded" || managed.Service.CurrentRevision != "sha256-test" {
		t.Fatalf("保存构建版本失败: %#v %v", managed, err)
	}
	managedService, err := deploymentRepo.SetRuntimeStatus(ctx, mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, managed.Service.ID, "running", "")
	if err != nil || managedService.DeploymentStatus != "running" {
		t.Fatalf("托管运行状态失败: %#v %v", managedService, err)
	}
	managedService, err = deploymentRepo.UpdateMetadata(ctx, mcpadmin.Actor{OrganizationID: orgID, UserID: userID},
		managed.Service.ID, "托管测试工具（已修改）", "## 使用说明")
	if err != nil || managedService.Name != "托管测试工具（已修改）" || managedService.Description != "" {
		t.Fatalf("托管服务名称修改或版本描述保护失败: %#v %v", managedService, err)
	}
	if err := serviceRepo.ReplaceTools(ctx, mcpadmin.DiscoverInput{Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, ServiceID: managed.Service.ID},
		[]mcpadmin.Tool{{Name: "echo", InputSchema: []byte(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	managedConnection, err := connections.Create(ctx, mcpconnection.CreateInput{Actor: actor, ServiceID: managed.Service.ID,
		Name: "托管开发连接", Purpose: "developer", AuthMode: "bearer"})
	if err != nil {
		t.Fatal(err)
	}
	managedRoute, err := connections.Lookup(ctx, managedConnection.Code)
	if err != nil || managedRoute.Transport != "stdio" || managedRoute.RuntimeType != "node" || managedRoute.ContainerName != managedService.ContainerName {
		t.Fatalf("托管网关路由字段错误: %#v %v", managedRoute, err)
	}
	interrupted, err := deploymentRepo.Create(ctx, mcpdeploy.Input{Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID},
		Name: "中断构建测试", SourceType: "upload", SourceRef: "interrupted.tar.gz", Revision: "test",
		RuntimeType: "node", EntryFile: "server.js"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mcp_service_versions SET created_at=NOW()-INTERVAL '10 minutes' WHERE id=$1`, interrupted.Version.ID); err != nil {
		t.Fatal(err)
	}
	if err := deploymentRepo.RecoverInterruptedBuilds(ctx); err != nil {
		t.Fatal(err)
	}
	interruptedService, err := deploymentRepo.Get(ctx, orgID, interrupted.Service.ID)
	if err != nil || interruptedService.DeploymentStatus != "failed" {
		t.Fatalf("中断构建未恢复: %#v %v", interruptedService, err)
	}
	developer, err := connections.Create(ctx, mcpconnection.CreateInput{
		Actor: actor, ServiceID: service.ID, Code: "dev_tools", Name: "开发者工具",
		Purpose: "developer", AuthMode: "bearer", ApplicationName: "业务应用 A",
		UpstreamAuthorization: "Bearer private-upstream-key",
		UpstreamHeaderName:    "X-API-Key", UpstreamHeaderValue: "private-header-key",
		UpstreamQueryName: "key", UpstreamQueryValue: "private-query-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if developer.Secret == "" {
		t.Fatal("开发者连接未生成入口密钥")
	}
	route, err := connections.Lookup(ctx, developer.Code)
	if err != nil || route.UpstreamAuthorization != "Bearer private-upstream-key" ||
		route.UpstreamHeaderName != "X-API-Key" || route.UpstreamHeaderValue != "private-header-key" ||
		route.UpstreamQueryName != "key" || route.UpstreamQueryValue != "private-query-key" || len(route.Tools) != 1 {
		t.Fatalf("连接路由与工具读取失败: %#v, %v", route, err)
	}
	second, err := connections.Create(ctx, mcpconnection.CreateInput{
		Actor: actor, ServiceID: service.ID, Code: "other_app_tools", Name: "另一应用工具",
		Purpose: "developer", AuthMode: "bearer", UpstreamHeaderName: "X-API-Key", UpstreamHeaderValue: "other-app-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondRoute, err := connections.Lookup(ctx, second.Code)
	if err != nil || secondRoute.UpstreamHeaderValue != "other-app-key" || secondRoute.UpstreamAuthorization != "" ||
		secondRoute.UpstreamQueryValue != "" || second.Secret == developer.Secret {
		t.Fatalf("同一服务多连接的凭据或入口密钥发生串扰: %#v, %v", secondRoute, err)
	}
	if _, err := connections.AuthorizeDeveloper(ctx, route, developer.Secret); err != nil {
		t.Fatalf("正确入口密钥被拒绝: %v", err)
	}
	if _, err := connections.AuthorizeDeveloper(ctx, route, "wrong"); err == nil {
		t.Fatal("错误入口密钥未被拒绝")
	}
	revealed, err := connections.RevealSecret(ctx, actor, developer.ID)
	if err != nil || revealed != developer.Secret {
		t.Fatalf("入口密钥再次查看失败: %v", err)
	}
	upstream, err := connections.RevealUpstreamAuthorization(ctx, actor, developer.ID)
	if err != nil || upstream != "Bearer private-upstream-key" {
		t.Fatalf("上游凭据再次查看失败: %v", err)
	}
	credentials, err := connections.RevealUpstreamCredentials(ctx, actor, developer.ID)
	if err != nil || credentials.Authorization != "Bearer private-upstream-key" ||
		credentials.HeaderName != "X-API-Key" || credentials.HeaderValue != "private-header-key" ||
		credentials.QueryName != "key" || credentials.QueryValue != "private-query-key" {
		t.Fatalf("各类型上游凭据再次查看失败: %#v, %v", credentials, err)
	}
	updated, err := connections.UpdateUpstreamCredentials(ctx, actor, developer.ID, mcpadmin.UpstreamCredentials{
		HeaderName: "X-API-Key", HeaderValue: "new-header-key",
	})
	if err != nil || updated.UpstreamAuthConfigured || !updated.UpstreamConfigured || updated.UpstreamQueryConfigured {
		t.Fatalf("上游凭据替换失败: %#v, %v", updated, err)
	}
	route, err = connections.Lookup(ctx, developer.Code)
	if err != nil || route.UpstreamAuthorization != "" || len(route.UpstreamParameters) != 1 ||
		route.UpstreamParameters[0].Name != "X-API-Key" || route.UpstreamParameters[0].Value != "new-header-key" || route.UpstreamQueryValue != "" {
		t.Fatalf("新上游凭据未即时生效: %#v, %v", route, err)
	}
	rotated, err := connections.RotateSecret(ctx, actor, developer.ID)
	if err != nil || rotated == developer.Secret || rotated == "" {
		t.Fatalf("入口密钥轮换失败: %v", err)
	}
	route, err = connections.Lookup(ctx, developer.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connections.AuthorizeDeveloper(ctx, route, developer.Secret); err != mcpconnection.ErrUnauthorized {
		t.Fatalf("旧入口密钥轮换后仍可调用: %v", err)
	}
	if _, err := connections.AuthorizeDeveloper(ctx, route, rotated); err != nil {
		t.Fatalf("新入口密钥轮换后不可调用: %v", err)
	}
	revealed, err = connections.RevealSecret(ctx, actor, developer.ID)
	if err != nil || revealed != rotated {
		t.Fatalf("轮换后查看入口密钥失败: %v", err)
	}
	harness, err := connections.Create(ctx, mcpconnection.CreateInput{
		Actor: actor, ServiceID: service.ID, Code: "employee_tools", Name: "员工工具",
		Purpose: "harness", AuthMode: "oidc", DepartmentIDs: []string{departmentID},
	})
	if err != nil {
		t.Fatal(err)
	}
	harnessRoute, err := connections.Lookup(ctx, harness.Code)
	if err != nil {
		t.Fatal(err)
	}
	caller, err := connections.AuthorizeUser(ctx, harnessRoute, orgID, userID)
	if err != nil || caller.DepartmentID != departmentID {
		t.Fatalf("部门授权未生效: %#v, %v", caller, err)
	}
	if err := connections.RecordCall(ctx, mcpconnection.CallRecord{
		ConnectionID: harness.ID, OrganizationID: orgID, ServiceID: service.ID,
		ToolName: "working_directory", UserID: userID, DepartmentID: departmentID, Result: "success", LatencyMS: 12,
	}); err != nil {
		t.Fatal(err)
	}
	usage, err := connections.ListUsage(ctx, orgID)
	if err != nil || len(usage) != 1 || usage[0].UserID != userID || usage[0].DepartmentID != departmentID {
		t.Fatalf("员工调用统计归属不正确: %#v, %v", usage, err)
	}
	if _, err := connections.ReplaceGrants(ctx, mcpconnection.GrantsInput{Actor: actor, ConnectionID: harness.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := connections.AuthorizeUser(ctx, harnessRoute, orgID, userID); err != mcpconnection.ErrForbidden {
		t.Fatalf("撤权后仍能调用: %v", err)
	}
	parameterService, err := serviceRepo.Create(ctx, mcpadmin.CreateInput{
		Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID},
		Code:  "parameterized", Name: "参数化服务", Description: "新旧连接参数隔离测试", Transport: "streamable_http",
		UpstreamURL: "https://example.com/{tenant}/mcp",
		Parameters: []mcpadmin.ParameterDefinition{
			{Location: "path", Name: "tenant", Required: true},
			{Location: "header", Name: "aaaa", Required: true},
		},
		Credentials: mcpadmin.UpstreamCredentials{Parameters: []mcpadmin.ParameterValue{
			{Location: "path", Name: "tenant", Value: "company-a"},
			{Location: "header", Name: "aaaa", Value: "service-private-key"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parameterized, err := connections.Create(ctx, mcpconnection.CreateInput{
		Actor: actor, ServiceID: parameterService.ID, Name: "任意字段连接",
		Purpose: "developer", AuthMode: "bearer",
	})
	if err != mcpconnection.ErrInvalidInput {
		t.Fatalf("新连接缺少必填参数值应被拒绝: %#v, %v", parameterized, err)
	}
	parameterized, err = connections.Create(ctx, mcpconnection.CreateInput{
		Actor: actor, ServiceID: parameterService.ID, Name: "任意字段连接",
		Purpose: "developer", AuthMode: "bearer",
		UpstreamParameters: []mcpadmin.ParameterValue{
			{Location: "path", Name: "tenant", Value: "company-a"},
			{Location: "header", Name: "aaaa", Value: "connection-private-key"},
		},
	})
	if err != nil || parameterized.Code == "" || !parameterized.UpstreamConfigured {
		t.Fatalf("填写独立参数值后连接创建失败: %#v, %v", parameterized, err)
	}
	parameterRoute, err := connections.Lookup(ctx, parameterized.Code)
	if err != nil || len(parameterRoute.ServiceParameters) != 0 || len(parameterRoute.UpstreamParameters) != 2 {
		t.Fatalf("新连接不应继承旧服务级参数: %#v, %v", parameterRoute, err)
	}
	parameterCredentials, err := serviceRepo.RevealCredentials(ctx, mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, parameterService.ID)
	if err != nil || len(parameterCredentials.Parameters) != 2 || parameterCredentials.Parameters[1].Name != "aaaa" {
		t.Fatalf("服务凭证无法再次查看: %#v, %v", parameterCredentials, err)
	}
	overridden, err := connections.Create(ctx, mcpconnection.CreateInput{
		Actor: actor, ServiceID: parameterService.ID, Name: "保留旧式独立凭证",
		Purpose: "developer", AuthMode: "bearer",
		UpstreamParameters: []mcpadmin.ParameterValue{
			{Location: "path", Name: "tenant", Value: "company-a"},
			{Location: "header", Name: "aaaa", Value: "connection-private-key"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟升级前已存在的连接；迁移会为这类连接开启历史继承标记。
	if _, err := pool.Exec(ctx, `UPDATE mcp_connections SET inherit_service_credentials = TRUE WHERE id = $1`, overridden.ID); err != nil {
		t.Fatal(err)
	}
	overriddenRoute, err := connections.Lookup(ctx, overridden.Code)
	if err != nil {
		t.Fatal(err)
	}
	effective := mcpadmin.MergeCredentials(mcpadmin.UpstreamCredentials{Parameters: overriddenRoute.ServiceParameters},
		mcpadmin.UpstreamCredentials{Parameters: overriddenRoute.UpstreamParameters})
	if len(overriddenRoute.ServiceParameters) != 2 || len(effective.Parameters) != 2 || effective.Parameters[1].Value != "connection-private-key" {
		t.Fatalf("已有连接的独立凭证未覆盖服务默认值: %#v", effective)
	}
	forDiscovery, err := serviceRepo.GetConnectionCredentials(ctx, orgID, parameterService.ID, overridden.ID)
	if err != nil || len(forDiscovery.Parameters) != 2 || forDiscovery.Parameters[1].Value != "connection-private-key" {
		t.Fatalf("工具发现未读取选定连接参数: %#v, %v", forDiscovery, err)
	}
	newDefinitions := []mcpadmin.ParameterDefinition{
		{Location: "path", Name: "tenant", Required: true},
		{Location: "header", Name: "aaaa", Required: true},
		{Location: "query", Name: "region", Required: false},
	}
	_, err = serviceRepo.UpdateCredentials(ctx, mcpadmin.UpdateCredentialsInput{
		Actor: mcpadmin.Actor{OrganizationID: orgID, UserID: userID}, ServiceID: parameterService.ID,
		Definitions: &newDefinitions,
		Credentials: mcpadmin.UpstreamCredentials{Parameters: []mcpadmin.ParameterValue{
			{Location: "path", Name: "tenant", Value: "company-b"},
			{Location: "header", Name: "aaaa", Value: "rotated-service-key"},
			{Location: "query", Name: "region", Value: "cn"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedService, err := serviceRepo.Get(ctx, orgID, parameterService.ID)
	if err != nil || len(updatedService.Parameters) != 3 {
		t.Fatalf("服务参数定义更新失败: %#v, %v", updatedService, err)
	}
	parameterRoute, err = connections.Lookup(ctx, parameterized.Code)
	if err != nil || len(parameterRoute.ServiceParameters) != 0 {
		t.Fatalf("更新旧服务级值后，新连接不应被影响: %#v, %v", parameterRoute, err)
	}
	overriddenRoute, err = connections.Lookup(ctx, overridden.Code)
	if err != nil || len(overriddenRoute.ServiceParameters) != 3 || overriddenRoute.ServiceParameters[1].Value != "rotated-service-key" {
		t.Fatalf("历史连接应继续继承旧服务级值: %#v, %v", overriddenRoute, err)
	}
}
