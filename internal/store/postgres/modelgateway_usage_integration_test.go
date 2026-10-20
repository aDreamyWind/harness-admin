package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/modelgateway"
)

func TestModelGatewayUsageQueriesAgainstDatabase(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("未配置只读数据库集成测试地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	organizationID := uuid.NewString()
	err = pool.QueryRow(ctx, `SELECT id::TEXT FROM organizations ORDER BY created_at LIMIT 1`).Scan(&organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	repository := NewModelGatewayRepository(pool, nil)
	if _, err := repository.ListAlertCandidates(ctx, organizationID, "2026-10"); err != nil {
		t.Fatalf("预警指标 SQL 执行失败: %v", err)
	}
	if _, err := repository.ListAlerts(ctx, modelgateway.AlertQuery{
		OrganizationID: organizationID, Month: "2026-10", Status: "active", Page: 1, PageSize: 20,
	}); err != nil {
		t.Fatalf("预警列表 SQL 执行失败: %v", err)
	}
	for _, groupBy := range []string{"department", "user", "model"} {
		page, err := repository.ListUsage(ctx, modelgateway.UsageQuery{
			OrganizationID: organizationID, Month: "2026-10", GroupBy: groupBy, Page: 1, PageSize: 20,
		})
		if err != nil {
			t.Fatalf("%s 维度统计 SQL 执行失败: %v", groupBy, err)
		}
		if page.GroupBy != groupBy || page.Items == nil || page.CallCount < 0 || page.Total < 0 {
			t.Fatalf("%s 维度统计结果不正确: %#v", groupBy, page)
		}
	}
}

func TestModelGatewayAlertDeduplicationAgainstDatabase(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("未配置可写数据库集成测试地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	organizationID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, code, name) VALUES ($1, $2, '预警集成测试企业')`,
		organizationID, "alert-test-"+organizationID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM audit_events WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("清理测试审计事件失败: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM model_gateway_alerts WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("清理测试预警失败: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM model_gateway_alert_settings WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("清理测试预警设置失败: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("清理测试用户失败: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id = $1`, organizationID); err != nil {
			t.Errorf("清理测试企业失败: %v", err)
		}
	}()
	actorID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, organization_id, username, display_name, password_hash)
		VALUES ($1, $2, 'alert-test-admin', '预警测试管理员', 'integration-test-unusable')`, actorID, organizationID); err != nil {
		t.Fatal(err)
	}
	repository := NewModelGatewayRepository(pool, nil)
	settings, err := repository.GetAlertSettings(ctx, organizationID)
	if err != nil || settings.AutoRefreshEnabled {
		t.Fatalf("预警自动刷新默认应关闭: %#v, %v", settings, err)
	}
	settings, err = repository.UpdateAlertSettings(ctx, modelgateway.UpdateAlertSettingsInput{
		OrganizationID: organizationID, ActorID: actorID, AutoRefreshEnabled: true,
		RequestID: "alert-settings-test", SourceIP: "127.0.0.1",
	})
	if err != nil || !settings.AutoRefreshEnabled || settings.UpdatedAt == nil {
		t.Fatalf("保存开启状态失败: %#v, %v", settings, err)
	}
	enabledOrganizations, err := repository.ListAutoRefreshOrganizations(ctx)
	if err != nil || !containsOrganization(enabledOrganizations, organizationID) {
		t.Fatalf("后台刷新任务未看到启用企业: %v, %v", enabledOrganizations, err)
	}
	settings, err = repository.UpdateAlertSettings(ctx, modelgateway.UpdateAlertSettingsInput{
		OrganizationID: organizationID, ActorID: actorID, AutoRefreshEnabled: false,
		RequestID: "alert-settings-test", SourceIP: "127.0.0.1",
	})
	if err != nil || settings.AutoRefreshEnabled {
		t.Fatalf("关闭后台刷新失败: %#v, %v", settings, err)
	}
	enabledOrganizations, err = repository.ListAutoRefreshOrganizations(ctx)
	if err != nil || containsOrganization(enabledOrganizations, organizationID) {
		t.Fatalf("关闭后仍被后台任务读取: %v, %v", enabledOrganizations, err)
	}
	month := time.Now().UTC().Format("2006-01")
	if candidates, err := repository.ListAlertCandidates(ctx, organizationID, month); err != nil || len(candidates) != 0 {
		t.Fatalf("预警候选查询失败: %#v, %v", candidates, err)
	}
	signal := modelgateway.AlertSignal{AlertCandidate: modelgateway.AlertCandidate{
		DedupeKey: "quota:user:" + uuid.NewString(), Kind: "quota", SubjectType: "user",
		SubjectName: "测试用户", UsedTokens: 8, TokenLimit: 10,
	}, Severity: "warning"}
	for range 2 {
		if err := repository.SyncAlerts(ctx, organizationID, month, []modelgateway.AlertSignal{signal}); err != nil {
			t.Fatal(err)
		}
	}
	query := modelgateway.AlertQuery{OrganizationID: organizationID, Month: month, Status: "all", Page: 1, PageSize: 20}
	page, err := repository.ListAlerts(ctx, query)
	if err != nil || page.Total != 1 || page.ActiveCount != 1 || len(page.Items) != 1 || page.Items[0].Month != month {
		t.Fatalf("重复刷新应只有一条有效预警: %#v, %v", page, err)
	}
	alertID := page.Items[0].ID
	acknowledged, err := repository.AcknowledgeAlert(ctx, modelgateway.AcknowledgeAlertInput{
		OrganizationID: organizationID, ActorID: actorID, AlertID: alertID, RequestID: "alert-test",
	})
	if err != nil || acknowledged.AcknowledgedAt == nil {
		t.Fatalf("确认预警失败: %#v, %v", acknowledged, err)
	}
	signal.Severity = "critical"
	signal.UsedTokens = 10
	if err := repository.SyncAlerts(ctx, organizationID, month, []modelgateway.AlertSignal{signal}); err != nil {
		t.Fatal(err)
	}
	page, err = repository.ListAlerts(ctx, query)
	if err != nil || page.Items[0].ID != alertID || page.Items[0].Severity != "critical" || page.Items[0].AcknowledgedAt != nil {
		t.Fatalf("升级后应保持同一预警记录: %#v, %v", page, err)
	}
	if err := repository.SyncAlerts(ctx, organizationID, month, nil); err != nil {
		t.Fatal(err)
	}
	page, err = repository.ListAlerts(ctx, query)
	if err != nil || page.ActiveCount != 0 || page.Items[0].ResolvedAt == nil {
		t.Fatalf("条件消失后预警应解除: %#v, %v", page, err)
	}
}

func containsOrganization(organizations []string, target string) bool {
	for _, organizationID := range organizations {
		if organizationID == target {
			return true
		}
	}
	return false
}
