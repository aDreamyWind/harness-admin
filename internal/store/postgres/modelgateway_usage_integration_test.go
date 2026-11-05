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

func TestModelGatewayVideoUsageSeparatesSubmissionsCompletionsAndQueries(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	orgID, userID, departmentID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	providerID, videoModelID, textModelID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	projectID, runID, segmentID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	localSuccessID, localFailureID := uuid.NewString(), uuid.NewString()
	month := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	exec(`INSERT INTO organizations (id, code, name) VALUES ($1, $2, '视频统计测试企业')`, orgID, "video-usage-"+orgID)
	exec(`INSERT INTO departments (id, organization_id, code, name) VALUES ($1, $2, 'video', '视频制作')`, departmentID, orgID)
	exec(`INSERT INTO users (id, organization_id, username, display_name, password_hash)
		VALUES ($1, $2, 'video-test', '测试用户', 'integration-test-unusable')`, userID, orgID)
	exec(`INSERT INTO model_provider_connections (id, organization_id, code, name, provider_kind, base_url)
		VALUES ($1, $2, 'video', '测试供应商', 'aliyun_bailian', 'https://example.invalid')`, providerID, orgID)
	exec(`INSERT INTO model_catalog_entries (id, organization_id, provider_connection_id, code, upstream_model_id, name, model_type)
		VALUES ($1, $2, $3, 'video-test', 'wan2.7-r2v', '测试视频模型', 'video'),
		       ($4, $2, $3, 'text-test', 'text-model', '测试文本模型', 'chat')`, videoModelID, orgID, providerID, textModelID)
	exec(`INSERT INTO drama_projects (id, organization_id, owner_id, title, source_text, source_length, source_mode, style, aspect_ratio, model_code, status, phase)
		VALUES ($1, $2, $3, '测试项目', '测试', 2, 'upload', '2D', '9:16', 'text-test', 'ready', 'done')`, projectID, orgID, userID)
	exec(`INSERT INTO drama_storyboard_runs (id, source_episode_id, project_id, version, status, script_text, source_hash, model_code, aspect_ratio)
		VALUES ($1, $2, $3, 1, 'confirmed', '测试', 'test', 'text-test', '9:16')`, runID, uuid.NewString(), projectID)
	exec(`INSERT INTO drama_storyboard_segments (id, run_id, number, title, start_line, end_line, status)
		VALUES ($1, $2, 1, '测试片段', 1, 1, 'ready')`, segmentID, runID)
	exec(`INSERT INTO drama_segment_videos (id, segment_id, storyboard_revision, model_code, duration_seconds, reference_image_ids, status, finished_at)
		VALUES ($1, $3, 1, 'video-test', 5, '{}', 'succeeded', $4),
		       ($2, $3, 1, 'video-test', 5, '{}', 'failed', $4)`, localSuccessID, localFailureID, segmentID, month.Add(time.Minute))

	record := func(at time.Time, department, source, operation, segmentVideoID, result, request, response string) {
		t.Helper()
		callID := uuid.NewString()
		modelID, modelCode, modelType, tokens := videoModelID, "video-test", "video", 0
		if operation == "chat" {
			modelID, modelCode, modelType, tokens = textModelID, "text-test", "chat", 12
		}
		exec(`INSERT INTO model_gateway_calls (id, organization_id, auth_type, user_id, model_catalog_entry_id, provider_connection_id,
			request_id, model_code, model_type, upstream_model_id, result, http_status, latency_ms, department_id, created_at, total_tokens, usage_reported)
			VALUES ($1, $2, 'user', $3, $4, $5, $6, $7, $8, $7, $9, 200, 50, NULLIF($10, '')::UUID, $11, $12, $13)`,
			callID, orgID, userID, modelID, providerID, uuid.NewString(), modelCode, modelType, result, department, at, tokens, tokens > 0)
		if operation != "" {
			if err := insertModelCallLog(ctx, tx, callID, modelgateway.RecordCallInput{
				OrganizationID: orgID, Source: source, Operation: operation, SegmentVideoID: segmentVideoID,
				RequestBody: []byte(request), ResponseBody: []byte(response),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	localDone := `{"output":{"task_id":"local","task_status":"SUCCEEDED","video_url":"https://example.invalid/local.mp4"}}`
	externalPending := `{"output":{"task_id":"external","task_status":"PENDING"}}`
	externalDone := `{"output":{"task_id":"external","task_status":"SUCCEEDED","video_url":"https://example.invalid/external.mp4"}}`
	legacyDone := `{"output":{"task_status":"SUCCEEDED","video_url":"https://example.invalid/legacy.mp4"}}`
	record(month.Add(-time.Hour), departmentID, "ai_drama", "video", localSuccessID, "success", "", "")
	record(month.Add(-time.Minute), departmentID, "model_gateway", "video", "", "success", "", externalPending)
	record(month, departmentID, "ai_drama", "video_task_query", localSuccessID, "success", "", localDone)
	record(month, departmentID, "ai_drama", "video", localFailureID, "success", "", "")
	record(month, departmentID, "ai_drama", "video_task_query", localFailureID, "success", "", localDone)
	record(month, departmentID, "ai_drama", "video", "", "success", "", `{"output":{"task_id":"legacy","task_status":"PENDING"}}`)
	record(month.Add(time.Second), departmentID, "ai_drama", "video_task_query", "", "success", `{"task_id":"legacy"}`, legacyDone)
	record(month, "", "model_gateway", "video_task_query", "", "success", "", externalDone)
	record(month.Add(time.Second), "", "model_gateway", "video_task_query", "", "success", "", externalDone)
	record(month.Add(time.Second), "", "model_gateway", "video_task_query", "", "success", "", "\xffSUCCEEDED")
	record(month.Add(time.Second), "", "model_gateway", "video_task_query", "", "failed", "", "")
	record(month.Add(time.Second), departmentID, "model_gateway", "chat", "", "success", "", "")
	record(month.Add(time.Second), departmentID, "", "", "", "success", "", "")
	record(month.AddDate(0, 1, 0), "", "model_gateway", "video_task_query", "", "success", "", externalDone)

	query := modelgateway.UsageQuery{OrganizationID: orgID, Month: "2026-11", GroupBy: "model", Page: 1, PageSize: 20}
	page, err := listModelUsage(ctx, tx, query)
	if err != nil || page.CallCount != 11 || page.KnownTokens != 12 || page.UnknownUsageCalls != 10 ||
		page.VideoSubmitCount != 2 || page.VideoSuccessCount != 3 || page.VideoQueryCount != 7 || page.Total != 2 {
		t.Fatalf("统计应区分提交、已成片和状态查询: %#v, %v", page, err)
	}
	if len(page.Items) != 2 || page.Items[1].ID != "video-test" ||
		page.Items[1].VideoSubmitCount != 2 || page.Items[1].VideoSuccessCount != 3 || page.Items[1].VideoQueryCount != 7 {
		t.Fatalf("按模型的分组统计有误: %#v", page.Items)
	}
	query.GroupBy = "department"
	page, err = listModelUsage(ctx, tx, query)
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != departmentID ||
		page.Items[0].VideoSuccessCount != 3 || page.Items[1].ID != "" || page.Items[1].VideoSuccessCount != 0 ||
		page.Items[1].VideoQueryCount != 4 {
		t.Fatalf("无部门的外部查询不能改变视频完成的提交部门归属: %#v, %v", page, err)
	}
	query.Month = "2026-12"
	page, err = listModelUsage(ctx, tx, query)
	if err != nil || page.VideoQueryCount != 1 || page.VideoSuccessCount != 0 {
		t.Fatalf("同一视频的跨月成功查询只能算查询: %#v, %v", page, err)
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
