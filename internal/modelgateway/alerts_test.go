package modelgateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAlertJSONUsesPublicContractFieldNames(t *testing.T) {
	body, err := json.Marshal(Alert{ID: "alert-1", Month: "2026-10", AlertSignal: AlertSignal{
		AlertCandidate: AlertCandidate{DedupeKey: "quota:user:1", Kind: "quota", SubjectName: "测试用户", UsedTokens: 80, TokenLimit: 100},
		Severity:       "warning",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"dedupeKey":"quota:user:1"`, `"subjectName":"测试用户"`, `"usedTokens":80`, `"severity":"warning"`} {
		if !strings.Contains(string(body), field) {
			t.Fatalf("预警 JSON 缺少 %s: %s", field, body)
		}
	}
}

func TestEvaluateAlertCandidatesUsesCorrectQuotaPool(t *testing.T) {
	candidates := []AlertCandidate{
		{DedupeKey: "quota:department:shared", Kind: "quota", SubjectType: "department", UsedTokens: 79, TokenLimit: 100},
		{DedupeKey: "quota:department_user:dept:user", Kind: "quota", SubjectType: "department_user", UsedTokens: 80, TokenLimit: 100},
		{DedupeKey: "quota:user:user", Kind: "quota", SubjectType: "user", UsedTokens: 100, TokenLimit: 100},
		{DedupeKey: "quota:api_key:key", Kind: "quota", SubjectType: "api_key", UsedTokens: 120, TokenLimit: 100},
		{DedupeKey: "quota:unlimited", Kind: "quota", UsedTokens: 1000, TokenLimit: 0},
	}
	signals := evaluateAlertCandidates(candidates)
	if len(signals) != 3 || signals[0].Severity != "warning" || signals[0].SubjectType != "department_user" ||
		signals[1].Severity != "critical" || signals[2].Severity != "critical" {
		t.Fatalf("额度预警计算错误: %#v", signals)
	}
}

func TestEvaluateAlertCandidatesUnknownUsageThreshold(t *testing.T) {
	signals := evaluateAlertCandidates([]AlertCandidate{
		{DedupeKey: "a", Kind: "unknown_usage", CallCount: 20, UnknownUsageCalls: 4},
		{DedupeKey: "b", Kind: "unknown_usage", CallCount: 25, UnknownUsageCalls: 5},
		{DedupeKey: "c", Kind: "unknown_usage", CallCount: 30, UnknownUsageCalls: 5},
		{DedupeKey: "d", Kind: "unknown_usage", CallCount: 26, UnknownUsageCalls: 6},
	})
	if len(signals) != 2 || signals[0].DedupeKey != "b" || signals[1].DedupeKey != "d" {
		t.Fatalf("用量未知阈值计算错误: %#v", signals)
	}
}

func TestListAlertsRefreshesOnlyCurrentUTCMonth(t *testing.T) {
	repository := &repositoryStub{alertCandidates: []AlertCandidate{{DedupeKey: "quota:user:1", Kind: "quota", UsedTokens: 8, TokenLimit: 10}}}
	service := NewService(repository, nil, nil)
	service.now = func() time.Time { return time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC) }
	if _, err := service.ListAlerts(context.Background(), AlertQuery{OrganizationID: "org-1"}); err != nil {
		t.Fatal(err)
	}
	if repository.alertQuery.Month != "2026-11" || repository.alertQuery.Status != "active" || repository.alertSyncCount != 1 || len(repository.alertSignals) != 1 {
		t.Fatalf("当前月份预警未刷新: %#v", repository)
	}
	if _, err := service.ListAlerts(context.Background(), AlertQuery{OrganizationID: "org-1", Month: "2026-10", Status: "all"}); err != nil {
		t.Fatal(err)
	}
	if repository.alertSyncCount != 1 {
		t.Fatal("历史月份不应按现有策略重新生成预警")
	}
	for _, query := range []AlertQuery{
		{OrganizationID: "org-1", Month: "2026-13"},
		{OrganizationID: "org-1", Status: "resolved"},
		{OrganizationID: "org-1", PageSize: 101},
	} {
		if _, err := service.ListAlerts(context.Background(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("无效查询应拒绝: %#v, %v", query, err)
		}
	}
}
