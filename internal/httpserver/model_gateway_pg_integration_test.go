package httpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	"harness-admin/internal/secret"
	pgstore "harness-admin/internal/store/postgres"
)

// 仅在显式提供可写测试库时执行，使用独立企业和模拟上游，不调用真实 Harness 或模型供应商。
func TestGatewayE2EConcurrentSoftQuotaAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未配置可写数据库集成测试地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		var body struct {
			Model    string `json:"model"`
			TestCase string `json:"testCase"`
		}
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer mock-provider-secret" ||
			json.NewDecoder(request.Body).Decode(&body) != nil || body.Model != "mock-model-v1" {
			t.Errorf("上游地址、认证或模型改写错误: path=%s model=%s", request.URL.Path, body.Model)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.TestCase == "unknown-usage" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","content":"未报告用量"}}]}`)
			return
		}
		arrived <- struct{}{}
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","content":"完成"}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	}))
	defer upstream.Close()

	organizationID, actorID := uuid.NewString(), uuid.NewString()
	providerID, modelID := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, code, name) VALUES ($1, $2, '网关端到端测试企业')`,
		organizationID, "gateway-e2e-"+organizationID); err != nil {
		t.Fatal(err)
	}
	defer cleanupGatewayE2EOrganization(t, pool, organizationID)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, organization_id, username, display_name, password_hash)
		VALUES ($1, $2, 'gateway-e2e-admin', '网关端到端测试管理员', 'integration-test-unusable')`, actorID, organizationID); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	providerSecret, err := cipher.Seal("mock-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO model_provider_connections
		(id, organization_id, code, name, provider_kind, base_url, request_timeout_ms)
		VALUES ($1, $2, 'mock-provider', '模拟供应商', 'openai_compatible', $3, 5000)`,
		providerID, organizationID, upstream.URL+"/v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO model_provider_credentials (provider_connection_id, encrypted_secret)
		VALUES ($1, $2)`, providerID, providerSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO model_catalog_entries
		(id, organization_id, provider_connection_id, code, upstream_model_id, name)
		VALUES ($1, $2, $3, 'main', 'mock-model-v1', '模拟模型')`, modelID, organizationID, providerID); err != nil {
		t.Fatal(err)
	}
	repository := pgstore.NewModelGatewayRepository(pool, cipher)
	service := modelgateway.NewService(repository, nil, nil)
	key, err := service.CreateAPIKey(ctx, modelgateway.CreateAPIKeyInput{
		OrganizationID: organizationID, ActorID: actorID, Name: "软额度测试密钥",
		MonthlyTokenLimit: 10, AllowedModelCodes: []string{"main"}, SourceIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	keysBeforeRename, err := service.ListAPIKeys(ctx, organizationID)
	if err != nil || len(keysBeforeRename) != 1 || len(keysBeforeRename[0].AllowedModels) != 1 ||
		keysBeforeRename[0].AllowedModels[0].ProviderName != "模拟供应商" ||
		keysBeforeRename[0].AllowedModels[0].ModelName != "模拟模型" {
		t.Fatalf("调用密钥范围应显示当前名称: %#v, %v", keysBeforeRename, err)
	}
	server := httptest.NewServer(New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway: service, GatewayForwarder: modelprovider.NewGatewayForwarder([]string{"127.0.0.1"}),
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(release) })
	client := &http.Client{Timeout: 15 * time.Second}
	unknownRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"main","messages":[],"testCase":"unknown-usage"}`))
	if err != nil {
		t.Fatal(err)
	}
	unknownRequest.Header.Set("Authorization", "Bearer "+key.Secret)
	unknownResponse, err := client.Do(unknownRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, unknownResponse.Body); err != nil {
		unknownResponse.Body.Close()
		t.Fatal(err)
	}
	if err := unknownResponse.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if unknownResponse.StatusCode != http.StatusOK {
		t.Fatalf("未返回用量的调用仍应完成: %d", unknownResponse.StatusCode)
	}
	callQuery := modelgateway.CallQuery{OrganizationID: organizationID, Page: 1, PageSize: 10}
	beforeRename, err := service.ListCalls(ctx, callQuery)
	if err != nil || len(beforeRename.Items) != 1 || beforeRename.Items[0].ModelName != "模拟模型" || beforeRename.Items[0].ProviderName != "模拟供应商" {
		t.Fatalf("调用记录应显示当前名称: %#v, %v", beforeRename, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE model_catalog_entries SET name = '修改后的模型名' WHERE id = $1`, modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE model_provider_connections SET name = '修改后的供应商名' WHERE id = $1`, providerID); err != nil {
		t.Fatal(err)
	}
	keysAfterRename, err := service.ListAPIKeys(ctx, organizationID)
	if err != nil || len(keysAfterRename) != 1 || len(keysAfterRename[0].AllowedModels) != 1 ||
		keysAfterRename[0].AllowedModels[0].Code != "main" ||
		keysAfterRename[0].AllowedModels[0].ProviderName != "修改后的供应商名" ||
		keysAfterRename[0].AllowedModels[0].ModelName != "修改后的模型名" {
		t.Fatalf("改名后调用密钥应显示新名称并保留编码: %#v, %v", keysAfterRename, err)
	}
	afterRename, err := service.ListCalls(ctx, callQuery)
	if err != nil || len(afterRename.Items) != 1 || afterRename.Items[0].ModelName != "修改后的模型名" ||
		afterRename.Items[0].ProviderName != "修改后的供应商名" || afterRename.Items[0].ModelCode != "main" ||
		afterRename.Items[0].UpstreamModelID != "mock-model-v1" {
		t.Fatalf("改名应更新历史记录展示名称并保留调用编码: %#v, %v", afterRename, err)
	}
	beforeConcurrency, err := service.ListUsage(ctx, modelgateway.UsageQuery{
		OrganizationID: organizationID, Month: time.Now().UTC().Format("2006-01"), GroupBy: "model", Page: 1, PageSize: 10,
	})
	if err != nil || beforeConcurrency.CallCount != 1 || beforeConcurrency.KnownTokens != 0 || beforeConcurrency.UnknownUsageCalls != 1 {
		t.Fatalf("用量未知的调用不应占用已知 Token 额度: %#v, %v", beforeConcurrency, err)
	}
	type responseResult struct {
		status int
		body   string
		err    error
	}
	results := make(chan responseResult, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions",
				strings.NewReader(`{"model":"main","messages":[{"role":"user","content":"测试"}]}`))
			if err != nil {
				results <- responseResult{err: err}
				return
			}
			request.Header.Set("Authorization", "Bearer "+key.Secret)
			response, err := client.Do(request)
			if err != nil {
				results <- responseResult{err: err}
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			results <- responseResult{status: response.StatusCode, body: string(body), err: err}
		}()
	}
	close(start)
	for range 2 {
		select {
		case <-arrived:
		case result := <-results:
			t.Fatalf("两次调用均应在入账前到达上游，提前结束: %#v", result)
		case <-time.After(10 * time.Second):
			t.Fatal("等待两次并发请求到达模拟上游超时")
		}
	}
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil || result.status != http.StatusOK || !strings.Contains(result.body, `"total_tokens":7`) {
				t.Fatalf("已开始的并发调用应正常完成: %#v", result)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("并发调用完成超时")
		}
	}

	calls, err := service.ListCalls(ctx, modelgateway.CallQuery{OrganizationID: organizationID, Page: 1, PageSize: 10})
	if err != nil || calls.Total != 3 || len(calls.Items) != 3 {
		t.Fatalf("并发调用记录数量错误: %#v, %v", calls, err)
	}
	exceeded := 0
	knownCalls := 0
	unknownCalls := 0
	for _, call := range calls.Items {
		if call.Result != "success" || call.APIKeyID != key.ID {
			t.Fatalf("并发调用用量记录错误: %#v", call)
		}
		if call.UsageReported && call.TotalTokens == 7 {
			knownCalls++
		} else if !call.UsageReported && call.TotalTokens == 0 {
			unknownCalls++
		} else {
			t.Fatalf("已知或未知用量记录异常: %#v", call)
		}
		if call.QuotaExceeded {
			exceeded++
		}
	}
	if exceeded != 1 || knownCalls != 2 || unknownCalls != 1 {
		t.Fatalf("已知/未知用量及软额度标记错误: known=%d unknown=%d exceeded=%d", knownCalls, unknownCalls, exceeded)
	}
	usage, err := service.ListUsage(ctx, modelgateway.UsageQuery{
		OrganizationID: organizationID, Month: time.Now().UTC().Format("2006-01"), GroupBy: "model", Page: 1, PageSize: 10,
	})
	if err != nil || usage.CallCount != 3 || usage.KnownTokens != 14 || usage.OverQuotaCalls != 1 || usage.UnknownUsageCalls != 1 {
		t.Fatalf("并发软额度用量汇总错误: %#v, %v", usage, err)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"main","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+key.Secret)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	blockedBody, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusTooManyRequests ||
		!strings.Contains(string(blockedBody), `"code":"insufficient_quota"`) || upstreamCalls.Load() != 3 {
		t.Fatalf("超过额度后新请求应在网关被拒绝: status=%d body=%s upstream=%d err=%v",
			response.StatusCode, blockedBody, upstreamCalls.Load(), err)
	}
}

func cleanupGatewayE2EOrganization(t *testing.T, pool *pgxpool.Pool, organizationID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, query := range []string{
		`DELETE FROM model_gateway_calls WHERE organization_id = $1`,
		`DELETE FROM model_gateway_monthly_usage WHERE organization_id = $1`,
		`DELETE FROM model_gateway_api_keys WHERE organization_id = $1`,
		`DELETE FROM model_catalog_entries WHERE organization_id = $1`,
		`DELETE FROM model_provider_connections WHERE organization_id = $1`,
		`DELETE FROM audit_events WHERE organization_id = $1`,
		`DELETE FROM users WHERE organization_id = $1`,
		`DELETE FROM organizations WHERE id = $1`,
	} {
		if _, err := pool.Exec(ctx, query, organizationID); err != nil {
			t.Errorf("清理网关端到端测试数据失败: %v", err)
		}
	}
}
